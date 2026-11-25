package agent

import (
	"context"
	"robot/apps/local-agent/internal/business"
	"testing"
)

func TestN5TimeoutResumesWithoutVerification(t *testing.T) {
	s, rs, key := orderFixture(t, 1)
	r := rs[0]
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		return business.OrderResult{}, context.DeadlineExceeded
	})
	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.status != "order_cooldown" || r.plan != nil || r.retryAt.IsZero() {
		t.Fatalf("unexpected timeout state %s", r.status)
	}
	if s.inventory.pending[r.task.ID] != nil {
		t.Fatal("timeout retained unresolved order")
	}
	r.cancel()
}
