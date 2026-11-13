package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/cloud"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/store"
	"robot/apps/local-agent/internal/tasks"
	"testing"
	"time"
)

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
