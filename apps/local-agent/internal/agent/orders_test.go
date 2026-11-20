package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/cloud"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/orders"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/store"
	"robot/apps/local-agent/internal/tasks"
	"robot/apps/local-agent/internal/userkey"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type orderProtocolStub struct {
	*protocols.Manager
	bindings map[int64]protocols.Binding
}

func (p *orderProtocolStub) Bindings() map[int64]protocols.Binding { return p.bindings }

type orderSenderFunc func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error)

func (f orderSenderFunc) CreateOrder(ctx context.Context, cfg business.Config, s business.Session, path string, b any) (business.OrderResult, error) {
	return f(ctx, cfg, s, path, b)
}

type stockStub struct {
	*business.Client
	query  func(context.Context, business.Config, business.Session, string) ([]byte, error)
	detail func(context.Context, business.Config, business.Session, string) (business.OrderDetail, error)
	cancel func(context.Context, business.Config, business.Session, int64) error
	refund func(context.Context, business.Config, business.Session, int64) error
}

func (s *stockStub) Stock(ctx context.Context, cfg business.Config, session business.Session, id string) ([]byte, error) {
	return s.query(ctx, cfg, session, id)
}

func (s *stockStub) MatchOrderInfo(ctx context.Context, cfg business.Config, session business.Session, id string) (business.OrderDetail, error) {
	if s.detail == nil {
		return business.OrderDetail{}, errors.New("test order detail unavailable")
	}
	return s.detail(ctx, cfg, session, id)
}
func (s *stockStub) CancelMatchOrder(ctx context.Context, cfg business.Config, session business.Session, id int64) error {
	if s.cancel == nil {
		return errors.New("test cancel unavailable")
	}
	return s.cancel(ctx, cfg, session, id)
}
func (s *stockStub) RefundMatchOrder(ctx context.Context, cfg business.Config, session business.Session, id int64) error {
	if s.refund == nil {
		return errors.New("test refund unavailable")
	}
	return s.refund(ctx, cfg, session, id)
}

func orderFixture(t *testing.T, n int) (*Service, []*inventoryRun, userkey.Material) {
	t.Helper()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ts, err := tasks.New(t.TempDir(), db)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	s := &Service{ctx: ctx, store: db, tasks: ts, state: State{LoggedIn: true}, session: &cloud.Session{Token: "agent-token", ExpiresAt: time.Now().Add(time.Hour)}, businessRuns: map[int64]*businessRun{}, inventory: inventoryState{runs: map[int64]*inventoryRun{}, settings: inventory.Defaults()}}
	p := &orderProtocolStub{bindings: map[int64]protocols.Binding{}}
	s.protocols = p
	key, err := userkey.Parse([]byte(agentKeySample), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var runs []*inventoryRun
	for i := 0; i < n; i++ {
		task, err := ts.Create()
		if err != nil {
			t.Fatal(err)
		}
		task.Mode = "area"
		task.Selected = []string{"109"}
		task.PeopleSelected = []string{"12"}
		binding := protocols.Binding{Protocol: "wechat", Account: protocols.Account{Ref: fmt.Sprint(task.ID)}, Status: "alive", Generation: "current"}
		p.bindings[task.ID] = binding
		b := &businessRun{ctx: ctx, cancel: func() {}, binding: binding, session: business.Session{Token: fmt.Sprintf("token-%d", task.ID), UserID: fmt.Sprint(task.ID), AuthCode: "test-code"}, keys: &userkey.Cache{}}
		_, err = b.keys.Get(ctx, func(context.Context) (userkey.Material, error) { return key, nil })
		if err != nil {
			t.Fatal(err)
		}
		s.businessRuns[task.ID] = b
		rctx, rcancel := context.WithCancel(ctx)
		match := protocol.MatchConfig{ID: "101", Name: "test-match"}
		r := &inventoryRun{ctx: rctx, cancel: rcancel, queryCancel: func() {}, business: b, status: "pending_order", task: task, match: match, people: []business.Person{{ID: "12", UID: "34", Name: "test-viewer", Card: "masked-card", FullCard: "private-card", Timestamp: 100, Signature: "viewer-signature"}}, catalog: tasks.Catalog{Tiers: []tasks.Tier{{Tier: "A", Areas: []tasks.Area{{Code: "109", IsSaleable: true}}}}}, plan: &inventory.Plan{MatchID: "101", Source: "default", Parts: []inventory.Allocation{{Region: inventory.Region{ID: 1, Group: 1, Estate: 1, Name: "109", Price: "100.00", Count: 1}, Quantity: 1}}}}
		if err := r.prepareOrderPeople(); err != nil {
			t.Fatal(err)
		}
		s.inventory.runs[task.ID] = r
		runs = append(runs, r)
	}
	t.Cleanup(func() {
		cancel()
		s.inventory.wg.Wait()
		s.businessWG.Wait()
		s.orderDetailWG.Wait()
		s.executionLogs.wg.Wait()
		db.Close()
	})
	return s, runs, key
}

func TestOrderFailureWaitsThenQueriesAndRematches(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	id := r.task.ID
	delay := 80 * time.Millisecond
	var calls, queries atomic.Int32
	var failedAt time.Time
	queriedAt := make(chan time.Time, 1)
	s.businessClient = &stockStub{query: func(ctx context.Context, _ business.Config, session business.Session, match string) ([]byte, error) {
		if session.Token != r.business.session.Token || match != "101" {
			t.Error("stock used wrong account/match")
		}
		queries.Add(1)
		queriedAt <- time.Now()
		return []byte(`{"code":1,"btn_status":1,"btn_text":"立即购买","data":[{"region":1,"list":[{"id":2,"estate":2,"name":"109","price":"100.00","usable_count":2}]}]}`), nil
	}}
	s.orderClient = orderSenderFunc(func(ctx context.Context, _ business.Config, session business.Session, path string, b any) (business.OrderResult, error) {
		var req orders.Request
		raw, err := b.(business.OrderJSONBody).OrderJSON()
		if err != nil || json.Unmarshal(raw, &req) != nil {
			t.Fatal("invalid encoded order", err)
		}
		if !strings.Contains(path, "lid2="+session.UserID+"&") || req.EncryptedData == "" || len(req.Users) != 1 {
			t.Error("bad submission")
		}
		if calls.Add(1) == 1 {
			failedAt = time.Now()
			return business.OrderResult{Code: 5, Delay: delay}, nil
		}
		if req.Regions[0].Estate != 2 || req.Regions[0].Count != 2 {
			t.Error("reused failed/default plan instead of rematching")
		}
		return business.OrderResult{Code: 1, OrderID: "34726"}, nil
	})
	s.submitInventoryOrder(r.ctx, id, r, key)
	s.mu.Lock()
	if r.status != "order_cooldown" || r.plan != nil || r.early || r.retryAt.IsZero() {
		t.Fatal("did not enter cooldown")
	}
	s.deliverInventoryLocked(99, []inventory.Region{{ID: 2, Group: 1, Estate: 2, Name: "109", Price: "100.00", Count: 5}}, time.Now())
	s.inventoryDueLocked(id, r)
	if r.status != "order_cooldown" || r.plan != nil || calls.Load() != 1 {
		t.Fatal("notification/deadline bypassed cooldown")
	}
	s.mu.Unlock()
	select {
	case at := <-queriedAt:
		if at.Sub(failedAt) < delay {
			t.Fatal("queried before timeout elapsed")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("polling did not resume")
	}
	s.inventory.wg.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	if calls.Load() != 2 || queries.Load() != 1 || r.status != "order_created" || r.order == nil || r.order.ID != "34726" {
		t.Fatalf("retry/success failed: calls=%d queries=%d status=%s", calls.Load(), queries.Load(), r.status)
	}
	if r.business.ctx.Err() != nil {
		t.Fatal("success stopped business session")
	}
	raw, err := s.store.Get(fmt.Sprintf("created-order.v1.%d.34726", id))
	if err != nil || len(raw) == 0 || strings.Contains(string(raw), "private-card") || strings.Contains(string(raw), "viewer-signature") || strings.Contains(string(raw), "test-code") {
		t.Fatal("order record missing or persisted secrets")
	}
	var record OrderRecord
	if json.Unmarshal(raw, &record) != nil || record.Viewers[0].ID != "12" || record.Plan.Parts[0].Region.Estate != 2 {
		t.Fatal("incomplete order record")
	}
}

func TestDebugOrderRequestIsWrittenToTaskLog(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	s.debugMode = true
	var sentPath string
	var sentBody orders.Request
	s.orderClient = orderSenderFunc(func(_ context.Context, _ business.Config, _ business.Session, path string, body any) (business.OrderResult, error) {
		sentPath = path
		raw, err := body.(business.OrderJSONBody).OrderJSON()
		if err != nil || json.Unmarshal(raw, &sentBody) != nil {
			t.Fatal("invalid encoded order", err)
		}
		return business.OrderResult{Code: 1, OrderID: "34726"}, nil
	})

	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)

	encoded, err := json.Marshal(sentBody)
	if err != nil {
		t.Fatal(err)
	}
	var logs []tasks.Log
	for _, task := range s.tasks.Snapshot().Tasks {
		if task.ID == r.task.ID {
			logs = task.Logs
			break
		}
	}
	linkFound, paramsFound := false, false
	for _, entry := range logs {
		linkFound = linkFound || strings.Contains(entry.Text, "[DEBUG] N5 POST 下单请求链接："+business.N5+sentPath)
		paramsFound = paramsFound || strings.Contains(entry.Text, "[DEBUG] N5 POST 下单请求参数："+string(encoded))
	}
	if !linkFound || !paramsFound {
		t.Fatalf("debug task log missing completed request link or exact body: link=%t params=%t", linkFound, paramsFound)
	}
	var combined strings.Builder
	for _, entry := range logs {
		combined.WriteString(entry.Text)
		combined.WriteByte('\n')
	}
	if strings.Contains(combined.String(), r.business.session.Token) {
		t.Fatal("business authorization token must not be written to task logs")
	}
}

func TestOrderRequestDetailsOmittedWhenDebugIsDisabled(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		return business.OrderResult{Code: 1, OrderID: "34726"}, nil
	})
	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	for _, task := range s.tasks.Snapshot().Tasks {
		if task.ID != r.task.ID {
			continue
		}
		for _, entry := range task.Logs {
			if strings.Contains(entry.Text, "N5 POST 下单请求链接") || strings.Contains(entry.Text, "N5 POST 下单请求参数") {
				t.Fatal("request details must not be logged when DEBUG is disabled")
			}
		}
		return
	}
	t.Fatal("task missing from task log snapshot")
}

func TestOrderInFlightIsPerTaskAndSingleSubmission(t *testing.T) {
	s, runs, key := orderFixture(t, 2)
	entered := make(chan int64, 2)
	release := make(chan struct{})
	s.orderClient = orderSenderFunc(func(ctx context.Context, _ business.Config, session business.Session, _ string, _ any) (business.OrderResult, error) {
		var id int64
		fmt.Sscan(session.UserID, &id)
		entered <- id
		select {
		case <-release:
		case <-ctx.Done():
			return business.OrderResult{}, ctx.Err()
		}
		return business.OrderResult{Code: 1, OrderID: fmt.Sprint(30000 + id)}, nil
	})
	done := make(chan struct{}, 2)
	for _, r := range runs {
		go func() { s.submitInventoryOrder(r.ctx, r.task.ID, r, key); done <- struct{}{} }()
	}
	for i := 0; i < 2; i++ {
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("tasks were serialized")
		}
	}
	for _, r := range runs {
		if !s.submitInventoryOrder(r.ctx, r.task.ID, r, key) {
			t.Fatal("duplicate not rejected")
		}
		s.mu.Lock()
		s.stageInventoryOrderLocked(r.task.ID, r)
		if r.status != "submitting" || !r.orderInFlight {
			t.Fatal("in-flight state lost")
		}
		s.mu.Unlock()
	}
	select {
	case <-entered:
		t.Fatal("duplicate request sent")
	default:
	}
	// Cancellation must not allow either task's response to revive execution.
	s.mu.Lock()
	for _, r := range runs {
		s.stopInventoryLocked(r.task.ID, "stopped", "已停止")
	}
	s.mu.Unlock()
	close(release)
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("stop did not finish waiting for in-flight submission")
		}
	}
	for _, r := range runs {
		if r.status != "order_created" || r.orderInFlight || r.order == nil {
			t.Fatal("late success was not saved while keeping the task stopped")
		}
	}
}

func TestOrderUnknownResultsWaitBeforePolling(t *testing.T) {
	for _, tt := range []struct {
		name   string
		result business.OrderResult
		err    error
	}{
		{"timeout", business.OrderResult{}, context.DeadlineExceeded},
		{"network", business.OrderResult{}, errors.New("network")},
		{"invalid-response", business.OrderResult{}, errors.New("invalid JSON")},
		{"missing-order-id", business.OrderResult{Code: 1}, nil},
		{"invalid-order-id", business.OrderResult{Code: 1}, errors.New("missing valid order ID")},
		{"other-business-code", business.OrderResult{Code: 2}, nil},
		{"other-business-code-timeout", business.OrderResult{Code: 2, Delay: 2 * time.Second}, nil},
		{"unknown-business-code", business.OrderResult{Code: 12345}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, runs, key := orderFixture(t, 1)
			r := runs[0]
			calls := 0
			s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
				calls++
				return tt.result, tt.err
			})
			before := s.calibratedNowLocked()
			want := tt.result.Delay
			if tt.err != nil || want <= 0 {
				want = time.Second
			}
			s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
			s.mu.Lock()
			s.stageInventoryOrderLocked(r.task.ID, r)
			s.inventoryDueLocked(r.task.ID, r)
			s.deliverInventoryLocked(99, []inventory.Region{{ID: 1, Group: 1, Estate: 1, Name: "109", Price: "100.00", Count: 3}}, time.Now())
			status, plan, retryAt := r.status, r.plan, r.retryAt
			s.stopInventoryLocked(r.task.ID, "stopped", "已停止")
			s.mu.Unlock()
			if calls != 1 || r.order != nil {
				t.Fatal("response resubmitted or marked success")
			}
			if tt.result.Code == 1 {
				if status != "order_verifying" || !retryAt.IsZero() {
					t.Fatal("missing order ID must pause without a retry timer")
				}
			} else if status != "order_cooldown" || plan != nil || retryAt.Before(before.Add(want)) || retryAt.After(s.calibratedNowLocked().Add(want)) {
				t.Fatal("uncertain result/other code used incorrect delay or notification bypassed waiting")
			}
		})
	}
}

func TestMissingOrderIDDoesNotBlockOtherTasks(t *testing.T) {
	s, runs, key := orderFixture(t, 2)
	s.orderClient = orderSenderFunc(func(_ context.Context, _ business.Config, session business.Session, _ string, _ any) (business.OrderResult, error) {
		if session.UserID == runs[0].business.session.UserID {
			return business.OrderResult{Code: 1}, errors.New("invalid order ID")
		}
		return business.OrderResult{Code: 1, OrderID: "34726"}, nil
	})
	for _, r := range runs {
		s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	}
	if runs[0].status != "order_verifying" || runs[1].status != "order_created" {
		t.Fatal("verification blocked another task or resumed itself")
	}
	s.mu.Lock()
	locked := s.inventoryActiveLocked(runs[0].task.ID)
	s.mu.Unlock()
	if !locked {
		t.Fatal("verifying task permits direct restart/config changes")
	}
}

func TestUnknownOrderResumesPollingAfterFullDelay(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	queried := make(chan time.Time, 1)
	s.businessClient = &stockStub{query: func(context.Context, business.Config, business.Session, string) ([]byte, error) {
		select {
		case queried <- time.Now():
		default:
		}
		return []byte(`{"code":1,"btn_status":1,"btn_text":"立即购买","data":[]}`), nil
	}}
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		return business.OrderResult{}, context.DeadlineExceeded
	})
	start := time.Now()
	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	select {
	case at := <-queried:
		if at.Sub(start) < time.Second {
			t.Fatal("polled before 1 second wait ended")
		}
	case <-time.After(9 * time.Second):
		t.Fatal("inventory polling did not resume")
	}
	s.mu.Lock()
	if r.early || r.plan != nil || r.status != "monitoring" {
		t.Error("resumed with default plan or wrong state")
	}
	s.stopInventoryLocked(r.task.ID, "stopped", "已停止")
	s.mu.Unlock()
	s.inventory.wg.Wait()
}

func TestSimulatedOrderRefreshCancelAndRefundNeverUseOfficialClient(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	now := time.Now()
	viewer := OrderViewer{ID: "12", Name: "test-viewer", Phone: "13800000000"}
	plan := inventory.Plan{MatchID: "101", Parts: []inventory.Allocation{{Region: inventory.Region{Group: 1, Estate: 1, Name: "109", Price: "100.00"}, Quantity: 1}}}
	s.inventory.orders = map[string]*OrderRecord{}
	for _, test := range []struct {
		id     string
		status int
		action string
		want   int
	}{{"SIM-cancel", 5, "cancel", 4}, {"SIM-refund", 6, "refund", 3}} {
		detail := simulatedOrderDetail(test.id, []OrderViewer{viewer}, plan, now)
		detail.Status = test.status
		if test.status == 6 {
			detail.Payed = detail.Payable
		}
		record := &OrderRecord{Simulated: true, ID: test.id, TaskID: r.task.ID, UserID: "1", Token: "LOCAL-SIMULATION", Match: r.match, ViewerIDs: []string{"12"}, Viewers: []OrderViewer{viewer}, Plan: plan, CreatedAt: now, Details: detail}
		key := fmt.Sprintf("created-order.v1.%d.%s", r.task.ID, test.id)
		raw, _ := json.Marshal(record)
		if err := s.store.Put(key, raw); err != nil {
			t.Fatal(err)
		}
		s.inventory.orders[key] = record
		if err := s.RefreshOrderDetail(r.task.ID, test.id); err != nil {
			t.Fatal(err)
		}
		if err := s.OrderAfterSale(r.task.ID, test.id, test.action); err != nil {
			t.Fatal(err)
		}
		items, err := s.GetOrderHistory()
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range items {
			if item.ID == test.id {
				found = item.Simulated && item.Details.Status == test.want
			}
		}
		if !found {
			t.Fatalf("simulated %s flow not persisted", test.action)
		}
	}
}

func TestStopCancelsOrderCooldown(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	s.mu.Lock()
	s.orderCooldownLocked(r.task.ID, r, time.Hour)
	s.stopInventoryLocked(r.task.ID, "stopped", "已停止")
	s.mu.Unlock()
	s.inventory.wg.Wait()
	if r.status != "stopped" || r.plan != nil || !r.retryAt.IsZero() {
		t.Fatal("stopped cooldown revived run")
	}
}
