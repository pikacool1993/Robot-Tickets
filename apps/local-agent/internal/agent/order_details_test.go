package agent

import (
	"context"
	"testing"

	"robot/apps/local-agent/internal/business"
)

func TestOrderDetailPersistenceAndAfterSaleUseSavedCredentials(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	s.saveCreatedOrderLocked(r.task.ID, r, *r.plan, "71", "order-token", nil)
	detailCalls, cancelCalls := 0, 0
	s.businessClient = &stockStub{
		detail: func(_ context.Context, _ business.Config, session business.Session, id string) (business.OrderDetail, error) {
			detailCalls++
			if session.Token != "order-token" || session.UserID != "1" || id != "71" {
				t.Errorf("details used credentials other than the order snapshot: %+v id=%s", session, id)
			}
			return business.OrderDetail{ID: 71, Status: 5, OrderEndTime: "2026-11-17 12:30:00", RefundEnabled: true}, nil
		},
		cancel: func(_ context.Context, _ business.Config, session business.Session, id int64) error {
			cancelCalls++
			if session.Token != "order-token" || session.UserID != "1" || id != 71 {
				t.Errorf("cancel used credentials other than the order snapshot: %+v id=%d", session, id)
			}
			return nil
		},
	}
	if err := s.RefreshOrderDetail(r.task.ID, "71"); err != nil {
		t.Fatal(err)
	}
	if err := s.OrderAfterSale(r.task.ID, "71", "cancel"); err != nil {
		t.Fatal(err)
	}
	if detailCalls != 2 || cancelCalls != 1 {
		t.Fatalf("unexpected request counts: details=%d cancel=%d", detailCalls, cancelCalls)
	}
	history, err := s.GetOrderHistory()
	if err != nil || len(history) != 1 || history[0].Details.ID != 71 || history[0].Details.OrderEndTime == "" {
		t.Fatalf("refreshed details were not persisted: %+v err=%v", history, err)
	}
	if !history[0].Details.RefundEnabled {
		t.Fatal("refund eligibility was not retained in the local order snapshot")
	}
}
