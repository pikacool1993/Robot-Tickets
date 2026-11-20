package agent

import (
	"context"
	"errors"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/userkey"
	"time"
)

type protocolRuntime interface {
	Bindings() map[int64]protocols.Binding
	MiniProgramCode(context.Context, int64, protocols.Binding, string) (string, error)
	UserEncryptionKey(context.Context, int64, protocols.Binding, string) (userkey.Material, error)
	ResumeTask(int64) error
	Attempt(int64) protocols.Attempt
	Begin(int64, string) (protocols.Attempt, error)
	Control(context.Context, string, string, bool) error
	Proxy(string) (string, error)
	Status(string) protocols.Status
	Activate(string)
	Deactivate()
	Ensure(context.Context, string, string, bool) error
	Close()
	AvatarURL(string, protocols.Account) string
	DeleteTask(context.Context, int64) error
}

// The desktop composition root installs the real runtime; unit tests may inject no runtime.
func (s *Service) EnableProtocols() error {
	m, e := protocols.New(s.root, s.state.InstanceID, s.store)
	if e != nil {
		return e
	}
	m.OnTaskLog = func(id int64, level, text string) {
		if err := s.tasks.AppendLog(id, level, text); err != nil {
			s.log.Warn("task log persistence failed")
		}
	}
	s.mu.Lock()
	s.protocols = m
	s.mu.Unlock()
	return nil
}
func (s *Service) ProtocolProxy(kind string) (string, error) {
	if s.protocols == nil {
		return "", errors.New("协议服务未初始化")
	}
	return s.protocols.Proxy(kind)
}
func (s *Service) ProtocolStatus() (protocols.Status, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.requireTaskSession(); e != nil {
		return protocols.Status{}, e
	}
	if s.protocols == nil {
		return protocols.Status{}, errors.New("协议服务未初始化")
	}
	return s.protocols.Status(s.state.AuthProtocol), nil
}
func (s *Service) ControlProtocol(action string, confirmed bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.requireTaskSession(); e != nil {
		return e
	}
	if s.protocols == nil {
		return errors.New("协议服务未初始化")
	}
	ctx, cancel := context.WithTimeout(s.ctx, 20*time.Second)
	defer cancel()
	if confirmed && (action == "stop" || action == "restart") {
		s.cancelInventoryLocked()
	}
	return s.protocols.Control(ctx, s.state.AuthProtocol, action, confirmed)
}
func (s *Service) BeginAuthorization(id int64) (protocols.Attempt, error) {
	if err := s.claimMaintenance(id); err != nil {
		return protocols.Attempt{}, err
	}
	defer s.releaseMaintenance(id)
	_, err := s.pauseForMaintenance(id)
	if err != nil {
		return protocols.Attempt{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.requireTaskSession(); e != nil {
		return protocols.Attempt{}, e
	}
	if s.protocols == nil {
		return protocols.Attempt{}, errors.New("协议服务未初始化")
	}
	for _, t := range s.tasks.Snapshot().Tasks {
		if t.ID == id {
			attempt, err := s.protocols.Begin(id, s.state.AuthProtocol)
			if err == nil {
				if b := s.businessRuns[id]; b != nil {
					b.refresh.authorizationPending = true
				}
			}
			return attempt, err
		}
	}
	return protocols.Attempt{}, errors.New("任务不存在")
}
func (s *Service) Authorization(id int64) (protocols.Attempt, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.requireTaskSession(); e != nil {
		return protocols.Attempt{}, e
	}
	if s.protocols == nil {
		return protocols.Attempt{}, errors.New("协议服务未初始化")
	}
	attempt := s.protocols.Attempt(id)
	if attempt.Status == "failed" || attempt.Status == "expired" || attempt.Status == "timeout" || attempt.Status == "cancelled" {
		if b := s.businessRuns[id]; b != nil {
			b.refresh.authorizationPending = false
		}
		if r := s.inventory.runs[id]; r != nil && r.status == "maintenance" {
			r.status, r.message = "maintenance_failed", "授权未完成，请重试授权或点击停止"
		}
	}
	return attempt, nil
}
