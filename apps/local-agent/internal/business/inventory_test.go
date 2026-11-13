package business

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
		io.WriteString(w, `{"code":200,"data":[]}`)
	}))
	defer server.Close()
	c := NewClient()
	defer c.Close()
	c.base = server.URL
	if _, err := c.Stock(t.Context(), config(), Session{"own-token", "own-uid"}, "101"); err == nil || calls != 1 {
		t.Fatalf("stock retries unexpectedly: %d %v", calls, err)
	}
	if _, err := c.Stock(t.Context(), config(), Session{"own-token", "own-uid"}, "101"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := c.Stock(ctx, config(), Session{"own-token", "own-uid"}, "101"); err == nil || calls != 2 {
		t.Fatal("cancelled request sent")
	}
}
