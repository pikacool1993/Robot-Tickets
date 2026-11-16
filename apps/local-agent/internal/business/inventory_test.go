package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestStockReplaysCapturedSuccess(t *testing.T) {
	raw, err := os.ReadFile("../inventory/testdata/captured-stock.json")
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/fc/wx_api/v1/MatchCon/getBillRegion" || r.URL.Query().Get("lid2") != "replay-uid" || r.Header.Get("Authorization") != "Bearer replay-token" {
			t.Error("incorrect inventory request")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["id"] != "58" {
			t.Error("incorrect match id")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(raw)
	}))
	defer server.Close()
	c := NewClient()
	defer c.Close()
	c.base = server.URL
	got, err := c.Stock(t.Context(), config(), Session{Token: "replay-token", UserID: "replay-uid"}, "58")
	if err != nil || !bytes.Equal(bytes.TrimSpace(got), bytes.TrimSpace(raw)) {
		t.Fatalf("captured success was rejected or changed: %v", err)
	}
}

func TestStockUsesOwnSessionAndOneAttempt(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/fc/wx_api/v1/MatchCon/getBillRegion" || r.URL.Query().Get("lid2") != "own-uid" || r.Header.Get("Authorization") != "Bearer own-token" {
			t.Error("wrong session or endpoint")
		}
		var body map[string]string
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["id"] != "101" {
			t.Error("wrong match")
		}
		if calls == 1 {
			w.WriteHeader(429)
			return
		}
		io.WriteString(w, `{"code":1,"data":[]}`)
	}))
	defer server.Close()
	c := NewClient()
	defer c.Close()
	c.base = server.URL
	if _, err := c.Stock(t.Context(), config(), Session{Token: "own-token", UserID: "own-uid"}, "101"); err == nil || calls != 1 {
		t.Fatalf("stock retries unexpectedly: %d %v", calls, err)
	}
	if _, err := c.Stock(t.Context(), config(), Session{Token: "own-token", UserID: "own-uid"}, "101"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Stock(ctx, config(), Session{Token: "own-token", UserID: "own-uid"}, "101"); err == nil || calls != 2 {
		t.Fatal("cancelled request sent")
	}
}

func TestStockRejectsBusinessCode200(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		io.WriteString(w, `{"code":200,"data":[]}`)
	}))
	defer server.Close()
	c := NewClient()
	defer c.Close()
	c.base = server.URL
	_, err := c.Stock(t.Context(), config(), Session{Token: "test-token", UserID: "test-uid"}, "58")
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.HTTP != 200 || apiErr.Code != 200 || calls != 1 {
		t.Fatalf("expected single-attempt business failure: calls=%d err=%v", calls, err)
	}
}

func TestStockErrorsAndResponseURLsNeverTriggerExtraRequests(t *testing.T) {
	redirectCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { redirectCalls++ }))
	defer target.Close()
	calls, status, code := 0, 200, 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"code":%d,"data":[],"url":%q,"reurl":%q}`, code, target.URL, target.URL)
	}))
	defer server.Close()
	c := NewClient()
	defer c.Close()
	c.base = server.URL
	check := func(wantError bool) {
		t.Helper()
		before := calls
		_, err := c.Stock(t.Context(), config(), Session{Token: "test-token", UserID: "test-uid"}, "58")
		if (err != nil) != wantError || calls != before+1 || redirectCalls != 0 {
			t.Fatalf("unexpected retry, redirect or result: calls=%d redirected=%d err=%v", calls-before, redirectCalls, err)
		}
	}
	check(false)
	check(false) // reurl must not affect the next request either.
	for _, code = range []int{-1, 996, 997, 998, 999, 200, 1234} {
		check(true)
	}
	for _, status = range []int{401, 429, 500, 503} {
		check(true)
	}
}
