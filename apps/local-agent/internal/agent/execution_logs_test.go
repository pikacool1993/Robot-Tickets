package agent

import (
	"context"
	"robot/apps/local-agent/internal/business"
	"testing"
	"time"
)

func TestBlockedExecutionLogsDoNotDelayStockRecoveryOrOrders(t *testing.T) {
	s, runs, _ := orderFixture(t, 2)
	blocked := make(chan struct{})
	releaseLogs := make(chan struct{})
	releaseOrders := make(chan struct{})
	defer close(releaseLogs)
	defer close(releaseOrders)
	firstWrite := true
	s.executionLogs.persist = func(entry executionLog) error {
		if firstWrite {
			firstWrite = false
			close(blocked)
			<-releaseLogs
		}
		return s.tasks.AppendLogAt(entry.taskID, entry.level, entry.message, entry.at)
	}
	s.queueExecutionLog(runs[0].task.ID, "info", "earlier event")
	select {
	case <-blocked:
	case <-time.After(time.Second):
		t.Fatal("writer did not start")
	}
	for _, r := range runs {
		r.plan = nil
		r.status = "monitoring"
		r.early = false
	}
	first := runs[0]
	first.queryError = "previous error"
	first.buttonHint = "暂未开售"
	s.businessClient = &stockStub{query: func(context.Context, business.Config, business.Session, string) ([]byte, error) {
		return []byte(`{"code":1,"btn_status":1,"data":[{"region":1,"list":[{"id":1,"estate":1,"name":"109","price":"100","usable_count":2}]}]}`), nil
	}}
	entered := make(chan string, 2)
	s.orderClient = orderSenderFunc(func(_ context.Context, _ business.Config, session business.Session, _ string, _ any) (business.OrderResult, error) {
		entered <- session.UserID
		<-releaseOrders
		return business.OrderResult{Code: 1, OrderID: "12345"}, nil
	})
	s.inventory.wg.Add(1)
	go func() { defer s.inventory.wg.Done(); s.pollInventory(first.task.ID, first, first.ctx, time.Now()) }()
	for range runs {
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("blocked log storage delayed stock match/order send")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if first.queryError != "" || first.buttonHint != "" {
		t.Fatal("recovery state was not applied")
	}
	for _, r := range runs {
		if !r.orderInFlight || r.status != "submitting" {
			t.Fatal("order state not reserved before sending")
		}
	}
}
