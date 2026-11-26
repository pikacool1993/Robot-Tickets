package orderalarm

import (
	"errors"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	raw  []byte
	fail bool
}

func (s *memoryStore) Get(string) ([]byte, error) { return s.raw, nil }
func (s *memoryStore) Put(_ string, raw []byte) error {
	if s.fail {
		return errors.New("disk full")
	}
	s.raw = append([]byte{}, raw...)
	return nil
}

type fakePlayer struct {
	mu           sync.Mutex
	loops, stops int
	fail         bool
}

func (p *fakePlayer) Loop() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loops++
	if p.fail {
		return errors.New("no audio")
	}
	return nil
}
func (p *fakePlayer) Stop() error  { p.mu.Lock(); defer p.mu.Unlock(); p.stops++; return nil }
func (p *fakePlayer) Close() error { return nil }
func eventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}
func TestDisabledDefaultPersistenceAndNoReplay(t *testing.T) {
	store := &memoryStore{}
	m := New(store)
	if m.View().Enabled || m.View().Active {
		t.Fatal("must default off")
	}
	m.Trigger("old", false)
	if m.View().Count != 0 {
		t.Fatal("disabled order rang")
	}
	if err := m.Enable(true); err != nil {
		t.Fatal(err)
	}
	m.Trigger("old", false)
	if m.View().Active {
		t.Fatal("enabling replayed order")
	}
	m.Close()
	next := New(store)
	defer next.Close()
	if !next.View().Enabled || next.View().Active || next.View().Count != 0 {
		t.Fatal("settings or runtime recovery wrong")
	}
}
func TestMultipleOrdersDoNotOverlapAndStopRearms(t *testing.T) {
	p := &fakePlayer{}
	m := New(&memoryStore{})
	defer m.Close()
	m.SetPlayer(p)
	_ = m.Enable(true)
	m.Trigger("one", false)
	eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.loops == 1 })
	m.Trigger("one", false)
	m.Trigger("simulation", true)
	m.Trigger("two", false)
	if m.View().Count != 2 {
		t.Fatal("duplicate/simulation counted")
	}
	m.Stop()
	eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.stops == 1 })
	if m.View().Active || m.View().Count != 0 {
		t.Fatal("stop did not clear batch")
	}
	m.Trigger("two", false)
	if m.View().Active {
		t.Fatal("stopped order replayed")
	}
	m.Trigger("three", false)
	eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.loops == 2 })
	if m.View().Count != 1 {
		t.Fatal("new batch count wrong")
	}
	_ = m.Enable(false)
	eventually(t, func() bool { p.mu.Lock(); defer p.mu.Unlock(); return p.stops == 2 })
}
func TestPreviewDoesNotEnableAndPlayerFailureReported(t *testing.T) {
	p := &fakePlayer{fail: true}
	store := &memoryStore{}
	m := New(store)
	defer m.Close()
	m.SetPlayer(p)
	m.Preview()
	eventually(t, func() bool { return m.View().Error != "" })
	if m.View().Enabled || m.View().Active || len(store.raw) > 0 {
		t.Fatal("preview changed preference or hid failure")
	}
	m.Stop()
	if m.View().Error != "" {
		t.Fatal("cannot dismiss failure")
	}
	store.fail = true
	if m.Enable(true) == nil || m.View().Enabled {
		t.Fatal("failed persistence changed setting")
	}
}
