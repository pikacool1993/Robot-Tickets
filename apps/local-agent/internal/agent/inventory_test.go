package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/cloud"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/store"
	"robot/apps/local-agent/internal/tasks"
	"sync/atomic"
	"testing"
	"time"
)

func TestNonPurchasableButtonDoesNotMatchOrNotifyUntilEnabled(t *testing.T) {
	s, runs, _ := orderFixture(t, 2)
	first, second := runs[0], runs[1]
	first.plan, second.plan = nil, nil
	first.early, second.early = true, true
	first.status, second.status = "monitoring", "monitoring"
	s.inventory.settings = inventory.Settings{QueryMinMS: 5, QueryMaxMS: 5, OrderMinMS: 4500, OrderMaxMS: 7500}
	var calls atomic.Int32
	checked := make(chan int32, 3)
	s.businessClient = &stockStub{query: func(context.Context, business.Config, business.Session, string) ([]byte, error) {
		call := calls.Add(1)
		checked <- call
		buttonFields := `"btn_text":""`
		if call == 2 {
			buttonFields = `"btn_status":0,"btn_text":"暂未开售"`
		} else if call > 2 {
			buttonFields = `"btn_status":1,"btn_text":"立即购买"`
		}
		return []byte(fmt.Sprintf(`{"code":1,%s,"data":[{"region":1,"list":[{"id":2,"estate":1,"name":"109","price":"100.00","usable_count":2}]}]}`, buttonFields)), nil
	}}
	s.inventory.wg.Add(1)
	go func() { defer s.inventory.wg.Done(); s.pollInventory(first.task.ID, first, first.ctx, time.Now()) }()
	select {
	case <-checked:
	case <-time.After(time.Second):
		t.Fatal("first stock query did not run")
	}
	waitForHint := func(want string) {
		t.Helper()
		deadline := time.Now().Add(time.Second)
		for {
			s.mu.Lock()
			waiting := first.plan == nil && second.plan == nil && first.status == "monitoring" && first.message == want
			s.mu.Unlock()
			if waiting {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("unexpected state while waiting for %q", want)
			}
			time.Sleep(time.Millisecond)
		}
	}
	waitForHint("服务端提示：服务端未返回可购状态；继续查询库存")
	select {
	case call := <-checked:
		if call != 2 {
			t.Fatalf("expected second stock response, got call %d", call)
		}
	case <-time.After(time.Second):
		t.Fatal("polling stopped after missing button status")
	}
	waitForHint("服务端提示：暂未开售；继续查询库存")
	deadline := time.Now().Add(time.Second)
	for {
		s.mu.Lock()
		matched := first.plan != nil && second.plan != nil
		if matched && (first.status != "ready" || second.status != "ready" || first.plan.Source != "realtime" || second.plan.Source != "notification") {
			s.mu.Unlock()
			t.Fatal("enabled stock did not notify both matching tasks correctly")
		}
		s.mu.Unlock()
		if matched {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("polling did not resume and notify tasks after purchase button enabled")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestInventoryPollingStateMatrixAndChangeOnlyLogs(t *testing.T) {
	s, runs, _ := orderFixture(t, 2)
	first, second := runs[0], runs[1]
	for _, r := range runs {
		r.plan = nil
		r.early = true
		r.status = "monitoring"
	}
	s.inventory.settings = inventory.Settings{QueryMinMS: 5, QueryMaxMS: 5, OrderMinMS: 4500, OrderMaxMS: 7500}
	var calls atomic.Int32
	checked := make(chan struct{}, 8)
	s.businessClient = &stockStub{query: func(context.Context, business.Config, business.Session, string) ([]byte, error) {
		n := calls.Add(1)
		checked <- struct{}{}
		switch n {
		case 1:
			return []byte(`{"code":1,"btn_status":1,"btn_text":"立即购买","data":[]}`), nil
		case 2, 3:
			return nil, errors.New("synthetic network failure")
		case 4, 5:
			return nil, &business.APIError{HTTP: 200, Code: 996}
		case 6:
			return []byte(`{"code":1,"btn_status":1,"btn_text":"立即购买","data":[]}`), nil
		default:
			return []byte(`{"code":1,"btn_status":1,"btn_text":"立即购买","data":[{"region":1,"list":[{"id":2,"estate":1,"name":"109","price":"100.00","usable_count":2}]}]}`), nil
		}
	}}
	s.inventory.wg.Add(1)
	go func() { defer s.inventory.wg.Done(); s.pollInventory(first.task.ID, first, first.ctx, time.Now()) }()
	awaitQuery := func(n int32) {
		t.Helper()
		deadline := time.After(2 * time.Second)
		for calls.Load() < n {
			select {
			case <-checked:
			case <-deadline:
				t.Fatalf("inventory query %d did not run", n)
			}
		}
	}
	awaitMessage := func(message string) {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for {
			s.mu.Lock()
			ready := first.message == message
			s.mu.Unlock()
			if ready {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("inventory state did not become %q", message)
			}
			time.Sleep(time.Millisecond)
		}
	}
	awaitQuery(1)
	awaitMessage("暂无符合条件的库存")
	s.mu.Lock()
	if first.status != "monitoring" || first.plan != nil || second.plan != nil || first.message != "暂无符合条件的库存" {
		s.mu.Unlock()
		t.Fatal("empty successful response changed the monitoring state")
	}
	s.mu.Unlock()
	awaitQuery(2)
	awaitQuery(3)
	awaitQuery(4)
	awaitQuery(5)
	awaitQuery(6)
	awaitQuery(7)
	deadline := time.Now().Add(2 * time.Second)
	for {
		s.mu.Lock()
		matched := first.plan != nil && second.plan != nil
		correct := first.status == "ready" && second.status == "ready" && first.plan != nil && second.plan != nil && first.plan.Source == "realtime" && second.plan.Source == "notification"
		s.mu.Unlock()
		if matched {
			if !correct {
				t.Fatal("stock hit did not notify and stage matching runs")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("matching inventory did not stage orders")
		}
		time.Sleep(time.Millisecond)
	}
	s.executionLogs.wg.Wait()
	logs := s.tasks.Snapshot().Tasks[0].Logs
	counts := map[string]int{}
	for _, entry := range logs {
		counts[entry.Text]++
	}
	if counts["库存查询异常，按配置间隔继续查询（不表示零库存）"] != 1 || counts["库存查询异常（HTTP 200，业务码 996），按配置间隔继续查询"] != 1 || counts["库存查询已恢复"] != 1 {
		t.Fatalf("query state transitions were not logged once: %+v", counts)
	}
	if counts["库存匹配成功：1人，来源 realtime"] != 1 {
		t.Fatalf("stock hit log missing: %+v", counts)
	}
}

func TestInventoryQueryErrorsDoNotPauseOrCancelDeadline(t *testing.T) {
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
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	qctx, qcancel := context.WithCancel(ctx)
	defer qcancel()
	target := time.Now().Add(time.Second)
	r := &inventoryRun{ctx: ctx, cancel: cancel, queryCancel: qcancel, status: "monitoring", target: target, early: true}
	s := &Service{tasks: ts, inventory: inventoryState{runs: map[int64]*inventoryRun{task.ID: r}}}
	errorsToCheck := []error{errors.New("network failure"), context.DeadlineExceeded}
	for _, code := range []int{-1, 996, 997, 998, 999, 200, 1234} {
		errorsToCheck = append(errorsToCheck, &business.APIError{HTTP: 200, Code: code})
	}
	for _, status := range []int{401, 429, 500, 503} {
		errorsToCheck = append(errorsToCheck, &business.APIError{HTTP: status})
	}
	for _, queryErr := range errorsToCheck {
		s.recordInventoryQueryErrorLocked(task.ID, r, queryErr)
		s.executionLogs.wg.Wait()
		if r.status != "monitoring" || r.plan != nil || r.target != target || ctx.Err() != nil || qctx.Err() != nil || r.message == "" {
			t.Fatalf("query error changed execution state: %v", queryErr)
		}
		before := len(ts.Snapshot().Tasks[0].Logs)
		s.recordInventoryQueryErrorLocked(task.ID, r, queryErr)
		s.executionLogs.wg.Wait()
		if len(ts.Snapshot().Tasks[0].Logs) != before {
			t.Fatal("repeated error flooded logs")
		}
	}
	// Query failures leave the run eligible for the existing default-plan handoff.
	r.plan = &inventory.Plan{Source: "default", Parts: []inventory.Allocation{{Quantity: 2}}}
	s.stageInventoryOrderLocked(task.ID, r)
	if r.status != "pending_order" || r.plan.Source != "default" || qctx.Err() == nil || ctx.Err() != nil {
		t.Fatal("query failures prevented default-plan handoff")
	}
}

func TestStopCancelsAllWorkAndRejectsStaleRun(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	qctx, qcancel := context.WithCancel(ctx)
	r := &inventoryRun{ctx: ctx, cancel: cancel, queryCancel: qcancel, status: "ready", plan: &inventory.Plan{Source: "realtime"}}
	s := &Service{inventory: inventoryState{runs: map[int64]*inventoryRun{1: r}}}
	if !s.inventoryActiveLocked(1) || s.requireInventoryStoppedLocked(1) == nil {
		t.Fatal("running configuration not locked")
	}
	s.stopInventoryLocked(1, "stopped", "已停止")
	if ctx.Err() == nil || qctx.Err() == nil || r.plan != nil || s.inventoryActiveLocked(1) || s.requireInventoryStoppedLocked(1) != nil {
		t.Fatal("stop did not clear and unlock")
	}
	if s.validInventoryLocked(1, r) {
		t.Fatal("stale callback accepted")
	}
	if waitInventory(ctx, time.Hour) {
		t.Fatal("cancelled timer fired")
	}
}

func TestClockFailurePreservesCalibrationAndSettingsPersist(t *testing.T) {
	key := bytes.Repeat([]byte{9}, 32)
	keys := protocol.Keyring{"client-v1": key}
	fail := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.WriteHeader(503)
			return
		}
		plain, _ := json.Marshal(map[string]any{"code": "ok", "data": map[string]any{"unix_ms": time.Now().Add(2 * time.Second).UnixMilli()}})
		env, _ := keys.Seal("client-v1", plain)
		_ = json.NewEncoder(w).Encode(env)
	}))
	defer server.Close()
	db, err := store.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	client, err := cloud.New(protocol.Config{Version: 1, ServerURL: server.URL, TransportKeyID: "client-v1", TransportKey: base64.StdEncoding.EncodeToString(key), ExportID: "test", IssuedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	s := &Service{ctx: t.Context(), store: db, client: client, session: &cloud.Session{Token: "test", ExpiresAt: time.Now().Add(time.Hour)}, state: State{LoggedIn: true}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	if err = s.initInventory(); err != nil {
		t.Fatal(err)
	}
	if s.inventory.synced {
		t.Fatal("clock restored from previous run")
	}
	if err = s.SyncInventoryClock(); err != nil {
		t.Fatal(err)
	}
	if !s.inventory.synced || s.inventory.offset < 1900 || s.inventory.offset > 2100 {
		t.Fatalf("wrong offset %d", s.inventory.offset)
	}
	before := s.calibratedNowLocked()
	anchor := s.inventory.anchor
	fail = true
	if s.SyncInventoryClock() == nil || !s.inventory.synced || s.inventory.anchor != anchor || s.inventory.syncError == "" || s.calibratedNowLocked().Before(before) {
		t.Fatal("failed sync lost valid monotonic anchor")
	}
	settings := inventory.Settings{QueryMinMS: 400, QueryMaxMS: 900, OrderMinMS: 5000, OrderMaxMS: 8000}
	if err = s.SaveInventorySettings(settings); err != nil {
		t.Fatal(err)
	}
	other := &Service{store: db}
	if err = other.initInventory(); err != nil || other.inventory.settings != settings || other.inventory.synced {
		t.Fatal("restart persistence or clock reset failed")
	}
}

func TestCrossSettingPersistsAndExecutionLocksMutations(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts, err := tasks.New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	task, err := ts.Create()
	if err != nil {
		t.Fatal(err)
	}
	s := &Service{ctx: t.Context(), tasks: ts, session: &cloud.Session{ExpiresAt: time.Now().Add(time.Hour)}, state: State{LoggedIn: true}}
	if err = s.SaveTaskCross(task.ID, true); err != nil {
		t.Fatal(err)
	}
	reloaded, err := tasks.New(root, db)
	if err != nil || !reloaded.Snapshot().Tasks[0].AllowCross {
		t.Fatal("cross flag not persisted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.inventory.runs = map[int64]*inventoryRun{task.ID: {ctx: ctx, cancel: cancel, queryCancel: func() {}, status: "monitoring"}}
	if s.SaveTaskCross(task.ID, false) == nil || s.SaveTaskSeats(task.ID, "tier", nil) == nil || s.SaveTaskPeople(task.ID, nil) == nil || s.RefreshTaskPeople(task.ID, true) == nil || s.DeleteTask(task.ID) == nil {
		t.Fatal("execution configuration editable while running")
	}
	if _, err = s.BeginAuthorization(task.ID); err == nil {
		t.Fatal("reauthorization allowed while running")
	}
}

func TestNotificationAndDeadlineOnlyStageOnce(t *testing.T) {
	root := t.TempDir()
	db, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ts, err := tasks.New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	task, err := ts.Create()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	qctx, qcancel := context.WithCancel(ctx)
	r := &inventoryRun{ctx: ctx, cancel: cancel, queryCancel: qcancel, status: "monitoring", early: true, task: task}
	s := &Service{tasks: ts, inventory: inventoryState{runs: map[int64]*inventoryRun{task.ID: r}}}
	real := inventory.Plan{Source: "notification", MatchID: "101", Parts: []inventory.Allocation{{Quantity: 2}}}
	s.acceptInventoryPlanLocked(task.ID, r, real)
	if r.status != "ready" || qctx.Err() == nil {
		t.Fatal("notification failed to stop polling")
	}
	// A concurrent own response cannot replace a peer's accepted plan.
	s.acceptInventoryPlanLocked(task.ID, r, inventory.Plan{Source: "realtime"})
	s.stageInventoryOrderLocked(task.ID, r)
	before := len(ts.Snapshot().Tasks[0].Logs)
	s.stageInventoryOrderLocked(task.ID, r)
	s.acceptInventoryPlanLocked(task.ID, r, inventory.Plan{Source: "default"})
	if r.status != "pending_order" || r.plan.Source != "notification" || len(ts.Snapshot().Tasks[0].Logs) != before {
		t.Fatal("duplicate deadline/response staged twice")
	}
	s.stopInventoryLocked(task.ID, "stopped", "已停止")
	s.acceptInventoryPlanLocked(task.ID, r, real)
	s.stageInventoryOrderLocked(task.ID, r)
	if r.plan != nil || r.status != "stopped" {
		t.Fatal("stale notification revived stopped run")
	}
	// Late starts stage a live plan immediately without waiting or default fallback.
	ctx2, cancel2 := context.WithCancel(t.Context())
	defer cancel2()
	late := &inventoryRun{ctx: ctx2, cancel: cancel2, queryCancel: func() {}, status: "monitoring", task: task}
	s.inventory.runs[task.ID] = late
	s.acceptInventoryPlanLocked(task.ID, late, real)
	if late.status != "pending_order" || late.plan.Source != "notification" {
		t.Fatal("late live notification did not stage immediately")
	}
}
