package agent

import (
	"context"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/store"
	"robot/apps/local-agent/internal/tasks"
	"robot/apps/local-agent/internal/userkey"
	"testing"
	"time"
)

const agentKeySample = `{"encrypt_key":"MDEyMzQ1Njc4OWFiY2RlZg==","iv":"0123456789abcdef","version":54,"create_time":1000,"expire_in":50}`

func TestStopCancelsStagedKeyWaitAndPreservesNoLateOrder(t *testing.T) {
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts, err := tasks.New(t.TempDir(), db)
	if err != nil {
		t.Fatal(err)
	}
	task, err := ts.Create()
	if err != nil {
		t.Fatal(err)
	}
	cache := &userkey.Cache{}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		_, _ = cache.Get(t.Context(), func(context.Context) (userkey.Material, error) {
			close(entered)
			<-release
			return userkey.Parse([]byte(agentKeySample), time.Now())
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	bctx, bcancel := context.WithCancel(t.Context())
	defer bcancel()
	b := &businessRun{ctx: bctx, cancel: bcancel, keys: cache}
	r := &inventoryRun{ctx: ctx, cancel: cancel, queryCancel: func() {}, business: b, status: "monitoring", plan: &inventory.Plan{Source: "default"}}
	s := &Service{tasks: ts, inventory: inventoryState{runs: map[int64]*inventoryRun{task.ID: r}}}
	s.stageInventoryOrderLocked(task.ID, r)
	if r.status != "pending_order" || r.plan == nil || r.plan.Source != "default" {
		t.Fatal("waiting discarded the complete plan")
	}
	s.mu.Lock()
	s.stopInventoryLocked(task.ID, "stopped", "已停止")
	s.mu.Unlock()
	s.inventory.wg.Wait()
	close(release)
	<-done
	if r.status != "stopped" || r.plan != nil || r.orderPrepared {
		t.Fatal("late key result revived stopped order")
	}
}

func TestNewBusinessSessionInvalidatesCachedKeys(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	b := &businessRun{ctx: ctx, cancel: func() {}, binding: protocols.Binding{Protocol: "wechat", Account: protocols.Account{Ref: "test-account"}}}
	s := &Service{}
	s.adoptOrderSessionLocked(1, b, business.Session{Token: "test-token", UserID: "99", AuthCode: "first-code"})
	calls := 0
	fetch := func(context.Context) (userkey.Material, error) {
		calls++
		return userkey.Parse([]byte(agentKeySample), time.Now())
	}
	if _, err := b.keys.Get(t.Context(), fetch); err != nil {
		t.Fatal(err)
	}
	s.adoptOrderSessionLocked(1, b, business.Session{Token: "new-token", UserID: "99", AuthCode: "second-code"})
	if _, err := b.keys.Get(t.Context(), fetch); err != nil || calls != 2 {
		t.Fatal("new login reused old key material")
	}
	s.stopOrderKeysLocked(b)
	s.businessWG.Wait()
}
