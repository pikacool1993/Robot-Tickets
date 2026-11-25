package ippool

import (
	"context"
	"errors"
	"io"
	"net/http"
	"path/filepath"
	"robot/apps/local-agent/internal/store"
	"testing"
	"time"
)

func manager(t *testing.T) *Manager {
	t.Helper()
	db, e := store.Open(filepath.Join(t.TempDir(), "data"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	m, e := New(db)
	if e != nil {
		t.Fatal(e)
	}
	return m
}
func importPool(t *testing.T, m *Manager, k string) {
	t.Helper()
	notes, e := m.Import(k, "127.0.0.1|2018|user|secret|2099-12-31\n127.0.0.2|2018|user|secret|2099-12-31")
	if e != nil || len(notes) != 0 {
		t.Fatal(notes, e)
	}
}
func TestImportPersistenceAndRedaction(t *testing.T) {
	m := manager(t)
	importPool(t, m, "N1")
	notes, e := m.Import("N1", "127.0.0.1|2018|x|new|2099-12-31\nbad\n127.0.0.3|2018|u|p|2099-12-31\n127.0.0.3|2018|u|p|2099-12-31")
	if e != nil || len(notes) != 3 {
		t.Fatal(notes, e)
	}
	if len(m.pools["N1"].Entries) != 3 || m.pools["N1"].Entries[0].Password != "secret" {
		t.Fatal("duplicate changed credentials")
	}
	v := m.Snapshot(nil)
	if v.Pools["N1"].Entries[0].Password != "" {
		t.Fatal("credential leak")
	}
	restored, e := New(m.db)
	if e != nil || restored.pools["N1"].Entries[0].Password != "secret" || len(restored.bindings) != 0 {
		t.Fatal("persistence", e)
	}
}
func TestAllocationControlsAndCooldown(t *testing.T) {
	m := manager(t)
	importPool(t, m, "N1")
	a := m.choose(1, "N1", false)
	b := m.choose(2, "N1", false)
	if a.Exit == b.Exit {
		t.Fatal("unbalanced")
	}
	for i := 0; i < 2; i++ {
		m.record(1, "N1", a, 0, errors.New("connection reset"))
	}
	if m.choose(1, "N1", false).Exit != a.Exit {
		t.Fatal("premature switch")
	}
	m.record(1, "N1", a, 200, nil)
	if m.bindings[1]["N1"].Failures != 0 {
		t.Fatal("not reset")
	}
	for i := 0; i < 3; i++ {
		m.record(1, "N1", a, 0, errors.New("failed"))
	}
	if m.choose(1, "N1", false).Exit == a.Exit {
		t.Fatal("no cooldown switch")
	}
	m.cool["N1"+a.Exit] = time.Now().Add(-time.Second)
	if !contains(m.candidates("N1"), a.Exit) {
		t.Fatal("cooldown not recovered")
	}
	if e := m.Settings("N1", true, false, 3, 60); e != nil {
		t.Fatal(e)
	}
	if m.choose(1, "N1", false).Exit != "本机直连" {
		t.Fatal("force direct")
	}
	m.Settings("N1", false, true, 3, 60)
	if len(m.candidates("N1")) != 3 {
		t.Fatal("direct candidate missing")
	}
}
func TestN5TimeoutAndInFlightGeneration(t *testing.T) {
	m := manager(t)
	importPool(t, m, "N5")
	b := m.choose(1, "N5", false)
	m.record(1, "N5", b, 0, errors.New("ordinary"))
	if m.bindings[1]["N5"].Failures != 0 {
		t.Fatal("non timeout counted")
	}
	for i := 0; i < 3; i++ {
		m.record(1, "N5", b, 0, context.DeadlineExceeded)
	}
	next := m.choose(1, "N5", false)
	if next.Exit == b.Exit {
		t.Fatal("timeout not switched")
	}
	m.record(1, "N5", b, 200, nil)
	if m.bindings[1]["N5"].Generation != next.Generation {
		t.Fatal("stale request changed binding")
	}
	before := next.Exit
	m.Shuffle([]int64{1})
	if m.bindings[1]["N5"].Exit != before {
		t.Fatal("cooling proxy selected")
	}
}
func TestExpiryAndDisabledFallback(t *testing.T) {
	m := manager(t)
	m.Import("N1", "127.0.0.1|80|u|p|2020-01-01")
	if m.choose(1, "N1", false).Exit != "本机直连" {
		t.Fatal("expired proxy selected")
	}
	e, _ := parse("127.0.0.1|80|u|p|2026-11-25")
	want := time.Date(2026, 11, 26, 0, 0, 0, 0, time.FixedZone("CST", 8*3600))
	if !expiry(e).Equal(want) {
		t.Fatal("wrong date boundary")
	}
}

type roundFunc func(*http.Request) (*http.Response, error)

func (f roundFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type failingBody struct{}

func (failingBody) Read([]byte) (int, error) { return 0, context.DeadlineExceeded }
func (failingBody) Close() error             { return nil }
func TestBodyTimeoutCountedOnce(t *testing.T) {
	m := manager(t)
	req, _ := http.NewRequestWithContext(WithTask(context.Background(), 7), "POST", "https://fccdn5.k4n.cc/test", nil)
	res, e := m.RoundTrip(req, roundFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: failingBody{}}, nil
	}))
	if e != nil {
		t.Fatal(e)
	}
	io.ReadAll(res.Body)
	res.Body.Close()
	if m.bindings[7]["N5"].Failures != 1 {
		t.Fatal("body timeout missing or double counted")
	}
}

func TestRandomSwitchAndGlobalCooling(t *testing.T) {
	m := manager(t)
	importPool(t, m, "N1")
	a := m.choose(1, "N1", false)
	m.choose(2, "N1", false)
	m.choose(3, "N1", false)
	if m.choose(3, "N1", false).Exit != a.Exit {
		t.Fatal("fixture")
	}
	for i := 0; i < 3; i++ {
		m.record(1, "N1", a, 0, errors.New("network"))
	}
	if m.choose(3, "N1", false).Exit == a.Exit {
		t.Fatal("global cooldown ignored")
	}
	m.cool["N1"+a.Exit] = time.Now().Add(-time.Second)
	before := m.choose(2, "N1", false).Exit
	m.Shuffle([]int64{2})
	if m.bindings[2]["N1"].Exit == before {
		t.Fatal("random did not exclude current")
	}
	m.Release(2)
	m.Snapshot([]int64{2})
	if len(m.bindings[2]) != 0 {
		t.Fatal("snapshot reallocated stopped task")
	}
}
