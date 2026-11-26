package orderalarm

import (
	"encoding/json"
	"errors"
	"runtime"
	"sync"
)

type Player interface {
	Loop() error
	Stop() error
	Close() error
}
type Store interface {
	Get(string) ([]byte, error)
	Put(string, []byte) error
}
type View struct {
	Enabled bool   `json:"enabled"`
	Active  bool   `json:"active"`
	Preview bool   `json:"preview"`
	Count   int    `json:"count"`
	Error   string `json:"error"`
}
type Manager struct {
	mu       sync.Mutex
	store    Store
	player   Player
	view     View
	seen     map[string]bool
	revision uint64
	wake     chan struct{}
	done     chan struct{}
	closed   bool
}

func New(store Store) *Manager {
	m := &Manager{store: store, seen: map[string]bool{}, wake: make(chan struct{}, 1), done: make(chan struct{})}
	if raw, err := store.Get("order-alarm.enabled.v1"); err == nil {
		_ = json.Unmarshal(raw, &m.view.Enabled)
	}
	go m.run()
	return m
}
func (m *Manager) signal() {
	m.revision++
	select {
	case m.wake <- struct{}{}:
	default:
	}
}
func (m *Manager) SetPlayer(p Player) { m.mu.Lock(); defer m.mu.Unlock(); m.player = p; m.signal() }
func (m *Manager) View() View         { m.mu.Lock(); defer m.mu.Unlock(); return m.view }
func (m *Manager) Enable(enabled bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, _ := json.Marshal(enabled)
	if err := m.store.Put("order-alarm.enabled.v1", raw); err != nil {
		return errors.New("铃声设置保存失败")
	}
	m.view.Enabled = enabled
	if !enabled {
		m.view.Active = false
		m.view.Count = 0
		m.view.Preview = false
	}
	m.view.Error = ""
	m.signal()
	return nil
}
func (m *Manager) Trigger(id string, simulated bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || simulated || m.seen[id] {
		return
	}
	m.seen[id] = true
	if !m.view.Enabled {
		return
	}
	m.view.Count++
	m.view.Preview = false
	m.view.Active = true
	m.view.Error = ""
	m.signal()
}
func (m *Manager) Preview() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return
	}
	if !m.view.Active {
		m.view.Active = true
		m.view.Preview = true
	}
	m.view.Error = ""
	m.signal()
}
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.view.Active = false
	m.view.Preview = false
	m.view.Count = 0
	m.view.Error = ""
	m.signal()
}
func (m *Manager) Close() { m.mu.Lock(); m.closed = true; m.signal(); m.mu.Unlock(); <-m.done }
func (m *Manager) run() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer close(m.done)
	playing := false
	for range m.wake {
		m.mu.Lock()
		p, v, rev, closed := m.player, m.view, m.revision, m.closed
		m.mu.Unlock()
		if closed {
			if p != nil {
				_ = p.Close()
			}
			return
		}
		var err error
		if v.Active && !playing {
			if p == nil {
				err = errors.New("铃声播放器尚未就绪")
			} else {
				err = p.Loop()
				playing = err == nil
			}
		}
		if !v.Active && playing {
			err = p.Stop()
			playing = false
		}
		if err != nil {
			m.mu.Lock()
			if m.revision == rev {
				m.view.Error = "铃声播放失败，请检查音频设备后试听重试"
				m.view.Active = false
			}
			m.mu.Unlock()
		}
	}
}
