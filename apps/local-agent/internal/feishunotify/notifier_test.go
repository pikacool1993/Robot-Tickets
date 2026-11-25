package feishunotify

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"robot/apps/cloud-server/pkg/feishu"
)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

const testHook = "https://open.feishu.cn/open-apis/bot/v2/hook/test-hook-1234"

func setup(t *testing.T, fn roundTrip) *Manager {
	t.Helper()
	m := New(context.Background())
	m.retryDelays = []time.Duration{time.Millisecond, time.Millisecond, time.Millisecond}
	m.detailDelay = time.Millisecond
	m.http.Transport = fn
	c := feishu.Default()
	c.Webhook = testHook
	if err := m.Configure(c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	return m
}
func response(raw string) *http.Response {
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(raw))}
}
func done(t *testing.T, m *Manager) {
	t.Helper()
	until := time.Now().Add(8 * time.Second)
	for m.View().Pending > 0 {
		if time.Now().After(until) {
			t.Fatal("delivery did not finish")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBusinessFailureRetriesExactlyThreeTimesAndDeduplicates(t *testing.T) {
	var count atomic.Int32
	m := setup(t, func(r *http.Request) (*http.Response, error) {
		count.Add(1)
		return response(`{"code":19024,"msg":"test rejection"}`), nil
	})
	e := Event{ID: "order:1", Kind: "order_created", Text: "message"}
	if !m.Enqueue(e) || m.Enqueue(e) {
		t.Fatal("event dedup failed")
	}
	done(t, m)
	if count.Load() != 4 || !strings.Contains(m.View().LastResult, "重试已结束") {
		t.Fatalf("wrong attempts/result: %d %+v", count.Load(), m.View())
	}
	if m.Enqueue(e) {
		t.Fatal("completed event replayed")
	}
}
func TestWaitsForDetailsAndSendsCorrectText(t *testing.T) {
	var resolves atomic.Int32
	var got string
	m := setup(t, func(r *http.Request) (*http.Response, error) {
		raw, _ := io.ReadAll(r.Body)
		got = string(raw)
		return response(`{"code":0}`), nil
	})
	m.Enqueue(Event{ID: "wait", Kind: "order_created", Resolve: func(context.Context) (Resolved, error) {
		if resolves.Add(1) < 3 {
			return Resolved{}, errors.New("pending")
		}
		return Resolved{Text: "张三\n南区 ￥180", Ready: true, Deadline: time.Now().Add(time.Hour)}, nil
	}})
	done(t, m)
	if resolves.Load() != 3 || !strings.Contains(got, `张三\n南区 ￥180`) || m.View().LastResult != "发送成功" {
		t.Fatalf("message not resolved/sent: %q %+v", got, m.View())
	}
}
func TestExpiryAndDisabledConfigStopPendingNotifications(t *testing.T) {
	for _, mode := range []string{"expired", "disabled", "cancelled", "state-ended"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			m := setup(t, func(r *http.Request) (*http.Response, error) { calls.Add(1); return response(`{"code":0}`), nil })
			e := Event{ID: mode, Kind: "order_created", Text: "should not send"}
			switch mode {
			case "expired":
				e.Deadline = time.Now().Add(-time.Second)
			case "disabled":
				c := feishu.Default()
				c.Webhook = testHook
				c.OrderCreated = false
				_ = m.Configure(c)
			case "cancelled":
				m.Cancel()
			case "state-ended":
				e.Resolve = func(context.Context) (Resolved, error) { return Resolved{Stop: "订单已支付"}, nil }
			}
			m.Enqueue(e)
			done(t, m)
			if calls.Load() != 0 {
				t.Fatal("stopped event sent")
			}
		})
	}
}
func TestDoesNotExposeWebhookInErrorsOrStatus(t *testing.T) {
	m := setup(t, func(r *http.Request) (*http.Response, error) { return nil, errors.New("POST " + testHook + ": failed") })
	m.retryDelays = nil
	m.Enqueue(Event{ID: "secret", Kind: "test", Text: "test"})
	done(t, m)
	if strings.Contains(m.View().LastError, "test-hook") || strings.Contains(m.View().MaskedWebhook, "test-hook") {
		t.Fatal("secret leaked")
	}
	if m.View().LastError == "" {
		t.Fatal("missing failure feedback")
	}
}
func TestFreshManagerDoesNotReplayEvents(t *testing.T) {
	m := setup(t, func(r *http.Request) (*http.Response, error) { return response(`{"code":0}`), nil })
	m.Enqueue(Event{ID: "old", Kind: "test", Text: "test"})
	done(t, m)
	m.Close()
	var calls atomic.Int32
	next := setup(t, func(r *http.Request) (*http.Response, error) { calls.Add(1); return response(`{"code":0}`), nil })
	if next.View().Pending != 0 || calls.Load() != 0 {
		t.Fatal("old event recovered")
	}
}

func TestDisableOrExpireBetweenAttemptsPreventsRetry(t *testing.T) {
	for _, mode := range []string{"disabled", "expired"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			var m *Manager
			m = setup(t, func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				if mode == "disabled" {
					c := feishu.Default()
					c.Webhook = testHook
					c.Enabled = false
					_ = m.Configure(c)
				}
				return response(`{"code":1}`), nil
			})
			m.retryDelays = []time.Duration{10 * time.Millisecond}
			event := Event{ID: "retry", Kind: "order_created", Text: "test"}
			if mode == "expired" {
				event.Deadline = time.Now().Add(5 * time.Millisecond)
			}
			m.Enqueue(event)
			done(t, m)
			if calls.Load() != 1 {
				t.Fatalf("retried obsolete event: %d", calls.Load())
			}
		})
	}
}
