package netdebug

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestResponsesRetainInventoryAndRedactNestedSecrets(t *testing.T) {
	var out bytes.Buffer
	Configure(slog.New(slog.NewJSONHandler(&out, &slog.HandlerOptions{Level: slog.LevelDebug})), true)
	t.Cleanup(func() { Configure(nil, false) })
	var taskLogs []string
	ctx := WithTask(context.Background(), 7, func(s string) { taskLogs = append(taskLogs, s) })
	inner := base64.StdEncoding.EncodeToString([]byte(`{"err_no":0,"data":"{\"encrypt_key\":\"test-secret-key\",\"iv\":\"test-secret-iv\",\"version\":56,\"expire_in\":49}"}`))
	raw, _ := json.Marshal(map[string]any{"webhook": "secret-webhook", "code": 1, "token": "secret-token", "data": inner, "price": "180.00", "id": json.Number("9007199254740993"), "usable_count": 0})
	Response(ctx, "N1", "POST", "/stock?token=secret-query", 200, raw, time.Now(), nil)
	log := out.String()
	for _, secret := range []string{"secret-webhook", "test-secret-key", "test-secret-iv", "secret-token", "secret-query", inner} {
		if strings.Contains(log, secret) {
			t.Fatal("response log leaked secret")
		}
	}
	var entry struct {
		Body string `json:"body"`
		Task int64  `json:"task_id"`
	}
	if json.Unmarshal(out.Bytes(), &entry) != nil || entry.Task != 7 || !strings.Contains(entry.Body, `"price":"180.00"`) || !strings.Contains(entry.Body, `"id":9007199254740993`) || !strings.Contains(entry.Body, `"usable_count":0`) || !strings.Contains(entry.Body, `"expire_in":49`) {
		t.Fatalf("response fields changed: %s", log)
	}
	if len(taskLogs) != 1 || !strings.Contains(taskLogs[0], "HTTP 200") {
		t.Fatal("task response missing")
	}
	Response(ctx, "N1", "POST", "/stock", 503, []byte("upstream unavailable"), time.Now(), nil)
	Response(ctx, "N1", "POST", "/stock", 0, nil, time.Now(), errors.New("unsafe error with token"))
	if len(taskLogs) != 3 || !strings.Contains(out.String(), "upstream unavailable") || strings.Contains(out.String(), "unsafe error") {
		t.Fatal("failure response handling incorrect")
	}
	Configure(slog.New(slog.NewJSONHandler(&out, nil)), false)
	n := out.Len()
	Response(ctx, "N1", "POST", "/stock", 200, raw, time.Now(), nil)
	if out.Len() != n || len(taskLogs) != 3 {
		t.Fatal("disabled logging still emitted")
	}
}
