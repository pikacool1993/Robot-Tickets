package agent

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"time"
)

// These are local scheduling choices, not guarantees about upstream validity.
const refreshStagger = 2 * time.Second
const refreshSaleGuard = 30 * time.Second

type AutoRefreshSettings struct {
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
}

func defaultAutoRefreshSettings() AutoRefreshSettings {
	return AutoRefreshSettings{IntervalMinutes: 30}
}
func (v AutoRefreshSettings) Validate() error {
	if v.IntervalMinutes < 1 || v.IntervalMinutes > 1440 {
		return errors.New("自动刷新间隔须为 1～1440 分钟的整数")
	}
	return nil
}

type refreshSchedule struct {
	invalid              bool
	lastSuccess          time.Time
	next                 time.Time
	retryAt              time.Time
	started              time.Time
	active               bool
	full                 bool
	failures             int
	issue                string
	blocked              bool
	authorizationPending bool
}
type AutoRefreshView struct {
	LastSuccess time.Time `json:"lastSuccess"`
	Next        time.Time `json:"next"`
	RetryAt     time.Time `json:"retryAt"`
	Status      string    `json:"status"`
	Message     string    `json:"message"`
}

func (s *Service) refreshTime() time.Time {
	if s.refreshNow != nil {
		return s.refreshNow()
	}
	return time.Now()
}
func (s *Service) initAutoRefresh() error {
	s.autoRefresh = defaultAutoRefreshSettings()
	raw, err := s.store.Get("auto-refresh-settings.v1")
	if err != nil {
		return err
	}
	if len(raw) > 0 {
		if json.Unmarshal(raw, &s.autoRefresh) != nil || s.autoRefresh.Validate() != nil {
			return errors.New("自动刷新设置无效")
		}
	}
	return nil
}
func (s *Service) SaveAutoRefreshSettings(v AutoRefreshSettings) error {
	if err := v.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	raw, _ := json.Marshal(v)
	if err := s.store.Put("auto-refresh-settings.v1", raw); err != nil {
		return errors.New("自动刷新设置保存失败")
	}
	s.autoRefresh = v
	for _, b := range s.businessRuns {
		b.refresh.next = time.Time{}
		if !v.Enabled && !b.refresh.active {
			b.refresh.retryAt = time.Time{}
			b.refresh.failures = 0
		}
	}
	return nil
}
func (s *Service) refreshDueLocked(b *businessRun) time.Time {
	if b.refresh.next.IsZero() && !b.refresh.lastSuccess.IsZero() {
		b.refresh.next = b.refresh.lastSuccess.Add(time.Duration(s.autoRefresh.IntervalMinutes) * time.Minute)
	}
	if !b.refresh.retryAt.IsZero() {
		return b.refresh.retryAt
	}
	return b.refresh.next
}

// Called under s.mu: eligibility and reservation share the order state lock.
func (s *Service) autoRefreshDeferralLocked(id int64, b *businessRun) string {
	if b.refresh.authorizationPending || s.maintenanceClaims[id] {
		return "手动维护中"
	}
	if b.resumePending {
		return "等待维护后冷却结束"
	}
	if b.busy || b.refreshReady {
		return "刷新中"
	}
	if b.refresh.blocked {
		return b.refresh.issue
	}
	binding, ok := s.protocols.Bindings()[id]
	if !ok || !sameBinding(binding, b.binding) || binding.Paused || binding.Status != "alive" {
		return "等待协议账号在线"
	}
	if !s.currentBusinessLocked(id, b) {
		return "等待客户端登录或账号就绪"
	}
	if r := s.inventory.runs[id]; r != nil {
		if r.simulated {
			return "模拟任务不执行自动刷新"
		}
		if r.orderInFlight || r.status == "submitting" || r.status == "stopping" {
			return "等待在途下单完成"
		}
		if s.inventory.pending[id] != nil || r.status == "order_created" || r.status == "order_verifying" || r.status == "order_blocked" || r.status == "order_review" {
			return "等待人工处理订单状态"
		}
		if r.plan != nil || r.status == "ready" || r.status == "pending_order" {
			return "优先提交已命中方案"
		}
		if r.early && !r.match.SaleAt.IsZero() && !s.calibratedNowLocked().Before(r.match.SaleAt.Add(-refreshSaleGuard)) {
			return "避让首次开售提交窗口"
		}
	}
	return ""
}
func (s *Service) tickAutoRefreshLocked(now time.Time) {
	if s.ctx.Err() != nil || s.session == nil || !s.state.LoggedIn || s.protocols == nil || !now.Before(s.session.ExpiresAt) {
		return
	}
	// A full refresh waiting for its key must not hold the automatic slot forever.
	for id, b := range s.businessRuns {
		if b.refresh.authorizationPending {
			attempt := s.protocols.Attempt(id)
			switch attempt.Status {
			case "failed", "expired", "timeout", "cancelled":
				b.refresh.authorizationPending = false
			}
		}
		if b.refresh.active && !b.busy && now.Sub(b.refresh.started) >= 5*time.Minute {
			s.stopOrderKeysLocked(b)
			b.refreshReady = false
			s.failAutoRefreshLocked(id, b, "等待订单加密参数超时", now)
		}
	}
	if !s.autoRefresh.Enabled {
		return
	}
	for _, b := range s.businessRuns {
		if b.refresh.active {
			return
		}
	}
	if now.Before(s.nextAutoRefresh) {
		return
	}
	ids := make([]int64, 0, len(s.businessRuns))
	for id := range s.businessRuns {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		a, c := s.refreshDueLocked(s.businessRuns[ids[i]]), s.refreshDueLocked(s.businessRuns[ids[j]])
		if a.Equal(c) {
			return ids[i] < ids[j]
		}
		return a.Before(c)
	})
	for _, id := range ids {
		b := s.businessRuns[id]
		due := s.refreshDueLocked(b)
		if due.IsZero() || now.Before(due) || s.autoRefreshDeferralLocked(id, b) != "" {
			continue
		}
		s.beginAutoRefreshLocked(id, b, now)
		return
	}
}
func (s *Service) beginAutoRefreshLocked(id int64, b *businessRun, now time.Time) {
	if s.maintenanceResume == nil {
		s.maintenanceResume = map[int64]bool{}
	}
	r := s.inventory.runs[id]
	resume := s.maintenanceResume[id]
	if r != nil {
		if r.status != "maintenance" && r.status != "maintenance_failed" {
			resume = s.inventoryActiveLocked(id)
		}
		if r.status == "order_cooldown" {
			remaining := r.retryAt.Sub(s.calibratedNowLocked())
			if remaining > 0 {
				b.resumeNotBefore = now.Add(remaining)
			}
		}
		s.stopInventoryLocked(id, "stopped", "自动刷新，暂时暂停库存监控")
	} else {
		ctx, cancel := context.WithCancel(s.ctx)
		r = &inventoryRun{ctx: ctx, cancel: cancel, queryCancel: func() {}, match: s.session.CurrentMatch}
		if s.inventory.runs == nil {
			s.inventory.runs = map[int64]*inventoryRun{}
		}
		s.inventory.runs[id] = r
	}
	r.status, r.message = "maintenance", "定时刷新中"
	s.maintenanceResume[id] = resume
	b.refresh.active = true
	b.refresh.started = now
	b.refresh.issue = ""
	b.refresh.retryAt = time.Time{}
	s.nextAutoRefresh = now.Add(refreshStagger)
	s.queueExecutionLog(id, "info", "开始定时刷新：重新取码、登录并更新观演人")
	s.startBusinessLocked(id, b, false)
	if !b.busy {
		s.failAutoRefreshLocked(id, b, "自动刷新未能启动，请检查业务配置或本地存储", now)
	}
}
func (s *Service) failAutoRefreshLocked(id int64, b *businessRun, message string, now time.Time) {
	if !b.refresh.active {
		return
	}
	b.refresh.active = false
	b.refresh.issue = message
	b.refresh.failures++
	delays := []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute}
	index := b.refresh.failures - 1
	if index >= len(delays) {
		index = len(delays) - 1
	}
	b.refresh.retryAt = now.Add(delays[index])
	s.nextAutoRefresh = now.Add(refreshStagger)
	if r := s.inventory.runs[id]; r != nil && r.status == "maintenance" {
		r.status, r.message = "maintenance_failed", message
	}
}
func (s *Service) autoRefreshViewsLocked(now time.Time) map[int64]AutoRefreshView {
	views := map[int64]AutoRefreshView{}
	for id, b := range s.businessRuns {
		due := s.refreshDueLocked(b)
		v := AutoRefreshView{LastSuccess: b.refresh.lastSuccess, Next: due, RetryAt: b.refresh.retryAt, Status: "scheduled", Message: "按间隔自动刷新"}
		switch {
		case b.refresh.active || b.busy || b.refreshReady:
			v.Status = "refreshing"
			v.Message = "正在更新业务资料或等待加密参数"
		case !s.autoRefresh.Enabled:
			v.Status = "disabled"
			v.Message = "自动刷新已关闭"
			v.Next = time.Time{}
			v.RetryAt = time.Time{}
		case b.refresh.blocked:
			v.Status = "blocked"
			v.Message = b.refresh.issue
			v.Next = time.Time{}
		case due.IsZero():
			v.Status = "waiting"
			v.Message = "等待首次完整业务登录成功"
		case !b.refresh.retryAt.IsZero():
			v.Status = "retry"
			v.Message = b.refresh.issue
		case !now.Before(due):
			v.Status = "pending"
			v.Message = "已到期，等待错峰刷新"
		}
		if s.autoRefresh.Enabled && !b.refresh.active && !due.IsZero() && !now.Before(due) {
			if reason := s.autoRefreshDeferralLocked(id, b); reason != "" {
				v.Status = "deferred"
				v.Message = reason
			}
		}
		views[id] = v
	}
	return views
}

func (s *Service) claimMaintenance(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if s.maintenanceClaims == nil {
		s.maintenanceClaims = map[int64]bool{}
	}
	if s.maintenanceClaims[id] {
		return errors.New("正在刷新或重新授权，请稍候")
	}
	if b := s.businessRuns[id]; b != nil && (b.busy || b.refreshReady || b.refresh.active) {
		return errors.New("正在刷新或等待加密参数，请稍候")
	}
	s.maintenanceClaims[id] = true
	return nil
}
func (s *Service) releaseMaintenance(id int64) {
	s.mu.Lock()
	delete(s.maintenanceClaims, id)
	s.mu.Unlock()
}
