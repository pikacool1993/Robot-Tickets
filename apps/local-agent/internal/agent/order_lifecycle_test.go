package agent

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/tasks"
	"robot/apps/local-agent/internal/userkey"
)

func reloadOrderState(t *testing.T, s *Service, r *inventoryRun) *Service {
	t.Helper()
	next := &Service{ctx: s.ctx, store: s.store, tasks: s.tasks, session: s.session, state: s.state}
	if err := next.initInventory(); err != nil {
		t.Fatal(err)
	}
	next.reconcileMatchRunsLocked(r.match)
	return next
}

func TestDurableOrderSnapshotAndHistoryDeletion(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	r.people[0].Phone = "138****0000"
	r.people[0].FullPhone = "13812340000"
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		return business.OrderResult{Code: 1, OrderID: "12345"}, nil
	})
	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	raw, err := s.store.Get("created-order.v1.1.12345")
	if err != nil {
		t.Fatal(err)
	}
	var record OrderRecord
	if json.Unmarshal(raw, &record) != nil || record.Token != "token-1" || record.Account != "wechat:1" || record.Viewers[0].Phone != "13812340000" {
		t.Fatalf("missing immutable order credentials: %+v", record)
	}
	if len(s.inventory.pending) != 0 {
		t.Fatal("successful submission marker retained")
	}
	next := reloadOrderState(t, s, r)
	if len(next.inventory.runs) != 0 || len(next.inventory.orders) != 1 {
		t.Fatal("restart restored execution state or lost successful history")
	}
	history, err := s.GetOrderHistory()
	if err != nil || len(history) != 1 || history[0].Viewers[0].Phone != "" {
		t.Fatal("history exposed raw phone or omitted record", err)
	}
	if err = s.DeleteOrderRecord(r.task.ID, "12345"); err != nil {
		t.Fatal(err)
	}
	if r.status != "order_created" || r.order.ID != "12345" {
		t.Fatal("deleting history changed current task state")
	}
}

func TestRestartIgnoresUnfinishedExecutionAndLegacyMarkers(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	configureLifecycleTask(t, s, r)
	s.saveCreatedOrderLocked(r.task.ID, r, *r.plan, "11111", "original-token", []OrderViewer{{ID: "12"}})
	s.rememberSubmissionLocked(s.orderSnapshotLocked(r.task.ID, r, *r.plan))
	for _, prefix := range []string{"order-pending.v1.", "order-state.v1."} {
		if err := s.store.Put(prefix+"1", []byte(`{"taskId":1,"status":"order_verifying"}`)); err != nil {
			t.Fatal(err)
		}
	}
	next := reloadOrderState(t, s, r)
	if len(next.inventory.runs) != 0 || len(next.inventory.pending) != 0 || len(next.inventory.manual) != 0 || len(next.inventory.orders) != 1 {
		t.Fatal("restart restored execution flags or lost history")
	}
	// Keep the logged-in test session and emulate the fresh execution state.
	s.inventory = next.inventory
	s.inventory.synced = true
	s.inventory.wall = time.Now()
	s.inventory.anchor = time.Now()
	s.session.CurrentMatch.SaleAt = time.Now().Add(-time.Hour)
	if err := s.StartInventory(r.task.ID); err != nil {
		t.Fatal("restart could not start normally", err)
	}
	_ = s.StopInventory(r.task.ID)
}

func TestRestartClearsRuntimePurchaseBlock(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	message := "您有未支付的订单，请先处理"
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		return business.OrderResult{Code: 3, Message: message}, nil
	})
	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	next := reloadOrderState(t, s, r)
	if len(next.inventory.runs) != 0 || len(next.inventory.manual) != 0 {
		t.Fatal("restart retained runtime purchase block")
	}
	_ = s.StopInventory(r.task.ID)
	if r.status != "order_blocked" || r.message != message {
		t.Fatal("current-process purchase block was cleared")
	}
}

func TestSubmissionDoesNotWritePreparationRecords(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	called := false
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		called = true
		for _, prefix := range []string{"order-pending.v1.", "order-state.v1."} {
			values, err := s.store.ListPrefix(prefix)
			if err != nil || len(values) != 0 {
				t.Error("submission wrote a preparation record", err)
			}
		}
		return business.OrderResult{Code: 1, OrderID: "12345"}, nil
	})
	s.submitInventoryOrder(r.ctx, r.task.ID, r, key)
	if !called || r.status != "order_created" {
		t.Fatal("submission did not proceed normally")
	}
}

func TestOfflineGraceUsesOriginalExpiredKeyAndStopsAtLimit(t *testing.T) {
	for _, elapsed := range []time.Duration{time.Hour, 18 * time.Hour} {
		t.Run(elapsed.String(), func(t *testing.T) {
			s, runs, key := orderFixture(t, 1)
			r := runs[0]
			snapshot := key.Snapshot()
			snapshot.DeadlineUnixMilli = time.Now().Add(-time.Minute).UnixMilli()
			expired := userkey.Restore(snapshot)
			r.business.offlineSince = time.Now().Add(-elapsed)
			bindings := s.protocols.(*orderProtocolStub).bindings
			b := bindings[r.task.ID]
			b.Status = "expired"
			bindings[r.task.ID] = b
			called := false
			s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
				called = true
				return business.OrderResult{Code: 1, OrderID: "12345"}, nil
			})
			s.submitInventoryOrder(r.ctx, r.task.ID, r, expired)
			if called != (elapsed < 18*time.Hour) {
				t.Fatalf("unexpected send at offline duration %s", elapsed)
			}
		})
	}
}

func configureLifecycleTask(t *testing.T, s *Service, r *inventoryRun) {
	t.Helper()
	root := t.TempDir()
	r.catalog.Stadium = "测试球场"
	r.catalog.Tiers[0].Areas[0].Level = "首层看台"
	if err := os.MkdirAll(filepath.Join(root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(r.catalog)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "config", "seat-catalog.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	s.tasks, err = tasks.New(root, s.store)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.tasks.SaveSeats(r.task.ID, "area", []string{"109"}); err != nil {
		t.Fatal(err)
	}
	if err = s.tasks.SaveBusiness(r.task.ID, tasks.BusinessData{Account: "wechat:1", People: r.people, Selected: []string{"12"}, Usable: true, Status: "ready"}); err != nil {
		t.Fatal(err)
	}
	s.session.CurrentMatch = r.match
	s.session.CurrentMatch.Enabled = true
	s.session.CurrentMatch.SaleAt = time.Now().Add(time.Hour)
	s.session.CurrentMatch.MatchAt = time.Now().Add(48 * time.Hour)
	s.inventory.synced = true
	s.inventory.wall = time.Now()
	s.inventory.anchor = time.Now()
	s.businessClient = &stockStub{query: func(ctx context.Context, _ business.Config, _ business.Session, _ string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}}
}

func TestRepurchaseSavesStoppedRoundAndKeepsHistory(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	configureLifecycleTask(t, s, r)
	s.saveCreatedOrderLocked(r.task.ID, r, *r.plan, "11111", "old-token", []OrderViewer{{ID: "12"}})
	r.status = "order_created"
	if err := s.PrepareRepurchase(r.task.ID, []string{"12"}, []string{"109"}, "area"); err != nil {
		t.Fatal(err)
	}
	if r.status != "stopped" || r.order != nil || len(s.inventory.orders) != 1 {
		t.Fatal("repurchase did not preserve independent history")
	}
	next := reloadOrderState(t, s, r)
	if len(next.inventory.runs) != 0 {
		t.Fatal("restart reactivated old round")
	}
	if err := s.StartInventory(r.task.ID); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	current := s.inventory.runs[r.task.ID]
	early := current.early
	s.mu.Unlock()
	if early {
		t.Fatal("repurchase reused first order window/default inventory")
	}
	_ = s.StopInventory(r.task.ID)
}

func TestManualRecoveryAlwaysQueriesRealtime(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	configureLifecycleTask(t, s, r)
	r.status = "order_verifying"
	s.rememberSubmissionLocked(s.orderSnapshotLocked(r.task.ID, r, *r.plan))
	if s.RecoverInventory(r.task.ID, "resolved") == nil {
		t.Fatal("incorrect confirmation bypassed verification")
	}
	if err := s.RecoverInventory(r.task.ID, "confirm-not-created"); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	current := s.inventory.runs[r.task.ID]
	early := current.early
	queryAt := current.queryAt
	s.mu.Unlock()
	if early || queryAt.After(time.Now().Add(time.Second)) || len(s.inventory.pending) != 0 {
		t.Fatal("recovery reused initial window or retained marker")
	}
	_ = s.StopInventory(r.task.ID)
}

func TestMaintenanceIntentAndSaleStopPersistence(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	s.session.CurrentMatch = r.match
	r.status = "monitoring"
	resume, err := s.pauseForMaintenance(r.task.ID)
	if err != nil || !resume || r.status != "maintenance" {
		t.Fatal("running task maintenance intent lost", err)
	}
	if err = s.StopInventory(r.task.ID); err != nil {
		t.Fatal(err)
	}
	if s.maintenanceResume[r.task.ID] {
		t.Fatal("manual stop retained automatic resume intent")
	}
	resume, err = s.pauseForMaintenance(r.task.ID)
	if err != nil || resume {
		t.Fatal("stopped task acquired automatic resume intent", err)
	}
	if err = s.SetSaleStopped(true); err != nil {
		t.Fatal(err)
	}
	next := reloadOrderState(t, s, r)
	if !next.inventory.saleStops[r.match.ID] || next.StartInventory(r.task.ID) == nil {
		t.Fatal("sale stop lost on restart")
	}
	if err = s.SetSaleStopped(false); err != nil {
		t.Fatal(err)
	}
	if s.inventory.saleStops[r.match.ID] || r.status != "stopped" {
		t.Fatal("clearing sale stop resumed task")
	}
}

func TestStopUnknownResultCannotBecomeOrdinaryStart(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		close(entered)
		<-release
		return business.OrderResult{}, errors.New("connection lost")
	})
	go func() { s.submitInventoryOrder(r.ctx, r.task.ID, r, key); close(done) }()
	<-entered
	if err := s.StopInventory(r.task.ID); err != nil {
		t.Fatal(err)
	}
	s.mu.Lock()
	status := r.status
	s.mu.Unlock()
	if status != "stopping" {
		t.Fatal("in-flight order did not enter stopping")
	}
	close(release)
	<-done
	if r.status != "order_verifying" || s.inventory.pending[r.task.ID] == nil {
		t.Fatal("unknown result not held for verification")
	}
	_ = s.StopInventory(r.task.ID)
	if r.status != "order_verifying" || s.StartInventory(r.task.ID) == nil {
		t.Fatal("ordinary stop/start bypassed verification")
	}
}

func TestMaintenanceWaitsForInFlightResult(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	entered, release, submitted := make(chan struct{}), make(chan struct{}), make(chan struct{})
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		close(entered)
		<-release
		return business.OrderResult{Code: 3, Delay: time.Hour}, nil
	})
	go func() { s.submitInventoryOrder(r.ctx, r.task.ID, r, key); close(submitted) }()
	<-entered
	finished := make(chan error, 1)
	go func() { _, err := s.pauseForMaintenance(r.task.ID); finished <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		status := r.status
		s.mu.Unlock()
		if status == "stopping" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("maintenance did not stop further actions")
		}
		time.Sleep(time.Millisecond)
	}
	select {
	case err := <-finished:
		t.Fatal("maintenance completed before in-flight order", err)
	default:
	}
	close(release)
	<-submitted
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
	if r.status != "maintenance" || !r.retryAt.IsZero() || s.inventory.pending[r.task.ID] != nil || !s.maintenanceResume[r.task.ID] {
		t.Fatal("explicit failure did not finish before maintenance")
	}
}

func TestMaintenanceClearsOfflineTimerOnlyAfterAllUpdates(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	b := r.business
	when := time.Now().Add(-time.Hour)
	b.offlineSince = when
	b.refreshReady = true
	if err := s.store.Put("protocol-offline.v1.1", []byte(when.Format(time.RFC3339Nano))); err != nil {
		t.Fatal(err)
	}
	b.keys.Clear()
	s.mu.Lock()
	s.completeMaintenanceLocked(r.task.ID, b)
	s.mu.Unlock()
	if b.offlineSince.IsZero() {
		t.Fatal("missing key cleared offline timer")
	}
	b.keys.Set(key)
	s.mu.Lock()
	s.completeMaintenanceLocked(r.task.ID, b)
	s.mu.Unlock()
	if !b.offlineSince.IsZero() {
		t.Fatal("complete maintenance did not clear timer")
	}
	raw, err := s.store.Get("protocol-offline.v1.1")
	if err != nil || len(raw) != 0 {
		t.Fatal("offline timer retained after full recovery")
	}
}
