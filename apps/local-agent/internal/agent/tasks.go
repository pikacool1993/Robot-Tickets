package agent

import (
	"context"
	"errors"
	"robot/apps/local-agent/internal/tasks"
	"time"
)

func (s *Service) requireTaskSession() error {
	if s.ctx != nil && s.ctx.Err() != nil {
		return errors.New("客户端正在退出")
	}
	if s.session == nil || !s.state.LoggedIn || !time.Now().Before(s.session.ExpiresAt) {
		return errors.New("请先登录客户端")
	}
	return nil
}

// TaskPersonDetails returns display fields only, on explicit request.
func (s *Service) TaskPersonDetails(id int64, personID string) (tasks.PersonView, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return tasks.PersonView{}, err
	}
	b, err := s.tasks.Business(id)
	if err != nil {
		return tasks.PersonView{}, err
	}
	if s.protocols == nil {
		return tasks.PersonView{}, errors.New("请先绑定微信账号")
	}
	binding, ok := s.protocols.Bindings()[id]
	if !ok || b.Account != binding.Protocol+":"+binding.Account.Ref {
		return tasks.PersonView{}, errors.New("账号已变化，请刷新观演人")
	}
	for _, p := range b.People {
		if p.ID == personID {
			card, phone := p.FullCard, p.FullPhone
			if card == "" {
				card = p.Card
			}
			if phone == "" {
				phone = p.Phone
			}
			return tasks.PersonView{ID: p.ID, Name: p.Name, Card: card, Phone: phone, Self: p.Self}, nil
		}
	}
	return tasks.PersonView{}, errors.New("观演人不存在，请刷新列表")
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
				view.Tasks[i].ProtocolAvatar = b.Account.Avatar
				view.Tasks[i].ProtocolStatus = b.Status
				view.Tasks[i].AuthProtocol = b.Protocol
				view.Tasks[i].Paused = b.Paused
				cached, _ := s.tasks.Business(view.Tasks[i].ID)
				if cached.Account != b.Protocol+":"+b.Account.Ref {
					view.Tasks[i].People = nil
					view.Tasks[i].PeopleSelected = nil
					view.Tasks[i].SpectatorNames = nil
					view.Tasks[i].PeopleUsable = false
					view.Tasks[i].BusinessStatus = "waiting"
					view.Tasks[i].BusinessMessage = "等待业务登录"
				}
				run := s.businessRuns[view.Tasks[i].ID]
				if b.Status != "alive" || b.Paused || b.Protocol != s.state.AuthProtocol || run == nil || !sameBinding(run.binding, b) {
					view.Tasks[i].PeopleUsable = false
				}
				if b.Protocol == "yyb" && b.Account.Ref != "" {
					view.Tasks[i].ProtocolAvatar = s.protocols.AvatarURL(b.Protocol, b.Account)
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
	if err := s.requireInventoryStoppedLocked(id); err != nil {
		return err
	}
	err := s.tasks.SaveSeats(id, mode, selected)
	if err != nil {
		if logErr := s.tasks.AppendLog(id, "error", "座位要求保存失败，请重试；原配置保留"); logErr != nil {
			s.log.Warn("task log persistence failed")
		}
	}
	return err
}
func (s *Service) DeleteTask(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if err := s.requireInventoryStoppedLocked(id); err != nil {
		return err
	}
	if s.protocols != nil {
		ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
		defer cancel()
		if err := s.protocols.DeleteTask(ctx, id); err != nil {
			if logErr := s.tasks.AppendLog(id, "error", "删除任务失败：协议账号退出未确认，请启动所属协议后重试"); logErr != nil {
				s.log.Warn("task log persistence failed")
			}
			return err
		}
	}
	s.stopTaskBusinessLocked(id)
	return s.tasks.Delete(id)
}

func (s *Service) SaveTaskNote(id int64, note string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	return s.tasks.SaveNote(id, note)
}
func (s *Service) SaveTaskAddress(id int64, address *tasks.ShippingAddress) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	return s.tasks.SaveAddress(id, address)
}
