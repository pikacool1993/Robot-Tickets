package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/ippool"
	"strings"
	"testing"
)

type orderIPTransport func(*http.Request) (*http.Response, error)

func (f orderIPTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOrderIPSnapshotPersistenceAndUpload(t *testing.T) {
	s, rs, key := orderFixture(t, 1)
	r := rs[0]
	pool, err := ippool.New(s.store)
	if err != nil {
		t.Fatal(err)
	}
	s.ipPool = pool
	s.orderClient = orderSenderFunc(func(ctx context.Context, _ business.Config, _ business.Session, _ string, _ any) (business.OrderResult, error) {
		req, _ := http.NewRequestWithContext(ctx, "POST", business.N5+"/test", nil)
		res, e := pool.RoundTrip(req, orderIPTransport(func(*http.Request) (*http.Response, error) {
			// Change assignment while the original direct request is in flight.
			if _, e := pool.Import("N5", "192.0.2.10|2018|u|p|2099-12-31"); e != nil {
				t.Fatal(e)
			}
			pool.Shuffle([]int64{r.task.ID})
			return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("{}"))}, nil
		}))
		if e != nil {
			return business.OrderResult{}, e
		}
		io.ReadAll(res.Body)
		res.Body.Close()
		return business.OrderResult{Code: 1, OrderID: "98765"}, nil
	})
	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.order == nil || r.order.OrderIP != "direct" {
		t.Fatal("order did not preserve request-time exit", r.order)
	}
	raw, e := s.store.Get(fmt.Sprintf("created-order.v1.%d.98765", r.task.ID))
	if e != nil {
		t.Fatal(e)
	}
	var saved OrderRecord
	if json.Unmarshal(raw, &saved) != nil || saved.OrderIP != "direct" {
		t.Fatal("persisted exit lost", string(raw))
	}
	var payload map[string]any
	json.Unmarshal(syncOrderPayload(saved, business.OrderDetail{}, nil), &payload)
	if payload["order_ip"] != "direct" {
		t.Fatal("upload exit lost", payload)
	}
}
