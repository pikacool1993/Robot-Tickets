package agent

import (
	"errors"
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
	return s.tasks.Snapshot(), nil
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
	return s.tasks.Delete(id)
}
