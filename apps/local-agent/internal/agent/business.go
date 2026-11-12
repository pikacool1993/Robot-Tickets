package agent

import (
	"context"
	"errors"
	"fmt"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/protocols"
	"time"
)

type businessRun struct {
	binding   protocols.Binding
	ctx       context.Context
	cancel    context.CancelFunc
	busy      bool
	session   business.Session
	attempted bool
}

func sameBinding(a, b protocols.Binding) bool {
	return a.Protocol == b.Protocol && a.Account.Ref == b.Account.Ref && a.Generation == b.Generation
}
func (s *Service) cancelBusinessLocked() {
	for _, r := range s.businessRuns {
		r.cancel()
	}
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
		r := s.businessRuns[t.ID]
		if r != nil && (!ok || !sameBinding(r.binding, b) || b.Status == "expired" || b.Paused || b.Protocol != s.state.AuthProtocol) {
			r.cancel()
			delete(s.businessRuns, t.ID)
			r = nil
		}
		if !ok || b.Protocol != s.state.AuthProtocol {
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
			data.People = nil
			data.Selected = nil
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
			s.businessRuns[t.ID] = r
		}
		if !r.attempted && !r.busy {
			s.startBusinessLocked(t.ID, r, false)
		}
	}
	for id, r := range s.businessRuns {
		if !present[id] {
			r.cancel()
			delete(s.businessRuns, id)
		}
	}
}
func (s *Service) startBusinessLocked(id int64, r *businessRun, refresh bool) {
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
	data.Message = "正在获取观演人"
	data.Usable = false
	if !refresh || r.session.Token == "" {
		r.session = business.Session{}
		data.Message = "正在获取小程序授权码并登录业务系统"
	}
	if err = s.tasks.SaveBusiness(id, data); err != nil {
		return
	}
	r.busy = true
	s.businessWG.Add(1)
	go func() { defer s.businessWG.Done(); s.runBusiness(id, r, cfg, data.DeviceID) }()
}
func (s *Service) currentBusinessLocked(id int64, r *businessRun) bool {
	if s.businessRuns[id] != r || r.ctx.Err() != nil || s.session == nil || !s.state.LoggedIn || !time.Now().Before(s.session.ExpiresAt) {
		return false
	}
	b, ok := s.protocols.Bindings()[id]
	return ok && sameBinding(b, r.binding) && b.Status == "alive" && !b.Paused
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
	session, people, err := s.businessClient.Refresh(ctx, cfg, deviceID, r.session, func(ctx context.Context) (string, error) {
		return s.protocols.MiniProgramCode(ctx, id, r.binding, business.AppID)
	}, func(level, text string) { s.businessLog(id, r, level, text) })
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.currentBusinessLocked(id, r) {
		r.busy = false
		return
	}
	r.busy = false
	data, getErr := s.tasks.Business(id)
	if getErr != nil {
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
		return
	}
	allowed := map[string]bool{}
	for _, p := range people {
		allowed[p.ID] = true
	}
	selected := []string{}
	for _, id := range data.Selected {
		if allowed[id] {
			selected = append(selected, id)
		}
	}
	data.People = people
	data.Selected = selected
	data.Status = "ready"
	data.Message = fmt.Sprintf("业务已登录 · %d 位观演人", len(people))
	data.Usable = true
	if s.tasks.SaveBusiness(id, data) != nil {
		_ = s.tasks.AppendLog(id, "error", "观演人保存失败，请重试；原数据保留")
		return
	}
	r.session = session
	_ = s.tasks.AppendLog(id, "success", fmt.Sprintf("观演人获取成功：%d 人", len(people)))
}
func (s *Service) RefreshTaskPeople(id int64, login bool) error {
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
func (s *Service) SaveTaskPeople(id int64, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	r := s.businessRuns[id]
	if r == nil || !s.currentBusinessLocked(id, r) {
		return errors.New("协议账号状态已变化，请检查后重试")
	}
	if r.busy {
		return errors.New("正在刷新观演人，请稍候")
	}
	return s.tasks.SavePeopleSelection(id, ids)
}
func (s *Service) stopTaskBusinessLocked(id int64) {
	if r := s.businessRuns[id]; r != nil {
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
