package agent

import (
	"context"
	"fmt"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"testing"
	"time"
)

func TestPurchaseRestrictionsOverrideDelayAndOnlyPauseCurrentTask(t *testing.T) {
	for _, message := range []string{"您有未支付的订单，请先支付！", "此身份证已购买过本场比赛门票", "您已超过最高购票量"} {
		for _, code := range []int{0, 3, 5, 999} {
			t.Run(fmt.Sprintf("%d-%s", code, message), func(t *testing.T) {
				s, runs, key := orderFixture(t, 2)
				r, other := runs[0], runs[1]
				calls := 0
				s.orderClient = orderSenderFunc(func(_ context.Context, _ business.Config, session business.Session, _ string, _ any) (business.OrderResult, error) {
					calls++
					if session.UserID == r.business.session.UserID {
						return business.OrderResult{Code: code, Message: message, Delay: 5 * time.Second}, nil
					}
					return business.OrderResult{Code: 1, OrderID: "34726"}, nil
				})
				s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
				s.mu.Lock()
				s.stageInventoryOrderLocked(r.task.ID, r)
				s.inventoryDueLocked(r.task.ID, r)
				s.deliverInventoryLocked(99, []inventory.Region{{ID: 1, Group: 1, Estate: 1, Name: "109", Price: "100", Count: 3}}, time.Now())
				blocked := s.inventoryActiveLocked(r.task.ID)
				s.mu.Unlock()
				if !blocked || r.status != "order_blocked" || r.message != message || !r.retryAt.IsZero() || r.order != nil || calls != 1 {
					t.Fatal("restriction failed to pause, keep original message or override retry")
				}
				s.submitInventoryOrder(other.ctx, other.task.ID, other, key)
				if calls != 2 || other.status != "order_created" || r.status != "order_blocked" {
					t.Fatal("restriction affected another task")
				}
			})
		}
	}
}

func TestValidOrderSuccessOverridesPurchaseRestrictionMessage(t *testing.T) {
	for _, message := range []string{"您有未支付的订单", "此身份证已购买过本场比赛门票", "您已超过最高购票量"} {
		t.Run(message, func(t *testing.T) {
			s, runs, key := orderFixture(t, 1)
			r := runs[0]
			s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
				return business.OrderResult{Code: 1, OrderID: "34726", Message: message}, nil
			})
			s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
			if r.status != "order_created" || r.order == nil || r.order.ID != "34726" || !r.retryAt.IsZero() {
				t.Fatal("message overrode valid order success")
			}
			raw, err := s.store.Get(fmt.Sprintf("created-order.v1.%d.34726", r.task.ID))
			if err != nil || len(raw) == 0 {
				t.Fatal("valid order was not persisted")
			}
		})
	}
}

func TestCodeZeroAndThreeWaitBeforeResumingInventory(t *testing.T) {
	for _, code := range []int{0, 3} {
		for _, delay := range []time.Duration{0, 2 * time.Second} {
			t.Run(fmt.Sprintf("%d-%s", code, delay), func(t *testing.T) {
				s, runs, key := orderFixture(t, 1)
				r := runs[0]
				s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
					return business.OrderResult{Code: code, Delay: delay}, nil
				})
				before := s.calibratedNowLocked()
				s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
				s.mu.Lock()
				s.deliverInventoryLocked(99, []inventory.Region{{ID: 1, Group: 1, Estate: 1, Name: "109", Price: "100", Count: 3}}, time.Now())
				status, plan, retryAt := r.status, r.plan, r.retryAt
				s.stopInventoryLocked(r.task.ID, "stopped", "已停止")
				s.mu.Unlock()
				want := delay
				if want == 0 {
					want = 5600 * time.Millisecond
					if code == 0 {
						want = time.Second
					}
				}
				if status != "order_cooldown" || plan != nil || retryAt.Before(before.Add(want)) || retryAt.After(s.calibratedNowLocked().Add(want)) {
					t.Fatal("response did not use correct wait or notification bypassed wait")
				}
			})
		}
	}
}
