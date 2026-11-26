package agent

import (
	"robot/apps/local-agent/internal/orderalarm"
	"testing"
)

func TestSavedNewOrderTriggersAlarmButSimulationDoesNot(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	s.alarm = orderalarm.New(s.store)
	defer s.alarm.Close()
	_ = s.alarm.Enable(true)
	s.saveCreatedOrderLocked(r.task.ID, r, *r.plan, "71", "token", nil)
	if s.alarm.View().Count != 1 {
		t.Fatal("saved order did not trigger")
	}
	s.saveCreatedOrderLocked(r.task.ID, r, *r.plan, "71", "token", nil)
	if s.alarm.View().Count != 1 {
		t.Fatal("duplicate order rang again")
	}
	r.simulated = true
	s.saveCreatedOrderLocked(r.task.ID, r, *r.plan, "SIM-72", "token", nil)
	if s.alarm.View().Count != 1 {
		t.Fatal("simulation rang")
	}
}
