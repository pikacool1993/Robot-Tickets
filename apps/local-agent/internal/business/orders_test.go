package business

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestOrderResponseAndDelay(t *testing.T) {
	for _, tt := range []struct {
		raw   string
		code  int
		id    string
		delay time.Duration
		bad   bool
	}{
		{`{"code":1,"data":34726}`, 1, "34726", 0, false},
		{`{"code":1,"data":"34726"}`, 1, "34726", 0, false},
		{`{"code":5,"timeout":5}`, 5, "", 5 * time.Second, false},
		{`{"code":3,"timeout":2}`, 3, "", 2 * time.Second, false},
		{`{"code":0,"timeout":2,"msg":"频繁操作"}`, 0, "", 2 * time.Second, false},
		{`{"code":0}`, 0, "", time.Second, false},
		{`{"code":0,"timeout":0}`, 0, "", time.Second, false},
		{`{"code":0,"timeout":-1}`, 0, "", time.Second, false},
		{`{"code":0,"timeout":"invalid"}`, 0, "", time.Second, false},
		{`{"code":3,"timeout":"2.5"}`, 3, "", 2500 * time.Millisecond, false},
		{`{"code":3}`, 3, "", 5600 * time.Millisecond, false},
		{`{"code":3,"timeout":0}`, 3, "", 5600 * time.Millisecond, false},
		{`{"code":3,"timeout":-1}`, 3, "", 5600 * time.Millisecond, false},
		{`{"code":3,"timeout":"invalid"}`, 3, "", 5600 * time.Millisecond, false},
		{`{"code":3,"timeout":1e100}`, 3, "", 5600 * time.Millisecond, false},
		{`{"code":5,"timeout":5,"msg":"您有未支付的订单，请先支付"}`, 5, "", 5 * time.Second, false},
		{`{"code":5,"timeout":0}`, 5, "", 3 * time.Second, false},
		{`{"code":5}`, 5, "", 3 * time.Second, false},
		{`{"code":5,"timeout":"2.5"}`, 5, "", 2500 * time.Millisecond, false},
		{`{"code":5,"timeout":"invalid"}`, 5, "", 3 * time.Second, false},
		{`{"code":5,"timeout":-1}`, 5, "", 3 * time.Second, false},
		{`{"code":5,"timeout":1e100}`, 5, "", 3 * time.Second, false},
		{`{"code":1,"data":[]}`, 1, "", 0, true},
		{`{"code":1,"data":0}`, 1, "", 0, true},
		{`{"code":1,"data":"abc"}`, 1, "", 0, true},
		{`{"code":1,"data":1.5}`, 1, "", 0, true},
		{`{"data":34726}`, 0, "", 0, true},
		{`{"code":999,"url":"https://example.invalid"}`, 999, "", time.Second, false},
		{`{"code":2,"timeout":4}`, 2, "", 4 * time.Second, false},
		{`{"code":2}`, 2, "", time.Second, false},
		{`{"code":-1,"timeout":2}`, -1, "", 2 * time.Second, false},
		{`{"code":996,"timeout":0}`, 996, "", time.Second, false},
		{`{"code":997,"timeout":-2}`, 997, "", time.Second, false},
		{`{"code":998,"timeout":"bad"}`, 998, "", time.Second, false},
		{`{"code":12345,"timeout":"2.5"}`, 12345, "", 2500 * time.Millisecond, false},
	} {
		t.Run(tt.raw, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.Path != "/fc/wx_api/v1/MatchCon/createMatchOrder" || r.URL.Query().Get("lid2") != "99" || r.URL.Query().Get("s_time") != "100" || r.URL.Query().Get("s_sign") != "signature" || r.Header.Get("Authorization") != "Bearer own-token" || r.Header.Get("User-Agent") != config().Template.UserAgent || r.Header.Get("Referer") != config().Template.Referer {
					t.Error("incorrect order endpoint/session/headers")
				}
				var b map[string]any
				if json.NewDecoder(r.Body).Decode(&b) != nil || b["encryptedData"] != "test-cipher" {
					t.Error("incorrect order body")
				}
				io.WriteString(w, tt.raw)
			}))
			defer srv.Close()
			c := NewClient()
			defer c.Close()
			c.orderBase = srv.URL
			out, err := c.CreateOrder(t.Context(), config(), Session{Token: "own-token", UserID: "99"}, "/fc/wx_api/v1/MatchCon/createMatchOrder?lid2=99&s_time=100&s_sign=signature", map[string]string{"encryptedData": "test-cipher"})
			var response struct {
				Message string `json:"msg"`
			}
			_ = json.Unmarshal([]byte(tt.raw), &response)
			if out.Message != response.Message {
				t.Fatal("server message was not preserved")
			}
			if (err != nil) != tt.bad || out.Code != tt.code || out.OrderID != tt.id || out.Delay != tt.delay || calls != 1 {
				t.Fatalf("unexpected result %+v %v calls=%d", out, err, calls)
			}
		})
	}
}

func TestOrderNeverRetriesOrRedirects(t *testing.T) {
	var targetCalls atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { targetCalls.Add(1) }))
	defer target.Close()
	for _, status := range []int{401, 429, 500, 503, 302, 307} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Location", target.URL)
				w.WriteHeader(status)
			}))
			defer srv.Close()
			c := NewClient()
			defer c.Close()
			c.orderBase = srv.URL
			_, err := c.CreateOrder(t.Context(), config(), Session{Token: "t", UserID: "99"}, "/fc/wx_api/v1/MatchCon/createMatchOrder?lid2=99", struct{}{})
			if err == nil || calls != 1 || targetCalls.Load() != 0 {
				t.Fatalf("request replayed/redirected: calls=%d err=%v", calls, err)
			}
		})
	}
}

type orderTransportFunc func(*http.Request) (*http.Response, error)

func (f orderTransportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
func TestOrderTimeoutBoundAndSingleTransportAttempt(t *testing.T) {
	c := NewClient()
	defer c.Close()
	calls := 0
	c.http.Transport = orderTransportFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		d, ok := r.Context().Deadline()
		if !ok || time.Until(d) > 15*time.Second {
			t.Error("missing 15 second deadline")
		}
		return nil, io.ErrUnexpectedEOF
	})
	_, err := c.CreateOrder(t.Context(), config(), Session{Token: "t", UserID: "99"}, "/fc/wx_api/v1/MatchCon/createMatchOrder?lid2=99", struct{}{})
	if err == nil || calls != 1 {
		t.Fatal("transport failure retried")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	c.http.Transport = orderTransportFunc(func(r *http.Request) (*http.Response, error) { return nil, r.Context().Err() })
	_, err = c.CreateOrder(ctx, config(), Session{Token: "t", UserID: "99"}, "/fc/wx_api/v1/MatchCon/createMatchOrder?lid2=99", struct{}{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel not propagated: %v", err)
	}
}
