package tasks

import (
	"encoding/json"
	"errors"
	"robot/apps/local-agent/internal/business"
	"strings"
)

type BusinessData struct {
	ImportConfirmed bool              `json:"importConfirmed,omitempty"`
	DeviceID        string            `json:"deviceId"`
	DeviceType      string            `json:"deviceType"`
	Account         string            `json:"account"`
	People          []business.Person `json:"people"`
	Selected        []string          `json:"selected"`
	Status          string            `json:"status"`
	Message         string            `json:"message"`
	Usable          bool              `json:"usable"`
}
type PersonView struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Card  string `json:"card"`
	Phone string `json:"phone"`
	Self  bool   `json:"self"`
}

func cloneBusiness(in map[int64]BusinessData) map[int64]BusinessData {
	out := map[int64]BusinessData{}
	raw, _ := json.Marshal(in)
	_ = json.Unmarshal(raw, &out)
	if out == nil {
		out = map[int64]BusinessData{}
	}
	return out
}
func fillBusinessView(t *Task, b BusinessData) {
	t.BusinessStatus = b.Status
	t.BusinessMessage = b.Message
	t.PeopleUsable = b.Usable
	t.PeopleSelected = append([]string{}, b.Selected...)
	t.PeopleComplete = true
	t.ImportIssue = importedIssue(*t, b)
	if t.ImportIssue != "" {
		t.PeopleComplete = false
	}
	available := map[string]bool{}
	for _, p := range b.People {
		available[p.ID] = p.Available()
	}
	for _, id := range b.Selected {
		if !available[id] {
			t.PeopleComplete = false
		}
	}
	t.People = []PersonView{}
	t.SpectatorNames = []string{}
	for _, p := range b.People {
		if !p.Available() {
			continue
		}
		t.People = append(t.People, PersonView{p.ID, p.Name, maskPersonValue(p.Card), maskPersonValue(p.Phone), p.Self})
		for _, id := range b.Selected {
			if id == p.ID {
				t.SpectatorNames = append(t.SpectatorNames, p.Name)
			}
		}
	}
}
func (s *Service) Business(id int64) (BusinessData, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.doc.Tasks {
		if t.ID == id {
			return cloneBusiness(s.doc.Business)[id], nil
		}
	}
	return BusinessData{}, errors.New("任务不存在")
}
func (s *Service) SaveBusiness(id int64, b BusinessData) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for _, t := range next.Tasks {
		if t.ID == id {
			matchImportedPeople(t, &b)
			next.Business[id] = b
			return s.commit(next)
		}
	}
	return errors.New("任务不存在")
}
func (s *Service) SavePeopleSelection(id int64, ids []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	b, ok := next.Business[id]
	if !ok || !b.Usable {
		return errors.New("业务账号尚未就绪，请先完成业务登录并刷新观演人")
	}
	allowed := map[string]bool{}
	for _, p := range b.People {
		allowed[p.ID] = p.Available()
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !allowed[id] || seen[id] {
			return errors.New("观演人选择已变化，请重新加载")
		}
		seen[id] = true
	}
	b.Selected = append([]string{}, ids...)
	next.Business[id] = b
	for i := range next.Tasks {
		if next.Tasks[i].ID == id {
			b.ImportConfirmed = len(ids) == next.Tasks[i].ImportCount && next.Tasks[i].ImportCount > 0
			next.Business[id] = b
			addLog(&next.Tasks[i], "success", "观演人选择已保存")
		}
	}
	return s.commit(next)
}
func (s *Service) DisableBusiness() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for id, b := range next.Business {
		b.Usable = false
		b.Status = "waiting"
		b.Message = "等待协议账号检查与业务登录"
		next.Business[id] = b
	}
	return s.commit(next)
}

func (s *Service) EnsureBusinessDeviceID(id int64, device string) (string, error) {
	if device != "windows" && device != "android" && device != "ios" {
		return "", errors.New("设备类型无效")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	next := s.clone()
	for _, t := range next.Tasks {
		if t.ID == id {
			b := next.Business[id]
			if b.DeviceID != "" && b.DeviceType == device {
				return b.DeviceID, nil
			}
			generated, err := business.NewDeviceID()
			if err != nil {
				return "", err
			}
			b.DeviceID = generated
			b.DeviceType = device
			next.Business[id] = b
			if err = s.commit(next); err != nil {
				return "", err
			}
			return generated, nil
		}
	}
	return "", errors.New("任务不存在")
}

func maskPersonValue(value string) string {
	if value == "" || strings.Contains(value, "*") {
		return value
	}
	chars := []rune(value)
	if len(chars) <= 7 {
		return "***"
	}
	return string(chars[:3]) + "****" + string(chars[len(chars)-4:])
}
