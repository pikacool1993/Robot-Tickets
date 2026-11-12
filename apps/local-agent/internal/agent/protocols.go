package agent

import (
	"context"
	"errors"
	"robot/apps/local-agent/internal/protocols"
	"time"
)

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
	return s.protocols.Control(ctx, s.state.AuthProtocol, action, confirmed)
}
func (s *Service) BeginAuthorization(id int64) (protocols.Attempt, error) {
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
			return s.protocols.Begin(id, s.state.AuthProtocol)
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
	return s.protocols.Attempt(id), nil
}
