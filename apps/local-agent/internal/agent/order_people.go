package agent

import (
	"errors"
	"strings"

	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/orders"
)

// A run owns one immutable combination of session and prepared spectator data.
// Maintenance stops the old run and builds a new one only after refresh succeeds.
type orderPeopleSnapshot struct {
	session business.Session
	people  *orders.PreparedPeople
	viewers []OrderViewer
}

func (r *inventoryRun) prepareOrderPeople() error {
	if r.task.ImportCount > 0 && (len(r.task.PeopleSelected) != r.task.ImportCount || !r.task.PeopleComplete) {
		return errors.New("请先确认全部预定观演人")
	}
	people, err := orders.PreparePeople(r.people, r.task.PeopleSelected)
	if err != nil {
		return err
	}
	viewers := make([]OrderViewer, 0, len(r.task.PeopleSelected))
	for _, p := range people.People() {
		phone := p.FullPhone
		if phone == "" && !strings.Contains(p.Phone, "*") {
			phone = p.Phone
		}
		viewers = append(viewers, OrderViewer{ID: string(p.ID), Name: p.Name, Card: p.Card, Phone: phone})
	}
	r.orderPeople = &orderPeopleSnapshot{session: r.business.session, people: people, viewers: viewers}
	return nil
}

func (r *inventoryRun) checkOrderPeople() error {
	// Internal callers/tests can construct runs directly. Production runs prepare
	// before publication so this fallback does not run on their submission path.
	if r.orderPeople == nil {
		if err := r.prepareOrderPeople(); err != nil {
			return err
		}
	}
	if r.orderPeople.session != r.business.session {
		return errors.New("业务会话已变化，请刷新后重新启动任务")
	}
	return nil
}
