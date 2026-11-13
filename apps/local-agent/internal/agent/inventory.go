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
	queryAt     time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	queryCancel context.CancelFunc
	business    *businessRun
	status      string
	message     string
	target      time.Time
	plan        *inventory.Plan
	task        tasks.Task
	catalog     tasks.Catalog
	defaults    []inventory.Region
	match       protocol.MatchConfig
	early       bool
}
type ExecutionView struct {
	Status  string          `json:"status"`
	Message string          `json:"message"`
	Target  time.Time       `json:"target"`
	Plan    *inventory.Plan `json:"plan,omitempty"`
}
type InventoryView struct {
	Match      protocol.MatchConfig    `json:"match"`
	Settings   inventory.Settings      `json:"settings"`
	ClockReady bool                    `json:"clockReady"`
	ClockError string                  `json:"clockError"`
	OffsetMS   int64                   `json:"offsetMS"`
	Now        time.Time               `json:"now"`
	Runs       map[int64]ExecutionView `json:"runs"`
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
}

func (s *Service) initInventory() error {
	s.inventory.settings = inventory.Defaults()
	s.inventory.runs = map[int64]*inventoryRun{}
	raw, err := s.store.Get("inventory-settings.v1")
	if err != nil {
		return err
	}
	if len(raw) > 0 {
		if json.Unmarshal(raw, &s.inventory.settings) != nil || s.inventory.settings.Validate() != nil {
			return errors.New("库存查询设置无效，请检查本地数据")
		}
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
	out := InventoryView{Match: s.session.CurrentMatch, Settings: s.inventory.settings, ClockReady: s.inventory.synced, ClockError: s.inventory.syncError, OffsetMS: s.inventory.offset, Now: s.calibratedNowLocked(), Runs: map[int64]ExecutionView{}}
	for id, r := range s.inventory.runs {
		// A detached DTO prevents callers from observing mutable run state.
		v := ExecutionView{Status: r.status, Message: r.message, Target: r.target}
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
	return r != nil && (r.status == "waiting" || r.status == "monitoring" || r.status == "ready" || r.status == "pending_order")
}
func (s *Service) requireInventoryStoppedLocked(id int64) error {
	if s.inventoryActiveLocked(id) {
		return errors.New("请先停止任务，再修改执行配置")
	}
	return nil
}
func (s *Service) validInventoryLocked(id int64, r *inventoryRun) bool {
	return s.inventory.runs[id] == r && r.ctx.Err() == nil && s.currentBusinessLocked(id, r.business) && !r.business.busy
}
func (s *Service) stopInventoryLocked(id int64, status, message string) {
	if r := s.inventory.runs[id]; r != nil {
		r.cancel()
		r.queryCancel()
		r.status = status
		r.message = message
		r.plan = nil
	}
}
func (s *Service) cancelInventoryLocked() {
	for id := range s.inventory.runs {
		s.stopInventoryLocked(id, "stopped", "已停止")
	}
}
func (s *Service) reconcileInventoryLocked() {
	for id, r := range s.inventory.runs {
		if s.inventoryActiveLocked(id) && !s.validInventoryLocked(id, r) {
			s.stopInventoryLocked(id, "paused", "账号或业务状态变化，请处理后重新启动")
			_ = s.tasks.AppendLog(id, "warning", "库存监控已暂停：账号或业务状态变化")
		}
	}
}
func (s *Service) StartInventory(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if s.inventoryActiveLocked(id) {
		return errors.New("任务已启动")
	}
	if !s.inventory.synced {
		return errors.New("尚未完成时间校准，请先校准")
	}
	if err := s.session.CurrentMatch.Ready(); err != nil {
		return err
	}
	b := s.businessRuns[id]
	if b == nil || !s.currentBusinessLocked(id, b) || b.busy || b.session.Token == "" {
		return errors.New("请先确保账号在线并完成业务登录")
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
	monotonicStart := time.Now()
	now := s.calibratedNowLocked()
	query, target, early := inventory.Schedule(now, s.session.CurrentMatch.SaleAt, s.inventory.settings)
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
		if _, ok := inventory.Match(defaults, view.Catalog, *task, len(task.PeopleSelected), task.AllowCross, func(int64) int64 { return 0 }); !ok {
			return errors.New("默认库存无法满足本任务人数或座位要求，不能启动")
		}
	}
	if old := s.inventory.runs[id]; old != nil {
		old.cancel()
		old.queryCancel()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	qctx, qcancel := context.WithCancel(ctx)
	r := &inventoryRun{ctx: ctx, cancel: cancel, queryCancel: qcancel, business: b, status: "waiting", message: "等待开售后第1秒查询", target: target, task: *task, catalog: view.Catalog, defaults: defaults, match: s.session.CurrentMatch, early: early, queryAt: query}
	s.inventory.runs[id] = r
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
	_ = s.tasks.AppendLog(id, "info", "库存任务已启动；首次提交仅调度，订单接口尚未接入")
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
	s.stopInventoryLocked(id, "stopped", "已停止")
	return s.tasks.AppendLog(id, "info", "库存任务已停止，已清除本轮方案与定时")
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
func (s *Service) inventoryDueLocked(id int64, r *inventoryRun) {
	if !s.validInventoryLocked(id, r) || r.status == "pending_order" {
		return
	}
	if r.plan == nil {
		plan, ok := inventory.Match(r.defaults, r.catalog, r.task, len(r.task.PeopleSelected), r.task.AllowCross, nil)
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
	if s.inventory.runs[id] != r || r.ctx.Err() != nil || r.plan == nil || r.status == "pending_order" {
		return
	}
	r.queryCancel()
	r.status = "pending_order"
	r.message = "首次提交时间已到，待接入下单接口（未发送订单）"
	_ = s.tasks.AppendLog(id, "success", fmt.Sprintf("首次提交调度已到点，方案来源：%s；下单接口未接入，未发送订单", r.plan.Source))
}
func (s *Service) deliverInventoryLocked(source int64, regions []inventory.Region, at time.Time) {
	// All receivers match the same immutable snapshot with their own conditions.
	for id, r := range s.inventory.runs {
		if (r.status != "monitoring" && !(r.status == "waiting" && !s.calibratedNowLocked().Before(r.queryAt))) || !s.validInventoryLocked(id, r) {
			continue
		}
		plan, ok := inventory.Match(regions, r.catalog, r.task, len(r.task.PeopleSelected), r.task.AllowCross, nil)
		if !ok {
			continue
		}
		plan.Source = "notification"
		if source == id {
			plan.Source = "realtime"
		}
		plan.MatchID = r.match.ID
		plan.ObservedAt = at
		s.acceptInventoryPlanLocked(id, r, plan)
	}
}

// Caller validates account/session eligibility under s.mu before reaching this state transition.
func (s *Service) acceptInventoryPlanLocked(id int64, r *inventoryRun, plan inventory.Plan) {
	if s.inventory.runs[id] != r || r.ctx.Err() != nil || r.plan != nil || (r.status != "monitoring" && r.status != "waiting") {
		return
	}
	r.plan = &plan
	r.queryCancel()
	r.status = "ready"
	r.message = "已命中库存，等待首次提交时间"
	_ = s.tasks.AppendLog(id, "success", fmt.Sprintf("库存匹配成功：%d人，来源 %s", len(r.task.PeopleSelected), plan.Source))
	if !r.early {
		s.stageInventoryOrderLocked(id, r)
	}
}
func (s *Service) pollInventory(id int64, r *inventoryRun, ctx context.Context, deadline time.Time) {
	if !waitInventory(ctx, time.Until(deadline)) {
		return
	}
	failures := 0
	for {
		s.mu.Lock()
		if !s.validInventoryLocked(id, r) || r.plan != nil {
			s.mu.Unlock()
			return
		}
		r.status = "monitoring"
		r.message = "正在查询实时库存"
		cfg, session := s.session.BusinessConfig, r.business.session
		s.mu.Unlock()
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		raw, err := s.businessClient.Stock(callCtx, cfg, session, r.match.ID)
		cancel()
		var regions []inventory.Region
		if err == nil {
			regions, err = inventory.Parse(raw)
		}
		s.mu.Lock()
		if !s.validInventoryLocked(id, r) || ctx.Err() != nil || r.plan != nil {
			s.mu.Unlock()
			return
		}
		if business.IsTokenExpired(err) {
			s.stopInventoryLocked(id, "paused", "业务登录失效，请刷新后重新启动")
			s.mu.Unlock()
			return
		}
		if err == nil {
			if failures > 0 {
				_ = s.tasks.AppendLog(id, "info", "库存查询已恢复")
			}
			failures = 0
			r.message = "暂无符合条件的库存"
			// Zero snapshots do not produce notifications.
			for _, region := range regions {
				if region.Count > 0 {
					s.deliverInventoryLocked(id, regions, s.calibratedNowLocked())
					break
				}
			}
		} else {
			failures++
			r.message = "库存查询异常，正在重试（不表示零库存）"
			if failures == 1 {
				_ = s.tasks.AppendLog(id, "warning", r.message)
			}
		}
		settings := s.inventory.settings
		delay := time.Duration(inventory.Between(settings.QueryMinMS, settings.QueryMaxMS)) * time.Millisecond
		if failures > 0 {
			backoff := time.Second * time.Duration(1<<min(failures-1, 3))
			if delay < backoff {
				delay = backoff
			}
		}
		done := r.plan != nil
		s.mu.Unlock()
		if done || !waitInventory(ctx, delay) {
			return
		}
	}
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
