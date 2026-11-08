package agent

import (
	"context"
	"errors"
	"net/url"
	"robot/apps/local-agent/internal/tasks"
	"time"
)

func (s *Service) requireTaskSession() error {
	if s.session == nil || !s.state.LoggedIn || !time.Now().Before(s.session.ExpiresAt) {
		return errors.New("请先登录客户端")
	}
	return nil
}
func (s *Service) TaskView() (tasks.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return tasks.View{}, err
	}
	view := s.tasks.Snapshot()
	if s.protocols != nil {
		bindings := s.protocols.Bindings()
		for i := range view.Tasks {
			if b, ok := bindings[view.Tasks[i].ID]; ok {
				view.Tasks[i].WechatNickname = b.Account.Nickname
				view.Tasks[i].ProtocolStatus = b.Status
				view.Tasks[i].AuthProtocol = b.Protocol
				view.Tasks[i].Paused = b.Paused
				if b.Protocol == "yyb" && b.Account.Ref != "" {
					status := s.protocols.Status(b.Protocol)
					view.Tasks[i].ProtocolAvatar = "http://" + status.Address + "/accounts/avatar?ref=" + url.QueryEscape(b.Account.Ref)
				}
			}
		}
	}
	return view, nil
}
func (s *Service) CreateTask() (tasks.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return tasks.Task{}, err
	}
	return s.tasks.Create()
}
func (s *Service) SaveTaskSeats(id int64, mode string, selected []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	return s.tasks.SaveSeats(id, mode, selected)
}
func (s *Service) DeleteTask(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if s.protocols != nil {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		if err := s.protocols.DeleteTask(ctx, id); err != nil {
			return err
		}
	}
	return s.tasks.Delete(id)
}
