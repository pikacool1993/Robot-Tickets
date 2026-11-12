package agent

import (
	"context"
	"io"
	"log/slog"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/cloud"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/store"
	"robot/apps/local-agent/internal/tasks"
	"testing"
	"time"
)

func TestStaleBusinessResultCannotOverwriteTask(t *testing.T) {
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
	original := tasks.BusinessData{Account: "wechat:new", Status: "ready", Message: "new binding result", Usable: true, DeviceID: "17944492587160", DeviceType: "ios"}
	if err = ts.SaveBusiness(task.ID, original); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stale := &businessRun{ctx: ctx, cancel: cancel, busy: true, binding: protocols.Binding{Generation: "old"}}
	current := &businessRun{ctx: ctx, cancel: cancel, binding: protocols.Binding{Generation: "new"}}
	c := business.NewClient()
	defer c.Close()
	s := &Service{tasks: ts, businessClient: c, businessRuns: map[int64]*businessRun{task.ID: current}, session: &cloud.Session{ExpiresAt: time.Now().Add(time.Hour)}, state: State{LoggedIn: true}, log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	// Invalid configuration completes immediately with an error; the old job must not write that error into a new binding.
	s.runBusiness(task.ID, stale, business.Config{}, original.DeviceID)
	after, err := ts.Business(task.ID)
	if err != nil || after.Status != original.Status || after.Message != original.Message || after.Account != original.Account {
		t.Fatalf("old job overwrote new binding: %+v %v", after, err)
	}
	if sameBinding(stale.binding, current.binding) {
		t.Fatal("same-account reauthorization generations were conflated")
	}
	s.cancelBusinessLocked()
	if ctx.Err() == nil || len(s.businessRuns) != 0 {
		t.Fatal("session cleanup did not cancel jobs")
	}
}
