package agent

import (
	"errors"
	"robot/apps/local-agent/internal/ippool"
)

func (s *Service) ipIDs(id int64) ([]int64, error) {
	ids := []int64{}
	for _, t := range s.tasks.Snapshot().Tasks {
		if id == 0 || t.ID == id {
			ids = append(ids, t.ID)
		}
	}
	if id != 0 && len(ids) == 0 {
		return nil, errors.New("任务不存在")
	}
	return ids, nil
}
func (s *Service) GetIPPools() (ippool.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return ippool.View{}, err
	}
	ids, _ := s.ipIDs(0)
	return s.ipPool.Snapshot(ids), nil
}
func (s *Service) SaveIPPool(k string, direct, include bool, threshold, cooldown int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	return s.ipPool.Settings(k, direct, include, threshold, cooldown)
}
func (s *Service) ImportIPPool(k, raw string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return nil, err
	}
	return s.ipPool.Import(k, raw)
}
func (s *Service) UpdateIPPoolEntry(k, id, action, line string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	return s.ipPool.Entry(k, id, action, line)
}
func (s *Service) RandomizeTaskIP(id int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return "", err
	}
	ids, err := s.ipIDs(id)
	if err != nil {
		return "", err
	}
	return s.ipPool.Shuffle(ids), nil
}
func (s *Service) TestIPProxy(k, id string) error {
	s.mu.Lock()
	err := s.requireTaskSession()
	s.mu.Unlock()
	if err != nil {
		return err
	}
	return s.ipPool.Test(s.ctx, k, id)
}
