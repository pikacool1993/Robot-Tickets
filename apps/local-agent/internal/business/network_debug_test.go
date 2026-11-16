package business

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"robot/apps/local-agent/internal/netdebug"
	"strings"
	"testing"
)

func TestInventoryDebugLogsEveryResponseIncludingRepeatedErrors(t *testing.T) {
	var logs bytes.Buffer
	netdebug.Configure(slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})), true)
	t.Cleanup(func() { netdebug.Configure(nil, false) })
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls <= 2 {
			io.WriteString(w, `{"code":1,"data":[]}`)
			return
		}
		w.WriteHeader(429)
		io.WriteString(w, `{"code":996,"msg":"limit"}`)
	}))
	defer srv.Close()
	c := NewClient()
	defer c.Close()
	c.base = srv.URL
	var entries []string
	ctx := netdebug.WithTask(context.Background(), 1, func(s string) { entries = append(entries, s) })
	for i := 0; i < 4; i++ {
		raw, err := c.Stock(ctx, config(), Session{Token: "private-token", UserID: "99"}, "101")
		if i < 2 && (err != nil || string(raw) != `{"code":1,"data":[]}`) {
			t.Fatal("debug altered response")
		}
		if i >= 2 && err == nil {
			t.Fatal("error response accepted")
		}
	}
	if calls != 4 || len(entries) != 4 || strings.Count(logs.String(), `"msg":"network response"`) != 4 || strings.Contains(logs.String(), "private-token") || !strings.Contains(entries[3], "996") {
		t.Fatal("missing per-request logs or credential leaked")
	}
}
