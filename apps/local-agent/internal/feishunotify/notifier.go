// Package feishunotify owns runtime-only notification delivery. It never persists
// events, and never uses the ticket request proxy pools.
package feishunotify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"robot/apps/cloud-server/pkg/feishu"
)

type View struct {
	Enabled       bool      `json:"enabled"`
	Configured    bool      `json:"configured"`
	MaskedWebhook string    `json:"maskedWebhook"`
	OrderCreated  bool      `json:"orderCreated"`
	AuthExpired   bool      `json:"authExpired"`
	OrderBlocked  bool      `json:"orderBlocked"`
	SyncedAt      time.Time `json:"syncedAt"`
	SyncError     string    `json:"syncError"`
	LastResult    string    `json:"lastResult"`
	LastError     string    `json:"lastError"`
	LastAt        time.Time `json:"lastAt"`
	Pending       int       `json:"pending"`
	RetryAt       time.Time `json:"retryAt"`
}

type Resolved struct {
	Text     string
	Deadline time.Time
	Ready    bool
	Stop     string
}
type Event struct {
	ID, Kind, Text string
	Deadline       time.Time
	Resolve        func(context.Context) (Resolved, error)
}
type Manager struct {
	mu          sync.Mutex
	ctx         context.Context
	cancel      context.CancelFunc
	config      feishu.Config
	view        View
	seen        map[string]bool
	active      map[string]time.Time
	wg          sync.WaitGroup
	http        *http.Client
	sendMu      sync.Mutex
	nextSend    time.Time
	retryDelays []time.Duration
	detailDelay time.Duration
}

func New(parent context.Context) *Manager {
	ctx, cancel := context.WithCancel(parent)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Manager{ctx: ctx, cancel: cancel, config: feishu.Default(), seen: map[string]bool{}, active: map[string]time.Time{}, http: &http.Client{Timeout: 10 * time.Second, Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, retryDelays: []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second}, detailDelay: 15 * time.Second}
}
func (m *Manager) Close()  { m.cancel(); m.wg.Wait(); m.http.CloseIdleConnections() }
func (m *Manager) Cancel() { m.cancel() }
func (m *Manager) Configure(c feishu.Config) error {
	if err := feishu.ValidateWebhook(c.Webhook); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config = c
	m.view.SyncedAt = time.Now()
	m.view.SyncError = ""
	return nil
}
func (m *Manager) SyncFailed() {
	m.mu.Lock()
	m.view.SyncError = "飞书配置同步失败，将保留当前配置并在下次同步时重试"
	m.mu.Unlock()
}
func (m *Manager) View() View {
	m.mu.Lock()
	defer m.mu.Unlock()
	v := m.view
	v.Enabled = m.config.Enabled
	v.Configured = m.config.Webhook != ""
	v.MaskedWebhook = m.config.MaskedWebhook()
	v.OrderCreated = m.config.OrderCreated
	v.AuthExpired = m.config.AuthExpired
	v.OrderBlocked = m.config.OrderBlocked
	v.Pending = len(m.active)
	for _, at := range m.active {
		if !at.IsZero() && (v.RetryAt.IsZero() || at.Before(v.RetryAt)) {
			v.RetryAt = at
		}
	}
	return v
}
func (m *Manager) Enqueue(e Event) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.ctx.Err() != nil || m.seen[e.ID] {
		return false
	}
	m.seen[e.ID] = true
	if !m.config.Allows(e.Kind) {
		return false
	}
	m.active[e.ID] = time.Time{}
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() { m.mu.Lock(); delete(m.active, e.ID); m.mu.Unlock() }()
		m.deliver(e)
	}()
	return true
}
func (m *Manager) result(message string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.view.LastAt = time.Now()
	m.view.LastResult = message
	m.view.LastError = ""
	if err != nil {
		m.view.LastError = err.Error()
	}
}
func wait(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
func (m *Manager) allowed(e Event) (feishu.Config, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config, m.ctx.Err() == nil && m.config.Allows(e.Kind)
}
func (m *Manager) deliver(e Event) {
	// Resolve on each retry so a newly learned deadline or changed order state
	// can stop delivery before an obsolete payment reminder is sent.
	attempts := 0
	for {
		c, ok := m.allowed(e)
		if !ok {
			m.result("通知已停发", nil)
			return
		}
		if !e.Deadline.IsZero() && !time.Now().Before(e.Deadline) {
			m.result("订单已过期，通知已停止", nil)
			return
		}
		if e.Resolve != nil {
			resolved, err := e.Resolve(m.ctx)
			if m.ctx.Err() != nil {
				return
			}
			if resolved.Stop != "" {
				m.result(resolved.Stop, nil)
				return
			}
			if !resolved.Deadline.IsZero() {
				e.Deadline = resolved.Deadline
			}
			if !e.Deadline.IsZero() && !time.Now().Before(e.Deadline) {
				m.result("订单已过期，通知已停止", nil)
				return
			}
			if err != nil || !resolved.Ready {
				m.result("等待订单详情", err)
				m.mu.Lock()
				m.active[e.ID] = time.Now().Add(m.detailDelay)
				m.mu.Unlock()
				if !wait(m.ctx, m.detailDelay) {
					return
				}
				continue
			}
			e.Text = resolved.Text
		}
		// Shared pacing prevents one client bursting many notifications at once.
		m.sendMu.Lock()
		if !wait(m.ctx, time.Until(m.nextSend)) {
			m.sendMu.Unlock()
			return
		}
		c, ok = m.allowed(e)
		if !ok {
			m.sendMu.Unlock()
			m.result("通知已停发", nil)
			return
		}
		if !e.Deadline.IsZero() && !time.Now().Before(e.Deadline) {
			m.sendMu.Unlock()
			m.result("订单已过期，通知已停止", nil)
			return
		}
		m.mu.Lock()
		m.active[e.ID] = time.Time{}
		m.mu.Unlock()
		err := m.send(c.Webhook, e.Text)
		m.nextSend = time.Now().Add(time.Second)
		m.sendMu.Unlock()
		if m.ctx.Err() != nil {
			return
		}
		if err == nil {
			m.result("发送成功", nil)
			return
		}
		if attempts >= len(m.retryDelays) {
			m.result("发送失败，重试已结束", err)
			return
		}
		delay := m.retryDelays[attempts]
		attempts++
		m.result(fmt.Sprintf("发送失败，等待第 %d 次重试", attempts), err)
		m.mu.Lock()
		m.active[e.ID] = time.Now().Add(delay)
		m.mu.Unlock()
		if !wait(m.ctx, delay) {
			return
		}
	}
}
func (m *Manager) send(webhook, text string) error {
	body, _ := json.Marshal(map[string]any{"msg_type": "text", "content": map[string]string{"text": text}})
	req, err := http.NewRequestWithContext(m.ctx, "POST", webhook, bytes.NewReader(body))
	if err != nil {
		return errors.New("机器人地址无效")
	}
	req.Header.Set("Content-Type", "application/json")
	response, err := m.http.Do(req)
	// Do not expose URL-bearing transport errors or arbitrary remote response text.
	if err != nil {
		return errors.New("无法连接飞书或请求超时")
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("飞书返回 HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024+1))
	if err != nil || len(raw) > 64*1024 {
		return errors.New("飞书响应读取失败")
	}
	var out struct {
		Code       *int `json:"code"`
		StatusCode *int `json:"StatusCode"`
	}
	if json.Unmarshal(raw, &out) != nil {
		return errors.New("飞书响应格式无效")
	}
	code := out.Code
	if code == nil {
		code = out.StatusCode
	}
	if code == nil {
		return errors.New("飞书响应缺少结果码")
	}
	if *code != 0 {
		return fmt.Errorf("飞书拒绝消息（业务码 %d，请检查机器人关键词或安全设置）", *code)
	}
	return nil
}
