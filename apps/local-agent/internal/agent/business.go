package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/orders"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/userkey"
	"time"
)

type businessAPI interface {
	orderSender
	MatchOrderInfo(context.Context, business.Config, business.Session, string) (business.OrderDetail, error)
	CancelMatchOrder(context.Context, business.Config, business.Session, int64) error
	RefundMatchOrder(context.Context, business.Config, business.Session, int64) error
	Close()
	Stock(context.Context, business.Config, business.Session, string) ([]byte, error)
	RefreshWithLogin(context.Context, business.Config, string, business.Session, func(context.Context) (string, error), func(string, string), func(business.Session)) (business.Session, []business.Person, error)
}

type businessRun struct {
	refresh         refreshSchedule
	resumeNotBefore time.Time
	resumePending   bool
	resumeEpoch     uint64
	binding         protocols.Binding
	ctx             context.Context
	cancel          context.CancelFunc
	busy            bool
	session         business.Session
	attempted       bool
	keys            *userkey.Cache
	keyCancel       context.CancelFunc
	keyEpoch        uint64
	offlineSince    time.Time
	refreshReady    bool
}

type persistedBusinessSession struct {
	LastFullRefresh time.Time         `json:"lastFullRefresh"`
	Binding         protocols.Binding `json:"binding"`
	Token           string            `json:"token"`
	UserID          string            `json:"userId"`
	AuthCode        string            `json:"authCode"`
	Key             userkey.Snapshot  `json:"key"`
	HasKey          bool              `json:"hasKey"`
}

func (s *Service) persistBusinessSessionLocked(id int64, r *businessRun) {
	if s.store == nil || r == nil || r.session.Token == "" {
		return
	}
	data := persistedBusinessSession{Binding: r.binding, Token: r.session.Token, UserID: r.session.UserID, AuthCode: r.session.AuthCode, LastFullRefresh: r.refresh.lastSuccess}
	if r.keys != nil {
		if key, ok := r.keys.Peek(); ok {
			data.Key = key.Snapshot()
			data.HasKey = true
		}
	}
	raw, err := json.Marshal(data)
	if err == nil {
		_ = s.store.Put(fmt.Sprintf("business-session.v1.%d", id), raw)
	}
}

func sameBinding(a, b protocols.Binding) bool {
	return a.Protocol == b.Protocol && a.Account.Ref == b.Account.Ref && a.Generation == b.Generation
}
func (s *Service) cancelBusinessLocked() {
	for _, r := range s.businessRuns {
		s.stopOrderKeysLocked(r)
		if r.keys != nil {
			r.keys.Clear()
		}
		r.cancel()
	}
	s.orderKeys = nil
	s.businessRuns = map[int64]*businessRun{}
}
func (s *Service) businessLoop() {
	defer close(s.businessDone)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.mu.Lock()
			s.reconcileBusinessLocked()
			s.reconcileInventoryLocked()
			s.tickAutoRefreshLocked(s.refreshTime())
			s.mu.Unlock()
		}
	}
}
func (s *Service) reconcileBusinessLocked() {
	if s.session == nil || !s.state.LoggedIn || s.protocols == nil || !time.Now().Before(s.session.ExpiresAt) {
		return
	}
	bindings := s.protocols.Bindings()
	present := map[int64]bool{}
	for _, t := range s.tasks.Snapshot().Tasks {
		present[t.ID] = true
		b, ok := bindings[t.ID]
		if ok {
			s.authNoticeLocked(t, b)
		}
		r := s.businessRuns[t.ID]
		if r != nil && (!ok || !sameBinding(r.binding, b) || b.Paused || b.Protocol != s.state.AuthProtocol) {
			s.stopOrderKeysLocked(r)
			if r.keys != nil {
				r.keys.Clear()
			}
			r.cancel()
			delete(s.businessRuns, t.ID)
			r = nil
		}
		if !ok || b.Protocol != s.state.AuthProtocol {
			continue
		}
		var offlineRaw []byte
		if r == nil {
			offlineRaw, _ = s.store.Get(fmt.Sprintf("protocol-offline.v1.%d", t.ID))
		}
		if (b.Status == "expired" || len(offlineRaw) > 0) && r == nil {
			var saved persistedBusinessSession
			raw, _ := s.store.Get(fmt.Sprintf("business-session.v1.%d", t.ID))
			if json.Unmarshal(raw, &saved) == nil && saved.Token != "" && saved.UserID != "" && sameBinding(saved.Binding, b) {
				ctx, cancel := context.WithCancel(s.ctx)
				r = &businessRun{binding: b, ctx: ctx, cancel: cancel, attempted: true, session: business.Session{Token: saved.Token, UserID: saved.UserID, AuthCode: saved.AuthCode}, keys: &userkey.Cache{}}
				r.refresh.lastSuccess = saved.LastFullRefresh
				if saved.HasKey {
					r.keys.Set(userkey.Restore(saved.Key))
				}
				if offlineRaw, _ := s.store.Get(fmt.Sprintf("protocol-offline.v1.%d", t.ID)); len(offlineRaw) > 0 {
					r.offlineSince, _ = time.Parse(time.RFC3339Nano, string(offlineRaw))
				}
				if r.offlineSince.IsZero() {
					r.offlineSince = time.Now()
					_ = s.store.Put(fmt.Sprintf("protocol-offline.v1.%d", t.ID), []byte(r.offlineSince.UTC().Format(time.RFC3339Nano)))
				}
				s.businessRuns[t.ID] = r
				if data, e := s.tasks.Business(t.ID); e == nil && len(data.People) > 0 {
					data.Usable = true
					data.Status = "ready"
					data.Message = "协议授权失效，暂用已记录登录参数"
					_ = s.tasks.SaveBusiness(t.ID, data)
				}
			}
		}
		if r != nil && !r.offlineSince.IsZero() && time.Since(r.offlineSince) >= 18*time.Hour {
			message := "协议离线或授权失效已满18小时，请重新授权登录"
			if run := s.inventory.runs[t.ID]; run != nil && s.inventoryActiveLocked(t.ID) && run.status != "maintenance" && run.status != "maintenance_failed" {
				s.stopInventoryLocked(t.ID, "stopped", message)
			}
			if !r.busy && !r.refreshReady {
				if data, e := s.tasks.Business(t.ID); e == nil && (data.Status != "blocked" || data.Message != message) {
					data.Status, data.Message, data.Usable = "blocked", message, false
					_ = s.tasks.SaveBusiness(t.ID, data)
				}
			}
			if b.Status != "alive" {
				continue
			}
		}
		if b.Status == "expired" && r != nil {
			if r.offlineSince.IsZero() {
				r.offlineSince = time.Now()
				_ = s.store.Put(fmt.Sprintf("protocol-offline.v1.%d", t.ID), []byte(r.offlineSince.UTC().Format(time.RFC3339Nano)))
				_ = s.tasks.AppendLog(t.ID, "warning", "协议授权失效，18小时宽限计时开始；在宽限期内继续使用已记录参数")
			}
			if time.Since(r.offlineSince) >= 18*time.Hour {
				continue
			}
			if r.session.Token != "" {
				if data, e := s.tasks.Business(t.ID); e == nil && (!data.Usable || data.Status != "ready") {
					data.Usable = true
					data.Status = "ready"
					data.Message = "协议已失效，暂用已记录登录参数"
					_ = s.tasks.SaveBusiness(t.ID, data)
				}
			}
			continue
		}
		data, err := s.tasks.Business(t.ID)
		if err != nil {
			continue
		}
		account := b.Protocol + ":" + b.Account.Ref
		changed := data.Account != account
		if changed {
			data.Account = account
			if t.ImportCount > 0 {
				data.Selected = nil
				data.ImportConfirmed = false
			}
			data.People = nil
			data.Usable = false
			data.Status = "waiting"
			data.Message = "等待业务登录"
		}
		if b.Status == "expired" || b.Paused {
			message := "协议账号已失效，请重新扫码授权"
			if b.Status != "expired" {
				message = "任务已暂停，请启动协议；账号在线后点击卡片“刷新”恢复"
			}
			if changed || data.Usable || data.Status != "blocked" || data.Message != message {
				data.Usable = false
				data.Status = "blocked"
				data.Message = message
				_ = s.tasks.SaveBusiness(t.ID, data)
			}
			continue
		}
		if changed {
			if s.tasks.SaveBusiness(t.ID, data) != nil {
				continue
			}
		}
		if b.Status != "alive" {
			continue
		}
		// A temporary protocol check failure can make a completed job ineligible.
		// Release its loading state so the user can retry once health returns.
		if r != nil && !r.busy && data.Status == "loading" {
			data.Usable = false
			data.Status = "failed"
			data.Message = "协议账号状态曾发生变化，请重试业务登录"
			if s.tasks.SaveBusiness(t.ID, data) != nil {
				continue
			}
		}
		if r == nil {
			ctx, cancel := context.WithCancel(s.ctx)
			r = &businessRun{binding: b, ctx: ctx, cancel: cancel}
			if offlineRaw, _ := s.store.Get(fmt.Sprintf("protocol-offline.v1.%d", t.ID)); len(offlineRaw) > 0 {
				r.offlineSince, _ = time.Parse(time.RFC3339Nano, string(offlineRaw))
			}
			s.businessRuns[t.ID] = r
		}
		if !r.attempted && !r.busy {
			s.startBusinessLocked(t.ID, r, false)
		}
	}
	for id, r := range s.businessRuns {
		if !present[id] {
			s.stopOrderKeysLocked(r)
			if r.keys != nil {
				r.keys.Clear()
			}
			r.cancel()
			delete(s.businessRuns, id)
		}
	}
}
func (s *Service) startBusinessLocked(id int64, r *businessRun, refresh bool) {
	r.resumeEpoch++
	r.resumePending = false
	r.attempted = true
	cfg := s.session.BusinessConfig
	data, err := s.tasks.Business(id)
	if err != nil {
		return
	}
	if err = cfg.Validate(); err != nil {
		data.Usable = false
		data.Status = "config_error"
		data.Message = err.Error()
		_ = s.tasks.SaveBusiness(id, data)
		_ = s.tasks.AppendLog(id, "error", err.Error())
		return
	}
	deviceID, err := s.tasks.EnsureBusinessDeviceID(id, cfg.DeviceType)
	if err != nil {
		_ = s.tasks.AppendLog(id, "error", "任务设备标识生成或保存失败，请重试")
		return
	}
	data.DeviceID = deviceID
	data.DeviceType = cfg.DeviceType
	data.Status = "loading"
	r.refreshReady = false
	data.Message = "正在获取观演人"
	data.Usable = false
	r.refresh.full = !refresh || r.session.Token == ""
	if !refresh || r.session.Token == "" {
		r.session = business.Session{}
		data.Message = "正在获取小程序授权码并登录业务系统"
	}
	if err = s.tasks.SaveBusiness(id, data); err != nil {
		return
	}
	r.busy = true
	r.refresh.invalid = true
	s.stopOrderKeysLocked(r)
	if r.keys != nil {
		r.keys.Clear()
	}
	s.businessWG.Add(1)
	go func() { defer s.businessWG.Done(); s.runBusiness(id, r, cfg, data.DeviceID) }()
}
func (s *Service) currentBusinessLocked(id int64, r *businessRun) bool {
	if s.businessRuns[id] != r || r.ctx.Err() != nil || s.session == nil || !s.state.LoggedIn || !time.Now().Before(s.session.ExpiresAt) {
		return false
	}
	b, ok := s.protocols.Bindings()[id]
	if !ok || !sameBinding(b, r.binding) || b.Paused {
		return false
	}
	if b.Status == "alive" {
		return true
	}
	return !r.offlineSince.IsZero() && time.Since(r.offlineSince) < 18*time.Hour
}
func (s *Service) businessLog(id int64, r *businessRun, level, text string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.currentBusinessLocked(id, r) {
		_ = s.tasks.AppendLog(id, level, text)
	}
}
func (s *Service) runBusiness(id int64, r *businessRun, cfg business.Config, deviceID string) {
	ctx, cancel := context.WithTimeout(r.ctx, 5*time.Minute)
	defer cancel()
	session, people, err := s.businessClient.RefreshWithLogin(s.taskNetworkContext(ctx, id), cfg, deviceID, r.session, func(ctx context.Context) (string, error) {
		return s.protocols.MiniProgramCode(ctx, id, r.binding, business.AppID)
	}, func(level, text string) { s.businessLog(id, r, level, text) }, func(session business.Session) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.currentBusinessLocked(id, r) {
			r.refresh.full = true
			s.adoptOrderSessionLocked(id, r, session)
		}
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.currentBusinessLocked(id, r) {
		r.busy = false
		return
	}
	r.busy = false
	if business.IsTokenExpired(err) {
		session = business.Session{}
	}
	if session.Token != "" {
		s.adoptOrderSessionLocked(id, r, session)
	} else {
		s.stopOrderKeysLocked(r)
		if r.keys != nil {
			r.keys.Clear()
		}
	}
	data, getErr := s.tasks.Business(id)
	if getErr != nil {
		s.failAutoRefreshLocked(id, r, "无法读取任务资料", s.refreshTime())
		return
	}
	if err != nil {
		if business.IsTokenExpired(err) {
			session = business.Session{}
		}
		data.Status = "failed"
		data.Message = "业务流程失败，请重试；原观演人数据保留"
		var apiError *business.APIError
		if errors.As(err, &apiError) {
			data.Message = apiError.Error() + "，请重试；原观演人数据保留"
		}
		data.Usable = false
		r.session = session
		_ = s.tasks.SaveBusiness(id, data)
		_ = s.tasks.AppendLog(id, "error", data.Message)
		if run := s.inventory.runs[id]; run != nil && run.status == "maintenance" {
			run.status, run.message = "maintenance_failed", data.Message
		}
		s.failAutoRefreshLocked(id, r, data.Message, s.refreshTime())
		return
	}
	allowed := map[string]bool{}
	for _, p := range people {
		allowed[p.ID] = p.Available()
	}
	missing := false
	for _, id := range data.Selected {
		if !allowed[id] {
			missing = true
		}
	}
	data.People = people
	data.Status = "ready"
	data.Message = fmt.Sprintf("业务已登录 · %d 位观演人", len(people))
	if missing {
		data.Message = "已选观演人已不存在或不可用，请重新选择"
	}
	data.Usable = true
	if s.tasks.SaveBusiness(id, data) != nil {
		_ = s.tasks.AppendLog(id, "error", "观演人保存失败，请重试；原数据保留")
		s.failAutoRefreshLocked(id, r, "观演人保存失败", s.refreshTime())
		return
	}
	r.session = session
	s.persistBusinessSessionLocked(id, r)
	r.refreshReady = true
	s.stopOrderKeysLocked(r)
	s.startOrderKeysLocked(id, r)
	_ = s.tasks.AppendLog(id, "success", fmt.Sprintf("观演人获取成功：%d 人", len(people)))
	s.completeMaintenanceLocked(id, r)
}

// Called under s.mu only after business data and an unexpired key are available.
func (s *Service) completeMaintenanceLocked(id int64, r *businessRun) {
	if !r.refreshReady || r.busy || r.keys == nil {
		return
	}
	key, ok := r.keys.Peek()
	if !ok || !key.Valid(time.Now()) {
		return
	}
	data, err := s.tasks.Business(id)
	if err != nil {
		s.failAutoRefreshLocked(id, r, "无法读取观演人资料", s.refreshTime())
		return
	}
	if len(data.Selected) > 0 {
		if _, err := orders.PreparePeople(data.People, data.Selected); err != nil {
			r.refreshReady = false
			r.refresh.active = false
			r.refresh.blocked = true
			r.refresh.issue = err.Error()
			r.refresh.retryAt = time.Time{}
			s.maintenanceResume[id] = false
			if run := s.inventory.runs[id]; run != nil && (run.status == "maintenance" || run.status == "maintenance_failed") {
				run.status, run.message = "stopped", err.Error()
			}
			s.queueExecutionLog(id, "warning", err.Error()+"；任务未自动恢复")
			return
		}
	}
	if r.refresh.full {
		r.refresh.lastSuccess = s.refreshTime()
		r.refresh.next = time.Time{}
		r.refresh.retryAt = time.Time{}
		r.refresh.failures = 0
		r.refresh.issue = ""
		r.refresh.blocked = false
	}
	if r.refresh.active {
		s.nextAutoRefresh = s.refreshTime().Add(refreshStagger)
	}
	r.refresh.active = false
	r.refresh.invalid = false
	s.persistBusinessSessionLocked(id, r)
	r.refreshReady = false
	if !r.offlineSince.IsZero() {
		r.offlineSince = time.Time{}
		_ = s.store.Delete(fmt.Sprintf("protocol-offline.v1.%d", id))
	}
	if run := s.inventory.runs[id]; run != nil && (run.status == "maintenance" || run.status == "maintenance_failed") {
		run.status, run.message = "stopped", "维护完成"
	}
	if !s.maintenanceResume[id] {
		return
	}
	available := map[string]bool{}
	for _, p := range data.People {
		available[p.ID] = p.Available()
	}
	for _, selected := range data.Selected {
		if !available[selected] {
			if run := s.inventory.runs[id]; run != nil {
				run.status, run.message = "stopped", "已选观演人已不存在或不可用，请重新选择"
			}
			s.maintenanceResume[id] = false
			_ = s.tasks.AppendLog(id, "warning", "已选观演人已不存在或不可用，请重新选择；任务未自动恢复")
			return
		}
	}
	if run := s.inventory.runs[id]; run != nil && (run.status == "maintenance" || run.status == "maintenance_failed") {
		run.status, run.message = "stopped", "维护完成"
	}
	s.businessWG.Add(1)
	notBefore := r.resumeNotBefore
	r.resumePending = true
	epoch := r.resumeEpoch
	go func() {
		defer s.businessWG.Done()
		if !waitInventory(r.ctx, time.Until(notBefore)) {
			return
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.businessRuns[id] != r || r.ctx.Err() != nil || r.resumeEpoch != epoch {
			return
		}
		r.resumePending = false
		r.resumeNotBefore = time.Time{}
		if err := s.startInventoryModeLocked(id, true, true); err != nil {
			_ = s.tasks.AppendLog(id, "warning", "维护完成但任务未自动恢复："+err.Error())
		}
	}()
}
func (s *Service) RefreshTaskPeople(id int64, login bool) error {
	if err := s.claimMaintenance(id); err != nil {
		return err
	}
	defer s.releaseMaintenance(id)
	_, err := s.pauseForMaintenance(id)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	resumed := false
	if s.protocols != nil {
		if b, ok := s.protocols.Bindings()[id]; ok && b.Paused {
			if b.Protocol != s.state.AuthProtocol {
				return errors.New("请先登录任务所属协议")
			}
			if err := s.protocols.ResumeTask(id); err != nil {
				return err
			}
			// Cancel any pre-pause job before the scheduler creates a fresh run.
			s.stopTaskBusinessLocked(id)
			resumed = true
		}
	}
	s.reconcileBusinessLocked()
	r := s.businessRuns[id]
	if r == nil || !s.currentBusinessLocked(id, r) {
		return errors.New("请先确保当前协议账号在线")
	}
	if r.busy {
		if resumed {
			return nil
		}
		return errors.New("业务流程正在执行，请稍候")
	}
	s.startBusinessLocked(id, r, !login)
	return nil
}

func (s *Service) pauseForMaintenance(id int64) (bool, error) {
	s.mu.Lock()
	if err := s.requireTaskSession(); err != nil {
		s.mu.Unlock()
		return false, err
	}
	r := s.inventory.runs[id]
	if r != nil && !r.orderInFlight && s.inventory.pending[id] != nil {
		r.status, r.message = "order_verifying", "下单结果不明，请先核实订单再维护"
	}
	if r != nil && (r.status == "order_verifying" || r.status == "order_blocked" || r.status == "order_review") {
		s.mu.Unlock()
		return false, errors.New("请先处理当前订单状态，再刷新或重新授权")
	}
	resume := s.inventoryActiveLocked(id) || s.maintenanceResume[id]
	if r != nil && (r.status == "maintenance" || r.status == "maintenance_failed") {
		resume = s.maintenanceResume[id]
	}
	if r != nil && r.status == "order_created" {
		resume = false
	}
	var done <-chan struct{}
	if resume && r != nil {
		if b := s.businessRuns[id]; b != nil && r.status == "order_cooldown" {
			remaining := r.retryAt.Sub(s.calibratedNowLocked())
			if remaining > 0 {
				b.resumeNotBefore = s.refreshTime().Add(remaining)
			}
		}
		s.stopInventoryLocked(id, "stopped", "已暂停，准备刷新或重新授权")
		if r.orderInFlight {
			done = r.orderDone
		} else {
			r.status, r.message = "maintenance", "维护中"
		}
	}
	if !resume && (r == nil || r.status == "stopped" || r.status == "paused" || r.status == "maintenance_failed") {
		if r == nil {
			ctx, cancel := context.WithCancel(s.ctx)
			r = &inventoryRun{ctx: ctx, cancel: cancel, queryCancel: func() {}, match: s.session.CurrentMatch}
			if s.inventory.runs == nil {
				s.inventory.runs = map[int64]*inventoryRun{}
			}
			s.inventory.runs[id] = r
		}
		r.status, r.message = "maintenance", "维护中"
	}
	if s.maintenanceResume == nil {
		s.maintenanceResume = map[int64]bool{}
	}
	s.maintenanceResume[id] = resume
	s.mu.Unlock()
	if done != nil {
		select {
		case <-done:
		case <-time.After(16 * time.Second):
			return resume, errors.New("等待在途下单结果超时，维护尚未开始")
		}
		s.mu.Lock()
		if r.orderInFlight {
			s.mu.Unlock()
			return resume, errors.New("下单请求仍在途，请稍后重试维护")
		}
		if r.status == "stopped" {
			r.status, r.message = "maintenance", "维护中"
		}
		if r.status == "order_created" || r.status == "order_verifying" || r.status == "order_blocked" {
			s.maintenanceResume[id] = false
			resume = false
		}
		if r.status == "order_verifying" || r.status == "order_blocked" {
			s.mu.Unlock()
			return false, errors.New("请先处理当前订单状态，再刷新或重新授权")
		}
		s.mu.Unlock()
	}
	return resume, nil
}
func (s *Service) SaveTaskPeople(id int64, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if err := s.requireInventoryStoppedLocked(id); err != nil {
		return err
	}
	r := s.businessRuns[id]
	if r == nil || !s.currentBusinessLocked(id, r) {
		return errors.New("协议账号状态已变化，请检查后重试")
	}
	if r.busy {
		return errors.New("正在刷新观演人，请稍候")
	}
	if err := s.tasks.SavePeopleSelection(id, ids); err != nil {
		return err
	}
	r.refresh.blocked = false
	r.refresh.issue = ""
	return nil
}
func (s *Service) stopTaskBusinessLocked(id int64) {
	if r := s.businessRuns[id]; r != nil {
		s.stopOrderKeysLocked(r)
		if r.keys != nil {
			r.keys.Clear()
		}
		r.cancel()
		delete(s.businessRuns, id)
	}
	if b, err := s.tasks.Business(id); err == nil {
		b.Usable = false
		b.Status = "waiting"
		b.Message = "等待业务登录"
		_ = s.tasks.SaveBusiness(id, b)
	}
}
