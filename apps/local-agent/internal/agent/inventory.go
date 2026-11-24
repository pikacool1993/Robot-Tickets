package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/tasks"
	"sync"
	"time"
)

type inventoryRun struct {
	simulationQueries int
	simulated         bool
	queryAt           time.Time
	ctx               context.Context
	cancel            context.CancelFunc
	queryCancel       context.CancelFunc
	business          *businessRun
	status            string
	message           string
	queryError        string
	buttonHint        string
	target            time.Time
	plan              *inventory.Plan
	task              tasks.Task
	catalog           tasks.Catalog
	matcher           *inventory.Matcher
	defaults          []inventory.Region
	match             protocol.MatchConfig
	people            []business.Person
	orderPeople       *orderPeopleSnapshot
	orderPrepared     bool
	orderInFlight     bool
	orderDone         chan struct{}
	order             *OrderRecord
	stopRequested     bool
	manualPaused      string
	retryAt           time.Time
	early             bool
	realtime          bool
}
type ExecutionView struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Target  time.Time       `json:"target"`
	Plan    *inventory.Plan `json:"plan,omitempty"`
	Order   *OrderDisplay   `json:"order,omitempty"`
	RetryAt time.Time       `json:"retryAt"`
}
type InventoryView struct {
	AutoRefresh AutoRefreshSettings       `json:"autoRefresh"`
	RefreshRuns map[int64]AutoRefreshView `json:"refreshRuns"`
	Match       protocol.MatchConfig      `json:"match"`
	Settings    inventory.Settings        `json:"settings"`
	ClockReady  bool                      `json:"clockReady"`
	ClockError  string                    `json:"clockError"`
	OffsetMS    int64                     `json:"offsetMS"`
	Now         time.Time                 `json:"now"`
	Runs        map[int64]ExecutionView   `json:"runs"`
	SaleStopped bool                      `json:"saleStopped"`
	Simulation  SimulationSettings        `json:"simulation"`
	DebugMode   bool                      `json:"debugMode"`
}
type inventoryState struct {
	settings  inventory.Settings
	runs      map[int64]*inventoryRun
	wg        sync.WaitGroup
	synced    bool
	anchor    time.Time
	wall      time.Time
	offset    int64
	syncError string
	syncing   bool
	orders    map[string]*OrderRecord
	pending   map[int64]*orderAttempt
	saleStops map[string]bool
	manual    map[int64]manualRunState
}

func (s *Service) initInventory() error {
	if err := s.initAutoRefresh(); err != nil {
		return err
	}
	s.inventory.settings = inventory.Defaults()
	s.inventory.runs = map[int64]*inventoryRun{}
	s.inventory.orders = map[string]*OrderRecord{}
	s.inventory.pending = map[int64]*orderAttempt{}
	s.inventory.saleStops = map[string]bool{}
	s.inventory.manual = map[int64]manualRunState{}
	s.simulation = defaultSimulationSettings()
	if raw, err := s.store.Get("simulation-settings.v1"); err != nil {
		return err
	} else if len(raw) > 0 {
		s.simulation.OrderFailurePercents = nil
		if json.Unmarshal(raw, &s.simulation) != nil || s.simulation.Validate() != nil {
			return errors.New("模拟设置无效")
		}
		s.simulation = s.simulation.withOrderProbabilities()
	}
	raw, err := s.store.Get("inventory-settings.v1")
	if err != nil {
		return err
	}
	if len(raw) > 0 {
		if json.Unmarshal(raw, &s.inventory.settings) != nil || s.inventory.settings.Validate() != nil {
			return errors.New("库存查询设置无效，请检查本地数据")
		}
	}
	if err := s.loadOrderStateLocked(); err != nil {
		return err
	}
	return nil
}
func (s *Service) calibratedNowLocked() time.Time {
	if !s.inventory.synced {
		return time.Now()
	}
	return s.inventory.wall.Add(time.Since(s.inventory.anchor))
}
func (s *Service) InventoryView() (InventoryView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return InventoryView{}, err
	}
	out := InventoryView{Match: s.session.CurrentMatch, Settings: s.inventory.settings, ClockReady: s.inventory.synced, ClockError: s.inventory.syncError, OffsetMS: s.inventory.offset, Now: s.calibratedNowLocked(), Runs: map[int64]ExecutionView{}, Simulation: s.simulation, DebugMode: s.debugMode, AutoRefresh: s.autoRefresh, RefreshRuns: s.autoRefreshViewsLocked(s.refreshTime())}
	if s.inventory.saleStops[out.Match.ID] {
		out.SaleStopped = true
	}
	s.reconcileMatchRunsLocked(out.Match)
	for id, r := range s.inventory.runs {
		// A detached DTO prevents callers from observing mutable run state.
		v := ExecutionView{Status: r.status, Message: r.message, Target: r.target}
		v.RetryAt = r.retryAt
		if r.order != nil {
			copy := OrderDisplay{ID: r.order.ID, TaskID: r.order.TaskID, Match: r.order.Match, ViewerIDs: append([]string{}, r.order.ViewerIDs...), Viewers: append([]OrderViewer{}, r.order.Viewers...), Plan: r.order.Plan, CreatedAt: r.order.CreatedAt}
			copy.Plan.Parts = append([]inventory.Allocation{}, r.order.Plan.Parts...)
			for i := range copy.Viewers {
				copy.Viewers[i].Phone = ""
			}
			v.Order = &copy
		}
		if r.plan != nil {
			p := *r.plan
			p.Parts = append([]inventory.Allocation{}, r.plan.Parts...)
			v.Plan = &p
		}
		out.Runs[id] = v
	}
	return out, nil
}
func (s *Service) SaveInventorySettings(v inventory.Settings) error {
	if err := v.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	raw, _ := json.Marshal(v)
	if err := s.store.Put("inventory-settings.v1", raw); err != nil {
		return errors.New("查询设置保存失败")
	}
	s.inventory.settings = v
	return nil
}
func (s *Service) SyncInventoryClock() error {
	s.mu.Lock()
	if err := s.requireTaskSession(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.inventory.syncing {
		s.mu.Unlock()
		return errors.New("正在校准时间，请稍候")
	}
	s.inventory.syncing = true
	client, session := s.client, s.session
	s.mu.Unlock()
	start := time.Now()
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	defer cancel()
	var response struct {
		MS int64 `json:"unix_ms"`
	}
	err := client.Call(ctx, "GET", "/time", session.Token, nil, &response)
	end := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inventory.syncing = false
	if s.session != session {
		return errors.New("登录会话已变化，请重新校准")
	}
	if err != nil || response.MS <= 0 {
		s.inventory.syncError = "时间同步失败，可重试校准"
		if err != nil {
			return err
		}
		return errors.New(s.inventory.syncError)
	}
	wall := time.UnixMilli(response.MS).Add(end.Sub(start) / 2)
	s.inventory.synced = true
	s.inventory.wall = wall
	s.inventory.anchor = end
	s.inventory.offset = wall.Sub(end).Milliseconds()
	s.inventory.syncError = ""
	return nil
}
func (s *Service) inventoryActiveLocked(id int64) bool {
	r := s.inventory.runs[id]
	if r != nil && r.status == "maintenance_failed" {
		return true
	}
	return r != nil && (r.orderInFlight || r.status == "maintenance" || r.status == "waiting" || r.status == "monitoring" || r.status == "ready" || r.status == "pending_order" || r.status == "submitting" || r.status == "order_cooldown" || r.status == "order_review" || r.status == "order_verifying" || r.status == "order_blocked")
}
func (s *Service) requireInventoryStoppedLocked(id int64) error {
	if s.inventoryActiveLocked(id) {
		return errors.New("请先停止任务，再修改执行配置")
	}
	return nil
}
func (s *Service) validInventoryLocked(id int64, r *inventoryRun) bool {
	if r.simulated {
		return s.inventory.runs[id] == r && r.ctx.Err() == nil
	}
	if r.business != nil && !r.business.offlineSince.IsZero() && time.Since(r.business.offlineSince) >= 18*time.Hour {
		return false
	}
	return s.inventory.runs[id] == r && r.ctx.Err() == nil && r.business != nil && s.currentBusinessLocked(id, r.business) && !r.business.busy && !r.business.refresh.invalid
}
func (s *Service) stopInventoryLocked(id int64, status, message string) {
	if r := s.inventory.runs[id]; r != nil {
		if r.status == "order_created" || r.status == "order_verifying" || r.status == "order_blocked" {
			return
		}
		if r.orderInFlight {
			r.stopRequested = true
			if r.queryCancel != nil {
				r.queryCancel()
			}
			s.stopOrderKeysLocked(r.business)
			if r.cancel != nil {
				r.cancel()
			}
			r.status, r.message = "stopping", "正在停止，等待下单结果"
			return
		}
		if r.business != nil && !r.business.busy && !r.business.refreshReady && !r.business.refresh.active {
			s.stopOrderKeysLocked(r.business)
		}
		r.cancel()
		r.queryCancel()
		r.status = status
		r.message = message
		r.plan = nil
		r.orderPrepared = false
		r.retryAt = time.Time{}
		r.stopRequested = false
		if s.inventory.pending[id] != nil {
			r.status, r.message = "order_verifying", "已停止／下单结果不明，请核实订单"
		}
	}
}
func (s *Service) cancelInventoryLocked() {
	for id := range s.inventory.runs {
		s.stopInventoryLocked(id, "stopped", "已停止")
	}
}
func (s *Service) reconcileInventoryLocked() {
	for id, r := range s.inventory.runs {
		if r.status == "order_created" || r.status == "order_verifying" || r.status == "order_blocked" || r.status == "order_review" || r.status == "stopping" {
			continue
		}
		if s.inventoryActiveLocked(id) && r.status != "maintenance" && r.status != "maintenance_failed" && !s.validInventoryLocked(id, r) {
			s.stopInventoryLocked(id, "paused", "账号或业务状态变化，请处理后重新启动")
			_ = s.tasks.AppendLog(id, "warning", "库存监控已暂停：账号或业务状态变化")
		}
	}
}
func (s *Service) StartInventory(id int64) error {
	return s.startInventory(id, false)
}

func (s *Service) startInventory(id int64, realtime bool) error {
	return s.startInventoryMode(id, realtime, false)
}
func (s *Service) startInventoryMode(id int64, realtime, maintenance bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.startInventoryModeLocked(id, realtime, maintenance)
}
func (s *Service) startInventoryModeLocked(id int64, realtime, maintenance bool) error {
	if s.debugMode && s.simulation.Enabled {
		return errors.New("模拟模式已开启，请使用任务卡片的模拟验收入口")
	}
	for _, run := range s.inventory.runs {
		if run.simulated && s.inventoryActiveLocked(run.task.ID) {
			return errors.New("请先停止模拟任务，再启动真实监控")
		}
	}
	if maintenance {
		if !s.maintenanceResume[id] {
			return nil
		}
		s.maintenanceResume[id] = false
	}
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	s.reconcileMatchRunsLocked(s.session.CurrentMatch)
	if s.inventoryActiveLocked(id) {
		return errors.New("任务已启动")
	}
	if r := s.inventory.runs[id]; r != nil && r.order != nil && r.order.Match.ID == s.session.CurrentMatch.ID {
		return errors.New("请先使用“再次购票”准备新一轮")
	}
	if s.inventory.pending[id] != nil {
		return errors.New("订单结果待核实，请先人工核实")
	}
	if !s.inventory.synced {
		return errors.New("尚未完成时间校准，请先校准")
	}
	if err := s.session.CurrentMatch.Ready(); err != nil {
		return err
	}
	if s.inventory.saleStops[s.session.CurrentMatch.ID] {
		return errors.New("本场已停止售票")
	}
	b := s.businessRuns[id]
	if b == nil || !s.currentBusinessLocked(id, b) || b.busy || b.refresh.active || b.refresh.invalid || b.refresh.blocked || b.refreshReady || b.session.Token == "" {
		return errors.New("请先确保账号在线并完成业务登录")
	}
	if !b.offlineSince.IsZero() && time.Since(b.offlineSince) >= 18*time.Hour {
		return errors.New("协议离线或授权失效已满18小时，请重新授权登录")
	}
	view := s.tasks.Snapshot()
	var task *tasks.Task
	for _, t := range view.Tasks {
		if t.ID == id {
			copy := t
			task = &copy
			break
		}
	}
	if task == nil {
		return errors.New("任务不存在")
	}
	if view.ConfigError != "" || !task.SeatValid || len(task.Selected) == 0 {
		return errors.New("请先配置有效座位要求")
	}
	if !task.PeopleUsable || len(task.PeopleSelected) == 0 {
		return errors.New("请先选择可用观演人")
	}
	peopleData, err := s.tasks.Business(id)
	if err != nil {
		return err
	}
	available := map[string]bool{}
	for _, p := range peopleData.People {
		available[p.ID] = p.Available()
	}
	for _, selected := range task.PeopleSelected {
		if !available[selected] {
			return errors.New("已选观演人已不存在或不可用，请重新选择")
		}
	}
	monotonicStart := time.Now()
	now := s.calibratedNowLocked()
	query, target, early := inventory.Schedule(now, s.session.CurrentMatch.SaleAt, s.inventory.settings)
	if state, ok := s.inventory.manual[id]; realtime || (ok && state.Match.ID == s.session.CurrentMatch.ID && state.Status == "stopped" && state.Realtime) {
		realtime = true
		query, target, early = now, now, false
	}
	matcher := inventory.NewMatcher(view.Catalog, *task)
	var defaults []inventory.Region
	if early {
		raw, err := readDefaultStock(filepath.Join(s.root, "config", "bill-regions.json"))
		if err != nil {
			return errors.New("默认库存文件无法读取，请检查 config/bill-regions.json")
		}
		defaults, err = inventory.Parse(raw)
		if err != nil {
			return fmt.Errorf("默认库存无效：%w", err)
		}
		if _, ok := matcher.Match(defaults, len(task.PeopleSelected), task.AllowCross, func(int64) int64 { return 0 }); !ok {
			return errors.New("默认库存无法满足本任务人数或座位要求，不能启动")
		}
	}
	if old := s.inventory.runs[id]; old != nil {
		old.cancel()
		old.queryCancel()
	}
	s.setManualRunLocked(id, &inventoryRun{match: s.session.CurrentMatch, status: "stopped", realtime: realtime})
	ctx, cancel := context.WithCancel(s.ctx)
	qctx, qcancel := context.WithCancel(ctx)
	r := &inventoryRun{ctx: ctx, cancel: cancel, queryCancel: qcancel, business: b, status: "waiting", message: "等待开售后第1秒查询", target: target, task: *task, catalog: view.Catalog, defaults: defaults, match: s.session.CurrentMatch, early: early, queryAt: query}
	r.people = peopleData.People
	if err := r.prepareOrderPeople(); err != nil {
		cancel()
		return err
	}
	r.matcher = matcher
	s.inventory.runs[id] = r
	s.startOrderKeysLocked(id, b)
	queryDeadline := monotonicStart.Add(query.Sub(now))
	targetDeadline := monotonicStart.Add(target.Sub(now))
	// Durations are captured under the calibration snapshot and timers use monotonic time.
	s.inventory.wg.Add(1)
	go func() { defer s.inventory.wg.Done(); s.pollInventory(id, r, qctx, queryDeadline) }()
	if early {
		s.inventory.wg.Add(1)
		go func() {
			defer s.inventory.wg.Done()
			if waitInventory(ctx, time.Until(targetDeadline)) {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.inventoryDueLocked(id, r)
			}
		}()
	}
	_ = s.tasks.AppendLog(id, "info", "库存任务已启动，按首次目标时间提交完整订单")
	return nil
}
func waitInventory(ctx context.Context, d time.Duration) bool {
	if d < 0 {
		d = 0
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return ctx.Err() == nil
	}
}
func (s *Service) StopInventory(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if s.maintenanceResume != nil {
		s.maintenanceResume[id] = false
	}
	if b := s.businessRuns[id]; b != nil {
		b.resumeEpoch++
		b.resumePending = false
		b.resumeNotBefore = time.Time{}
	}
	r := s.inventory.runs[id]
	if r != nil && (r.status == "order_created" || r.status == "order_verifying" || r.status == "order_blocked") {
		return nil
	}
	if r != nil && r.orderInFlight {
		r.stopRequested = true
		r.queryCancel()
		s.stopOrderKeysLocked(r.business)
		r.cancel()
		r.status, r.message = "stopping", "正在停止，等待下单结果"
		return s.tasks.AppendLog(id, "info", r.message)
	}
	s.stopInventoryLocked(id, "stopped", "已停止")
	return s.tasks.AppendLog(id, "info", "库存任务已停止，已清除本轮方案与定时")
}

func (s *Service) SetSaleStopped(stopped bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	match := s.session.CurrentMatch
	if s.inventory.saleStops == nil {
		s.inventory.saleStops = map[string]bool{}
	}
	key := "sale-stopped.v1." + match.ID
	if stopped {
		if err := s.store.Put(key, []byte("1")); err != nil {
			return errors.New("本场停止售票状态保存失败")
		}
		s.inventory.saleStops[match.ID] = true
		for id, r := range s.inventory.runs {
			if r.match.ID == match.ID {
				s.maintenanceResume[id] = false
			}
			if r.match.ID != match.ID || !s.inventoryActiveLocked(id) {
				continue
			}
			if r.orderInFlight {
				r.stopRequested = true
				r.queryCancel()
				s.stopOrderKeysLocked(r.business)
				r.cancel()
				r.status, r.message = "stopping", "正在停止，等待下单结果"
			} else {
				s.stopInventoryLocked(id, "stopped", "本场已停止售票")
			}
		}
	} else {
		if err := s.store.Delete(key); err != nil {
			return errors.New("本场停止售票状态清除失败")
		}
		delete(s.inventory.saleStops, match.ID)
	}
	return nil
}
func (s *Service) SaveTaskCross(id int64, value bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if err := s.requireInventoryStoppedLocked(id); err != nil {
		return err
	}
	return s.tasks.SaveCross(id, value)
}

func (s *Service) SaveTaskSeatRelaxation(id int64, enabled bool, afterSec int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if err := s.tasks.SaveSeatRelaxation(id, enabled, afterSec); err != nil {
		return err
	}
	if r := s.inventory.runs[id]; r != nil {
		r.task.RelaxSeats, r.task.RelaxAfterSec = enabled, afterSec
	}
	return nil
}

// Accessed under s.mu. Production runs compile at startup; lazy construction
// also supports runs restored/constructed by internal callers.
func (r *inventoryRun) stockMatcher() *inventory.Matcher {
	if r.matcher == nil {
		r.matcher = inventory.NewMatcher(r.catalog, r.task)
	}
	return r.matcher
}

func (s *Service) inventoryDueLocked(id int64, r *inventoryRun) {
	if !s.validInventoryLocked(id, r) || !r.early || (r.status != "waiting" && r.status != "monitoring" && r.status != "ready") {
		return
	}
	if r.plan == nil {
		plan, ok := r.stockMatcher().Match(r.defaults, len(r.task.PeopleSelected), r.task.AllowCross, nil)
		if !ok {
			s.stopInventoryLocked(id, "paused", "默认库存无法生成方案")
			return
		}
		plan.Source = "default"
		plan.MatchID = r.match.ID
		plan.ObservedAt = s.calibratedNowLocked()
		r.plan = &plan
	}
	s.stageInventoryOrderLocked(id, r)
}

func (s *Service) stageInventoryOrderLocked(id int64, r *inventoryRun) {
	if s.inventory.runs[id] != r || r.ctx.Err() != nil || r.plan == nil || (r.status != "ready" && r.status != "monitoring" && r.status != "waiting") {
		return
	}
	r.queryCancel()
	r.early = false
	r.status = "pending_order"
	r.message = "等待下单参数"
	if r.business != nil {
		s.waitOrderParametersLocked(id, r)
	}
}
func (s *Service) deliverInventoryLocked(source int64, regions []inventory.Region, at time.Time) {
	// All receivers match the same immutable snapshot with their own conditions.
	for id, r := range s.inventory.runs {
		if origin := s.inventory.runs[source]; origin != nil && origin.simulated != r.simulated {
			continue
		}
		if (r.status != "monitoring" && !(r.status == "waiting" && !s.calibratedNowLocked().Before(r.queryAt))) || !s.validInventoryLocked(id, r) {
			continue
		}
		matcher := r.stockMatcher()
		if r.task.RelaxSeats && !at.Before(r.match.SaleAt.Add(time.Duration(r.task.RelaxAfterSec)*time.Second)) {
			matcher = inventory.NewPriceMatcher(r.catalog, r.task)
		}
		plan, ok := matcher.Match(regions, len(r.task.PeopleSelected), r.task.AllowCross, nil)
		if !ok {
			continue
		}
		plan.Source = "notification"
		if source == id {
			plan.Source = "realtime"
		}
		plan.MatchID = r.match.ID
		plan.ObservedAt = at
		if s.acceptInventoryPlanLocked(id, r, plan) {
			message := fmt.Sprintf("库存匹配成功：%d人，来源 %s", len(r.task.PeopleSelected), plan.Source)
			s.queueExecutionLog(id, "success", message)
		}
	}
}

// Caller validates account/session eligibility under s.mu before reaching this state transition.
func (s *Service) acceptInventoryPlanLocked(id int64, r *inventoryRun, plan inventory.Plan) bool {
	if s.inventory.runs[id] != r || r.ctx.Err() != nil || r.plan != nil || (r.status != "monitoring" && r.status != "waiting") {
		return false
	}
	r.plan = &plan
	r.queryCancel()
	r.status = "ready"
	r.message = "已命中库存，等待首次提交时间"
	if !r.early {
		s.stageInventoryOrderLocked(id, r)
	}
	return true
}
func (s *Service) pollInventory(id int64, r *inventoryRun, ctx context.Context, deadline time.Time) {
	if !waitInventory(ctx, time.Until(deadline)) {
		return
	}
	for {
		s.mu.Lock()
		if ctx.Err() != nil || !s.validInventoryLocked(id, r) || r.plan != nil || (r.status != "waiting" && r.status != "monitoring") {
			s.mu.Unlock()
			return
		}
		r.status = "monitoring"
		r.message = "正在查询实时库存"
		cfg, session := s.session.BusinessConfig, r.business.session
		s.mu.Unlock()
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		var raw []byte
		var err error
		if r.simulated {
			raw, err = s.simulationStock(callCtx, id)
		} else {
			raw, err = s.businessClient.Stock(s.taskNetworkContext(callCtx, id), cfg, session, r.match.ID)
		}
		cancel()
		var stock inventory.StockResponse
		if err == nil {
			stock, err = inventory.ParseResponse(raw)
		}
		s.mu.Lock()
		if !s.validInventoryLocked(id, r) || ctx.Err() != nil || r.plan != nil {
			s.mu.Unlock()
			return
		}
		if err == nil {
			if r.queryError != "" {
				s.queueExecutionLog(id, "info", "库存查询已恢复")
			}
			r.queryError = ""
			canPurchase := stock.ButtonStatusPresent && stock.ButtonStatus == 1
			if canPurchase {
				if r.buttonHint != "" {
					s.queueExecutionLog(id, "info", "服务端按钮状态已恢复；继续监控库存")
					r.buttonHint = ""
				}
				r.message = "暂无符合条件的库存"
			} else {
				if stock.ButtonText != "" && stock.ButtonText != r.buttonHint {
					r.buttonHint = stock.ButtonText
					s.queueExecutionLog(id, "warning", "服务端提示："+r.buttonHint+"；库存监控继续")
				}
				hint := r.buttonHint
				if hint == "" {
					if stock.ButtonStatusPresent {
						hint = fmt.Sprintf("服务端按钮状态 %d", stock.ButtonStatus)
					} else {
						hint = "服务端未返回可购状态"
					}
				}
				r.message = "服务端提示：" + hint + "；继续查询库存"
			}
			// The official client only enables purchase while the server button is active.
			if canPurchase {
				for _, region := range stock.Regions {
					if region.Count > 0 {
						s.deliverInventoryLocked(id, stock.Regions, s.calibratedNowLocked())
						break
					}
				}
			}
		} else {
			s.recordInventoryQueryErrorLocked(id, r, err)
		}
		settings := s.inventory.settings
		delay := time.Duration(inventory.Between(settings.QueryMinMS, settings.QueryMaxMS)) * time.Millisecond
		done := r.plan != nil
		s.mu.Unlock()
		if done || !waitInventory(ctx, delay) {
			return
		}
	}
}

// Query errors only affect the hint. They never cancel the run or its default-stock deadline.
func (s *Service) recordInventoryQueryErrorLocked(id int64, r *inventoryRun, err error) {
	message := "库存查询异常，按配置间隔继续查询（不表示零库存）"
	var apiErr *business.APIError
	if errors.As(err, &apiErr) {
		message = fmt.Sprintf("库存查询异常（HTTP %d，业务码 %d），按配置间隔继续查询", apiErr.HTTP, apiErr.Code)
	}
	r.message = message
	if r.queryError != message {
		s.queueExecutionLog(id, "warning", message)
	}
	r.queryError = message
}

func (s *Service) inventoryClockLoop() {
	defer s.inventory.wg.Done()
	timer := time.NewTicker(time.Second)
	defer timer.Stop()
	next := time.Now()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			s.mu.Lock()
			ready := s.session != nil && s.state.LoggedIn
			s.mu.Unlock()
			if ready && !time.Now().Before(next) {
				_ = s.SyncInventoryClock()
				next = time.Now().Add(time.Minute)
			}
		}
	}
}

func readDefaultStock(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, (2<<20)+1))
}
