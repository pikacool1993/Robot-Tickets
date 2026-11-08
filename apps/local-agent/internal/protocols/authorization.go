package protocols

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

func terminal(status string) bool {
	switch status {
	case "confirmed", "expired", "cancelled", "failed", "timeout":
		return true
	}
	return false
}
func (m *Manager) Bindings() map[int64]Binding {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[int64]Binding{}
	for id, b := range m.doc.Bindings {
		out[id] = b
	}
	return out
}
func (m *Manager) Begin(taskID int64, k string) (Attempt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != k || m.workCtx == nil {
		return Attempt{}, errors.New("请先登录当前协议")
	}
	if m.state[k] != "running" {
		return Attempt{}, errors.New("请先启动协议服务")
	}
	if old := m.attempts[taskID]; old != nil && old.cancel != nil {
		old.cancel()
	}
	b := make([]byte, 16)
	if _, e := rand.Read(b); e != nil {
		return Attempt{}, e
	}
	ctx, cancel := context.WithTimeout(m.workCtx, 5*time.Minute)
	a := &Attempt{ID: hex.EncodeToString(b), TaskID: taskID, Protocol: k, Status: "creating", ExpiresAt: time.Now().Add(5 * time.Minute), cancel: cancel}
	m.attempts[taskID] = a
	out := *a
	m.wg.Add(1)
	go m.authorize(ctx, a)
	return out, nil
}
func (m *Manager) Attempt(taskID int64) Attempt {
	m.mu.Lock()
	defer m.mu.Unlock()
	if a := m.attempts[taskID]; a != nil {
		return *a
	}
	return Attempt{TaskID: taskID, Status: "none"}
}
func (m *Manager) updateAttempt(a *Attempt, status, message string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.attempts[a.TaskID] != a || terminal(a.Status) {
		return false
	}
	a.Status = status
	a.Message = message
	if terminal(status) {
		a.Image = ""
	}
	return true
}
func (m *Manager) authorize(ctx context.Context, a *Attempt) {
	defer m.wg.Done()
	defer a.cancel()
	// Every task owns an interface and an explicit account reference; adapters hold no current-account state.
	var protocol Protocol = adapt(m.client(a.Protocol))
	s, e := protocol.Begin(ctx)
	if e != nil {
		status := "failed"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			status = "timeout"
		}
		if errors.Is(ctx.Err(), context.Canceled) {
			status = "cancelled"
		}
		m.updateAttempt(a, status, e.Error())
		return
	}
	if s.ID == "" || s.Image == "" {
		m.updateAttempt(a, "failed", "协议未返回二维码")
		return
	}
	m.mu.Lock()
	if m.attempts[a.TaskID] != a || ctx.Err() != nil {
		m.mu.Unlock()
		return
	}
	a.scanID = s.ID
	a.Image = s.Image
	a.Status = "pending"
	m.mu.Unlock()
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			status := "cancelled"
			msg := "扫码已取消"
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				status = "timeout"
				msg = "扫码超过 5 分钟，请重新获取"
			}
			m.updateAttempt(a, status, msg)
			return
		case <-tick.C:
		}
		s, e = protocol.Poll(ctx, s.ID)
		if e != nil {
			if ctx.Err() != nil {
				continue
			}
			m.updateAttempt(a, "pending", "暂时无法检查扫码状态，正在重试")
			continue
		}
		if s.Account != nil && s.Account.Ref != "" {
			m.mu.Lock()
			if ctx.Err() != nil || m.attempts[a.TaskID] != a || terminal(a.Status) {
				m.mu.Unlock()
				return
			}
			old, exists := m.doc.Bindings[a.TaskID]
			m.doc.Bindings[a.TaskID] = Binding{Protocol: a.Protocol, Account: *s.Account, Status: "alive"}
			if e = m.saveLocked(); e != nil {
				if exists {
					m.doc.Bindings[a.TaskID] = old
				} else {
					delete(m.doc.Bindings, a.TaskID)
				}
				a.Status = "failed"
				a.Message = "账号绑定保存失败，原绑定已保留"
			} else {
				a.Status = "confirmed"
				a.Message = "协议账号已登录；业务授权尚未接入"
			}
			a.Image = ""
			m.mu.Unlock()
			return
		}
		msg := ""
		if s.Status == "verification" {
			msg = "微信要求额外验证，请在微信端完成后继续等待；若无法完成请重新扫码"
		}
		if s.Status == "expired" {
			msg = "二维码已过期，请重新获取"
		}
		m.updateAttempt(a, s.Status, msg)
		if terminal(s.Status) {
			return
		}
	}
}
func (m *Manager) checkAccounts(ctx context.Context, k string) {
	bindings := m.Bindings()
	results := map[string]string{}
	var p Protocol = adapt(m.client(k))
	for _, b := range bindings {
		if b.Protocol != k {
			continue
		}
		if ctx.Err() != nil {
			return
		}
		if _, ok := results[b.Account.Ref]; ok {
			continue
		}
		lock := m.accountLock(k, b.Account.Ref)
		lock.Lock()
		state, e := p.Maintain(ctx, b.Account)
		lock.Unlock()
		if e != nil {
			state = "unknown"
		}
		results[b.Account.Ref] = state
	}
	if ctx.Err() != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, b := range m.doc.Bindings {
		if b.Protocol == k {
			if state, ok := results[b.Account.Ref]; ok {
				b.Status = state
				m.doc.Bindings[id] = b
			}
		}
	}
	if m.saveLocked() != nil {
		m.messages[k] = "账号检查结果保存失败"
	}
}
func (m *Manager) DeleteTask(ctx context.Context, id int64) error {
	m.mu.Lock()
	if a := m.attempts[id]; a != nil {
		if a.cancel != nil {
			a.cancel()
		}
		delete(m.attempts, id)
	}
	binding, exists := m.doc.Bindings[id]
	m.mu.Unlock()
	if exists {
		var p Protocol = adapt(m.client(binding.Protocol))
		lock := m.accountLock(binding.Protocol, binding.Account.Ref)
		lock.Lock()
		err := p.Logout(ctx, binding.Account)
		lock.Unlock()
		if err != nil {
			return errors.New("协议账号退出未确认，任务尚未删除，请启动所属协议后重试")
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.doc.Bindings, id)
	if e := m.saveLocked(); e != nil {
		if exists {
			m.doc.Bindings[id] = binding
		}
		return e
	}
	return nil
}
