package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/ippool"
	"robot/apps/local-agent/internal/orders"
	"robot/apps/local-agent/internal/userkey"
	"sort"
	"strings"
	"time"
)

type orderSender interface {
	CreateOrder(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error)
}

type OrderViewer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Card  string `json:"card"`
	Phone string `json:"phone"`
}
type OrderRecord struct {
	OrderIP     string               `json:"orderIP,omitempty"`
	Simulated   bool                 `json:"simulated,omitempty"`
	Account     string               `json:"account"`
	UserID      string               `json:"userId"`
	ID          string               `json:"id"`
	TaskID      int64                `json:"taskId"`
	Match       protocol.MatchConfig `json:"match"`
	ViewerIDs   []string             `json:"viewerIds"`
	Viewers     []OrderViewer        `json:"viewers"`
	Plan        inventory.Plan       `json:"plan"`
	CreatedAt   time.Time            `json:"createdAt"`
	Token       string               `json:"token"`
	Details     business.OrderDetail `json:"details"`
	DetailError string               `json:"detailError,omitempty"`
}
type OrderDisplay struct {
	OrderIP     string               `json:"orderIP,omitempty"`
	Simulated   bool                 `json:"simulated,omitempty"`
	ID          string               `json:"id"`
	TaskID      int64                `json:"taskId"`
	Match       protocol.MatchConfig `json:"match"`
	ViewerIDs   []string             `json:"viewerIds"`
	Viewers     []OrderViewer        `json:"viewers"`
	Plan        inventory.Plan       `json:"plan"`
	CreatedAt   time.Time            `json:"createdAt"`
	Details     business.OrderDetail `json:"details"`
	DetailError string               `json:"detailError,omitempty"`
}

type orderAttempt struct {
	Account   string               `json:"account"`
	TaskID    int64                `json:"taskId"`
	Match     protocol.MatchConfig `json:"match"`
	ViewerIDs []string             `json:"viewerIds"`
	Viewers   []OrderViewer        `json:"viewers"`
	Plan      inventory.Plan       `json:"plan"`
	Token     string               `json:"token"`
	CreatedAt time.Time            `json:"createdAt"`
}
type manualRunState struct {
	Realtime bool                 `json:"realtime,omitempty"`
	TaskID   int64                `json:"taskId"`
	Match    protocol.MatchConfig `json:"match"`
	Status   string               `json:"status"`
	Message  string               `json:"message"`
}

func (s *Service) loadOrderStateLocked() error {
	if s.inventory.orders == nil {
		s.inventory.orders = map[string]*OrderRecord{}
	}
	if s.inventory.pending == nil {
		s.inventory.pending = map[int64]*orderAttempt{}
	}
	if s.inventory.saleStops == nil {
		s.inventory.saleStops = map[string]bool{}
	}
	if s.inventory.manual == nil {
		s.inventory.manual = map[int64]manualRunState{}
	}
	if s.store == nil {
		return nil
	}
	orders, err := s.store.ListPrefix("created-order.v1.")
	if err != nil {
		return err
	}
	for key, raw := range orders {
		var record OrderRecord
		if json.Unmarshal(raw, &record) != nil || record.ID == "" {
			return fmt.Errorf("本地订单记录无效：%s", key)
		}
		copy := record
		s.inventory.orders[key] = &copy
	}
	stops, err := s.store.ListPrefix("sale-stopped.v1.")
	if err != nil {
		return err
	}
	for key, raw := range stops {
		matchID := strings.TrimPrefix(key, "sale-stopped.v1.")
		if len(matchID) > 0 && string(raw) == "1" {
			s.inventory.saleStops[matchID] = true
		}
	}
	return nil
}

// Execution flags live only for this client process; restart begins stopped.
func (s *Service) setManualRunLocked(id int64, r *inventoryRun) {
	if s.inventory.manual == nil {
		s.inventory.manual = map[int64]manualRunState{}
	}
	s.inventory.manual[id] = manualRunState{TaskID: id, Match: r.match, Status: r.status, Message: r.message, Realtime: r.realtime}
	if r.status == "order_blocked" {
		delete(s.inventory.pending, id)
	}
}

func (s *Service) reconcileMatchRunsLocked(match protocol.MatchConfig) {
	for id, r := range s.inventory.runs {
		if r.match.ID != match.ID && !r.orderInFlight && r.status != "order_verifying" && r.status != "order_blocked" && s.inventory.pending[id] == nil {
			if r.cancel != nil {
				r.cancel()
			}
			delete(s.inventory.runs, id)
		}
	}
}

func (s *Service) orderSnapshotLocked(id int64, r *inventoryRun, p inventory.Plan) orderAttempt {
	viewers := append([]OrderViewer(nil), r.orderPeople.viewers...)
	planCopy := p
	planCopy.Parts = append([]inventory.Allocation{}, p.Parts...)
	return orderAttempt{Account: r.business.binding.Protocol + ":" + r.business.binding.Account.Ref, TaskID: id, Match: r.match, ViewerIDs: append([]string{}, r.task.PeopleSelected...), Viewers: viewers, Plan: planCopy, Token: r.orderPeople.session.Token, CreatedAt: s.calibratedNowLocked()}
}

// Keep the attempt snapshot in memory for response handling only.
func (s *Service) rememberSubmissionLocked(p orderAttempt) {
	if s.inventory.pending == nil {
		s.inventory.pending = map[int64]*orderAttempt{}
	}
	s.inventory.pending[p.TaskID] = &p
}

func (s *Service) clearSubmissionLocked(id int64) { delete(s.inventory.pending, id) }

// Returns false only if key expiry requires another key fetch before any send.
func (s *Service) submitInventoryOrder(ctx context.Context, id int64, r *inventoryRun, key userkey.Material) bool {
	s.mu.Lock()
	if ctx.Err() != nil || !s.validInventoryLocked(id, r) || r.plan == nil || r.status != "pending_order" || r.orderInFlight {
		s.mu.Unlock()
		return true
	}
	offlineGrace := r.business != nil && !r.business.offlineSince.IsZero() && time.Since(r.business.offlineSince) < 18*time.Hour
	if !key.Valid(time.Now()) && !(offlineGrace && key.Present()) {
		s.mu.Unlock()
		return false
	}
	err := r.checkOrderPeople()
	var body orders.Body
	if err == nil {
		body, err = r.orderPeople.people.FromPlan(*r.plan)
	}
	var request orders.EncodedRequest
	var path string
	if err == nil {
		request, path, err = orders.BuildEncodedWithExpiredKey(body, key, r.orderPeople.session, s.calibratedNowLocked(), offlineGrace)
	}
	if err != nil {
		if !key.Valid(time.Now()) && !(offlineGrace && key.Present()) {
			s.mu.Unlock()
			return false
		}
		r.status, r.message = "order_review", "订单参数准备失败："+err.Error()+"（未发送订单）"
		_ = s.tasks.AppendLog(id, "warning", r.message)
		s.mu.Unlock()
		return true
	}
	if s.orderClient == nil && !r.simulated {
		r.status, r.message = "order_review", "下单服务不可用（未发送订单）"
		s.mu.Unlock()
		return true
	}
	cfg, session, sender := s.session.BusinessConfig, r.orderPeople.session, s.orderClient
	submittedPlan := *r.plan
	submittedPlan.Parts = append([]inventory.Allocation{}, r.plan.Parts...)
	pending := s.orderSnapshotLocked(id, r, submittedPlan)
	s.rememberSubmissionLocked(pending)
	r.orderPrepared, r.orderInFlight, r.status, r.message = true, true, "submitting", "正在创建订单"
	r.orderDone = make(chan struct{})
	r.stopRequested = false
	if s.debugMode {
		if raw, marshalErr := request.OrderJSON(); marshalErr == nil {
			requestLabel := "N5"
			if r.simulated {
				requestLabel = "模拟验收拦截 N5（未发送）"
			}
			for _, message := range []string{
				"[DEBUG] " + requestLabel + " POST 下单请求链接：" + business.N5 + path,
				"[DEBUG] " + requestLabel + " POST 下单请求参数：" + string(raw),
			} {
				if err := s.tasks.AppendLog(id, "debug", message); err != nil && s.log != nil {
					s.log.Warn("order request task log persistence failed", "task_id", id)
				}
			}
		}
	}
	statusMessage := r.message
	s.queueExecutionLog(id, "info", statusMessage)
	s.mu.Unlock()
	callCtx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	var result business.OrderResult
	var sendErr error
	orderIP := ""
	if r.simulated {
		result, sendErr = s.simulationOrder(callCtx, id)
	} else {
		result, sendErr = sender.CreateOrder(ippool.WithExit(s.taskNetworkContext(callCtx, id), func(ip string) { orderIP = ip }), cfg, session, path, request)
	}
	cancel()
	s.mu.Lock()
	defer s.mu.Unlock()
	r.orderInFlight = false
	if r.orderDone != nil {
		close(r.orderDone)
		r.orderDone = nil
	}
	r.orderPrepared = false
	// A confirmed result is saved even if a stop/session change raced the response,
	// but stale callbacks never resume polling or overwrite a stopped run's UI.
	created := sendErr == nil && result.Code == 1 && result.OrderID != ""
	if created {
		s.clearSubmissionLocked(id)
		s.saveCreatedOrderLocked(id, r, submittedPlan, result.OrderID, session.Token, pending.Viewers, orderIP)
		record := *r.order
		owner := s.syncOwnerLocked()
		s.seedSyncOrderLocked(owner, &record, cfg)
		s.orderDetailWG.Add(1)
		go func() {
			defer s.orderDetailWG.Done()
			detailErr := s.refreshOrderDetail(cfg, &record)
			detail := record.Details
			if detailErr == nil {
				s.mu.Lock()
				if current, _ := s.findOrderLocked(record.TaskID, record.ID); current != nil {
					detail = current.Details
				}
				s.mu.Unlock()
			}
			s.completeSyncOrderDetail(owner, record, detail, detailErr)
		}()
	}
	if r.stopRequested || r.status == "stopping" {
		if s.ipPool != nil {
			s.ipPool.Release(id)
		}
		r.orderInFlight, r.orderPrepared = false, false
		if created {
			r.status, r.message = "order_created", "已停止／创建成功待支付，订单号："+result.OrderID
		} else if sendErr != nil {
			r.status, r.message, r.manualPaused = "order_verifying", "已停止／下单结果不明，请核实订单", "uncertain"
		} else if result.Code == 1 && result.OrderID == "" {
			r.status, r.message, r.manualPaused = "order_verifying", "已停止／订单待核实：成功码缺少有效订单 ID", "uncertain"
		} else if result.BlocksPurchase() {
			r.status, r.message = "order_blocked", result.Message
			s.setManualRunLocked(id, r)
		} else {
			r.status, r.message, r.manualPaused = "stopped", "已停止", ""
			s.clearSubmissionLocked(id)
		}
		_ = s.tasks.AppendLog(id, "info", r.message)
		return true
	}
	if ctx.Err() != nil || !s.validInventoryLocked(id, r) || r.status != "submitting" {
		return true
	}
	// A validated success and order ID always win over message classification.
	if created {
		r.status, r.message = "order_created", "创建成功／待支付，订单号："+result.OrderID
		_ = s.tasks.AppendLog(id, "success", r.message)
		return true
	}
	// Explicit purchase restrictions take precedence over code-based delays.
	if result.BlocksPurchase() {
		r.status, r.message = "order_blocked", result.Message
		r.retryAt = time.Time{}
		s.setManualRunLocked(id, r)
		_ = s.tasks.AppendLog(id, "warning", "购票任务已暂停："+result.Message)
		return true
	}
	if result.Code == 1 && result.OrderID == "" {
		r.status, r.message = "order_verifying", "订单核实中：返回成功码但缺少有效订单 ID，已暂停本任务库存查询及再次下单"
		r.retryAt = time.Time{}
		_ = s.tasks.AppendLog(id, "warning", r.message)
		return true
	}
	if sendErr != nil {
		var timeout interface{ Timeout() bool }
		if errors.As(sendErr, &timeout) && timeout.Timeout() {
			s.clearSubmissionLocked(id)
			s.orderCooldownReasonLocked(id, r, time.Second, "下单超时")
			return true
		}
		s.orderCooldownReasonLocked(id, r, time.Second, "下单结果不明")
		return true
	}
	s.clearSubmissionLocked(id)
	if result.Code == 5 {
		s.orderCooldownLocked(id, r, result.Delay)
		return true
	}
	if result.Code == 3 {
		delay := result.Delay
		if delay <= 0 {
			delay = 5600 * time.Millisecond
		}
		s.orderCooldownReasonLocked(id, r, delay, "门票数量不足")
		return true
	}
	delay := result.Delay
	if delay <= 0 {
		delay = time.Second
	}
	reason := fmt.Sprintf("下单未成功（业务码 %d）", result.Code)
	if result.Message != "" {
		reason += "：" + result.Message
	}
	s.orderCooldownReasonLocked(id, r, delay, reason)
	return true
}

func (s *Service) saveCreatedOrderLocked(id int64, r *inventoryRun, p inventory.Plan, orderID, token string, viewers []OrderViewer, orderIPs ...string) {
	p.Parts = append([]inventory.Allocation{}, p.Parts...)
	orderIP := ""
	if len(orderIPs) > 0 {
		orderIP = orderIPs[0]
	}
	record := &OrderRecord{OrderIP: orderIP, Account: r.business.binding.Protocol + ":" + r.business.binding.Account.Ref, UserID: r.business.session.UserID, ID: orderID, TaskID: id, Match: r.match, ViewerIDs: append([]string{}, r.task.PeopleSelected...), Viewers: append([]OrderViewer{}, viewers...), Plan: p, CreatedAt: s.calibratedNowLocked(), Token: token}
	if r.simulated {
		record.Simulated, record.Account, record.Token = true, "simulation", "LOCAL-SIMULATION"
		record.Details = simulatedOrderDetail(orderID, viewers, p, time.Now())
		if rand.Intn(100) < s.simulation.PaidOrderPercent {
			record.Details.Status, record.Details.Payed = 6, record.Details.Payable
			for i := range record.Details.Bill {
				record.Details.Bill[i].Status = 6
			}
		}
	}
	r.order = record
	if s.store != nil {
		if s.inventory.orders == nil {
			s.inventory.orders = map[string]*OrderRecord{}
		}
		raw, err := json.Marshal(record)
		key := fmt.Sprintf("created-order.v1.%d.%s", id, orderID)
		if err == nil {
			err = s.store.Put(key, raw)
		}
		if err != nil {
			_ = s.tasks.AppendLog(id, "warning", "订单已创建，订单记录保存失败，请记录订单号："+orderID)
		} else {
			copy := *record
			s.inventory.orders[key] = &copy
			delete(s.inventory.pending, id)
			delete(s.inventory.manual, id)
		}
	}
}

func (s *Service) GetOrderHistory() ([]OrderDisplay, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return nil, err
	}
	if err := s.loadOrderStateLocked(); err != nil {
		return nil, err
	}
	items := make([]OrderDisplay, 0, len(s.inventory.orders))
	for _, record := range s.inventory.orders {
		copy := OrderDisplay{OrderIP: record.OrderIP, ID: record.ID, TaskID: record.TaskID, Match: record.Match, Plan: record.Plan, CreatedAt: record.CreatedAt, Details: record.Details, DetailError: record.DetailError, Simulated: record.Simulated}
		copy.ViewerIDs = append([]string{}, record.ViewerIDs...)
		copy.Viewers = append([]OrderViewer{}, record.Viewers...)
		for i := range copy.Viewers {
			copy.Viewers[i].Phone = ""
		}
		copy.Plan.Parts = append([]inventory.Allocation{}, record.Plan.Parts...)
		items = append(items, copy)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	return items, nil
}

func (s *Service) findOrderLocked(taskID int64, id string) (*OrderRecord, string) {
	key := fmt.Sprintf("created-order.v1.%d.%s", taskID, id)
	if r := s.inventory.orders[key]; r != nil {
		return r, key
	}
	return nil, key
}
func (s *Service) persistOrderDetailLocked(record *OrderRecord, key string, detail business.OrderDetail, detailErr string) error {
	record.Details = detail
	record.DetailError = detailErr
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	if err = s.store.Put(key, raw); err != nil {
		return err
	}
	s.inventory.orders[key] = record
	return nil
}
func (s *Service) refreshOrderDetail(cfg business.Config, record *OrderRecord) error {
	if record == nil {
		return errors.New("订单记录不存在")
	}
	if record.Simulated {
		operationErr := s.simulationOperation(s.ctx, record.TaskID, "query")
		s.mu.Lock()
		defer s.mu.Unlock()
		current, key := s.findOrderLocked(record.TaskID, record.ID)
		if current == nil {
			return errors.New("订单记录不存在")
		}
		if operationErr != nil {
			current.DetailError = operationErr.Error()
			raw, _ := json.Marshal(current)
			_ = s.store.Put(key, raw)
			return operationErr
		}
		return s.persistOrderDetailLocked(current, key, current.Details, "")
	}
	if s.businessClient == nil {
		return errors.New("订单服务不可用")
	}
	detail, err := s.businessClient.MatchOrderInfo(s.taskNetworkContext(s.ctx, record.TaskID), cfg, business.Session{Token: record.Token, UserID: record.UserID}, record.ID)
	s.mu.Lock()
	defer s.mu.Unlock()
	current, key := s.findOrderLocked(record.TaskID, record.ID)
	if current == nil {
		return errors.New("订单记录不存在")
	}
	if err != nil {
		current.DetailError = err.Error()
		raw, _ := json.Marshal(current)
		_ = s.store.Put(key, raw)
		return err
	}
	return s.persistOrderDetailLocked(current, key, detail, "")
}
func (s *Service) RefreshOrderDetail(taskID int64, id string) error {
	s.mu.Lock()
	if err := s.requireTaskSession(); err != nil {
		s.mu.Unlock()
		return err
	}
	record, _ := s.findOrderLocked(taskID, id)
	if record == nil {
		s.mu.Unlock()
		return errors.New("订单记录不存在")
	}
	copy := *record
	if copy.Simulated {
		s.mu.Unlock()
		return s.refreshOrderDetail(business.Config{}, &copy)
	}
	cfg := s.session.BusinessConfig
	s.mu.Unlock()
	return s.refreshOrderDetail(cfg, &copy)
}
func (s *Service) OrderAfterSale(taskID int64, id, action string) error {
	if action != "cancel" && action != "refund" {
		return errors.New("售后操作无效")
	}
	s.mu.Lock()
	if err := s.requireTaskSession(); err != nil {
		s.mu.Unlock()
		return err
	}
	record, _ := s.findOrderLocked(taskID, id)
	if record == nil {
		s.mu.Unlock()
		return errors.New("订单记录不存在")
	}
	if s.orderActions == nil {
		s.orderActions = map[string]bool{}
	}
	key := fmt.Sprintf("%d:%s", taskID, id)
	if s.orderActions[key] {
		s.mu.Unlock()
		return errors.New("该订单操作正在处理中")
	}
	s.orderActions[key] = true
	copy := *record
	if copy.Simulated {
		s.mu.Unlock()
		defer func() { s.mu.Lock(); delete(s.orderActions, key); s.mu.Unlock() }()
		if err := s.refreshOrderDetail(business.Config{}, &copy); err != nil {
			return err
		}
		if err := s.simulationOperation(s.ctx, taskID, action); err != nil {
			return err
		}
		s.mu.Lock()
		current, _ := s.findOrderLocked(taskID, id)
		if current == nil {
			s.mu.Unlock()
			return errors.New("订单记录不存在")
		}
		copy = *current
		s.mu.Unlock()
		if action == "cancel" && copy.Details.Status == 5 {
			copy.Details.Status = 4
		} else if action == "refund" && copy.Details.Status == 6 && copy.Details.PayType == "微信支付" && copy.Details.RefundEnabled {
			copy.Details.Status = 3
			copy.Details.Refund = copy.Details.Payable
			copy.Details.RefundEnabled = false
			copy.Details.Refunded = append([]business.OrderTicket{}, copy.Details.Bill...)
			for i := range copy.Details.Refunded {
				copy.Details.Refunded[i].Status = 3
			}
			copy.Details.Bill = []business.OrderTicket{}
		} else {
			return errors.New("模拟订单当前状态不符合操作条件")
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		current, orderKey := s.findOrderLocked(taskID, id)
		if current == nil {
			return errors.New("订单记录不存在")
		}
		return s.persistOrderDetailLocked(&copy, orderKey, copy.Details, "")
	}
	cfg := s.session.BusinessConfig
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.orderActions, key)
		s.mu.Unlock()
	}()
	detail := copy.Details
	if detail.ID == 0 {
		return errors.New("请先刷新订单详情")
	}
	if action == "cancel" && detail.Status != 5 {
		return errors.New("订单当前不是待支付状态，不能取消")
	}
	if action == "refund" && (detail.Status != 6 || detail.PayType != "微信支付" || !detail.RefundEnabled) {
		return errors.New("订单当前不符合整单退款条件")
	}
	snap := copy
	var err error
	if action == "cancel" {
		err = s.businessClient.CancelMatchOrder(s.taskNetworkContext(s.ctx, taskID), cfg, business.Session{Token: snap.Token, UserID: snap.UserID}, detail.ID)
	} else {
		err = s.businessClient.RefundMatchOrder(s.taskNetworkContext(s.ctx, taskID), cfg, business.Session{Token: snap.Token, UserID: snap.UserID}, detail.ID)
	}
	if err != nil {
		_ = s.refreshOrderDetail(cfg, &snap)
		return fmt.Errorf("操作结果待确认：%w", err)
	}
	return s.refreshOrderDetail(cfg, &snap)
}

func (s *Service) DeleteOrderRecord(taskID int64, orderID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	key := fmt.Sprintf("created-order.v1.%d.%s", taskID, orderID)
	if s.inventory.orders[key] == nil {
		return errors.New("订单记录不存在")
	}
	if err := s.store.Delete(key); err != nil {
		return errors.New("订单记录删除失败")
	}
	delete(s.inventory.orders, key)
	return nil
}

func (s *Service) DeleteAllOrderRecords() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	for _, active := range s.orderActions {
		if active {
			return errors.New("订单操作正在处理中，请稍后再删除")
		}
	}
	if err := s.store.DeletePrefix("created-order.v1."); err != nil {
		return errors.New("订单记录删除失败")
	}
	s.inventory.orders = map[string]*OrderRecord{}
	return nil
}

func (s *Service) RecoverInventory(id int64, action string) error {
	s.mu.Lock()
	if err := s.requireTaskSession(); err != nil {
		s.mu.Unlock()
		return err
	}
	r := s.inventory.runs[id]
	if r == nil || r.orderInFlight || r.status == "stopping" {
		s.mu.Unlock()
		return errors.New("任务仍有在途下单请求，暂不能恢复")
	}
	if s.inventory.saleStops[s.session.CurrentMatch.ID] {
		s.mu.Unlock()
		return errors.New("本场已停止售票")
	}
	switch r.status {
	case "order_verifying":
		if action != "confirm-not-created" {
			s.mu.Unlock()
			return errors.New("请先确认官方购票端未创建订单")
		}
	case "order_blocked":
		if action != "resolved" {
			s.mu.Unlock()
			return errors.New("请先确认购票暂停原因已处理")
		}
	default:
		s.mu.Unlock()
		return errors.New("当前任务状态不需要人工恢复")
	}
	delete(s.inventory.manual, id)
	delete(s.inventory.pending, id)
	r.status, r.message, r.manualPaused = "stopped", "已停止", ""
	_ = s.tasks.AppendLog(id, "info", "代理商已解除任务暂停；任务保持停止，需再次点击启动才恢复库存监控")
	s.mu.Unlock()
	return nil
}

func (s *Service) PrepareRepurchase(id int64, viewerIDs, seats []string, mode string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	r := s.inventory.runs[id]
	if r == nil || r.status != "order_created" || r.order == nil {
		return errors.New("当前任务没有可再次购票的成功订单")
	}
	if r.orderInFlight {
		return errors.New("订单请求仍在处理")
	}
	if s.inventory.saleStops[r.match.ID] {
		return errors.New("本场已停止售票")
	}
	taskData, err := s.tasks.Business(id)
	if err != nil {
		return err
	}
	available := map[string]bool{}
	for _, person := range taskData.People {
		available[person.ID] = person.Available()
	}
	if len(viewerIDs) == 0 {
		return errors.New("请选择至少一位观演人")
	}
	for _, personID := range viewerIDs {
		if !available[personID] {
			return errors.New("所选观演人已不存在或不可用，请刷新后重新选择")
		}
	}
	if len(seats) == 0 {
		return errors.New("请选择有效座位要求")
	}
	if err := s.tasks.SavePurchaseConfiguration(id, mode, seats, viewerIDs); err != nil {
		return err
	}
	s.setManualRunLocked(id, &inventoryRun{match: r.match, status: "stopped", message: "再次购票配置已保存，等待手动启动", realtime: true})
	updated := s.tasks.Snapshot()
	for _, task := range updated.Tasks {
		if task.ID == id {
			r.task = task
			break
		}
	}
	r.status, r.message, r.order = "stopped", "再次购票配置已保存，点击启动开始新一轮", nil
	_ = s.tasks.AppendLog(id, "info", "再次购票配置已保存，等待手动启动新一轮")
	r.cancel()
	return nil
}

func (s *Service) orderCooldownLocked(id int64, r *inventoryRun, delay time.Duration) {
	s.orderCooldownReasonLocked(id, r, delay, "抢座失败")
}

func (s *Service) orderCooldownReasonLocked(id int64, r *inventoryRun, delay time.Duration, reason string) {
	if delay <= 0 {
		delay = 3 * time.Second
	}
	r.plan = nil
	r.early = false
	r.status = "order_cooldown"
	r.retryAt = s.calibratedNowLocked().Add(delay)
	r.message = fmt.Sprintf("%s，等待 %g 秒后恢复库存查询", reason, delay.Seconds())
	_ = s.tasks.AppendLog(id, "warning", r.message)
	s.inventory.wg.Add(1)
	go func() {
		defer s.inventory.wg.Done()
		if !waitInventory(r.ctx, delay) {
			return
		}
		s.mu.Lock()
		if !s.validInventoryLocked(id, r) || r.status != "order_cooldown" {
			s.mu.Unlock()
			return
		}
		r.queryError = ""
		r.retryAt = time.Time{}
		r.status, r.message = "monitoring", "等待结束，恢复库存查询"
		qctx, cancel := context.WithCancel(r.ctx)
		r.queryCancel = cancel
		_ = s.tasks.AppendLog(id, "info", r.message)
		s.mu.Unlock()
		s.pollInventory(id, r, qctx, time.Now())
	}()
}
