package agent

import "robot/apps/local-agent/internal/orderalarm"

func (s *Service) SetAlarmPlayer(p orderalarm.Player) { s.alarm.SetPlayer(p) }
func (s *Service) GetOrderAlarm() (orderalarm.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return orderalarm.View{}, err
	}
	return s.alarm.View(), nil
}
func (s *Service) SaveOrderAlarm(enabled bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	return s.alarm.Enable(enabled)
}
func (s *Service) StopOrderAlarm() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	s.alarm.Stop()
	return nil
}
func (s *Service) PreviewOrderAlarm() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	s.alarm.Preview()
	return nil
}
