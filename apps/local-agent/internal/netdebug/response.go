// Package netdebug records response bodies only when explicitly enabled by the desktop.
package netdebug

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

type config struct {
	logger  *slog.Logger
	enabled bool
}

var current atomic.Pointer[config]

func Configure(logger *slog.Logger, enabled bool) { current.Store(&config{logger, enabled}) }

type taskKey struct{}
type taskSink struct {
	id    int64
	write func(string)
}

func WithTask(ctx context.Context, id int64, write func(string)) context.Context {
	return context.WithValue(ctx, taskKey{}, taskSink{id, write})
}

// Response never receives request headers/body or logs URL queries/signatures.
func Response(ctx context.Context, service, method, path string, status int, raw []byte, started time.Time, err error) {
	c := current.Load()
	if c == nil || !c.enabled || c.logger == nil {
		return
	}
	if u, e := url.Parse(path); e == nil {
		path = u.Path
	} else {
		path = "[invalid path]"
	}
	body := string(raw)
	var value any
	decoder := json.NewDecoder(strings.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&value) == nil && json.Valid(raw) {
		value = sanitize(value, 0)
		b, e := json.Marshal(value)
		if e == nil {
			body = string(b)
		}
	}
	state := "响应已读取"
	if err != nil {
		state = "读取失败或请求未获得完整响应"
	}
	task, _ := ctx.Value(taskKey{}).(taskSink)
	ms := time.Since(started).Milliseconds()
	c.logger.DebugContext(ctx, "network response", "service", service, "method", method, "path", path, "task_id", task.id, "http_status", status, "elapsed_ms", ms, "bytes", len(raw), "result", state, "body", body)
	if task.write != nil {
		task.write(fmt.Sprintf("[DEBUG] %s %s %s HTTP %d，耗时 %dms，%s，响应：%s", service, method, path, status, ms, state, body))
	}
}

func sanitize(v any, depth int) any {
	if depth > 12 {
		return "[嵌套内容省略]"
	}
	switch x := v.(type) {
	case map[string]any:
		for k, value := range x {
			if strings.EqualFold(k, "code") {
				if s, ok := value.(string); ok && len(s) >= 24 && !strings.ContainsAny(s, "_- ") {
					x[k] = "[REDACTED]"
					continue
				}
			}
			switch strings.ToLower(k) {
			case "webhook", "token", "access_token", "refresh_token", "authorization", "password", "encrypt_key", "session_key", "sessionkey", "iv", "signature", "s_sign", "encrypteddata", "authcode", "login_code", "code2session", "real_card_id2", "phone2", "qrcode", "qr_code", "qrbase64", "qr_code_base64":
				x[k] = "[REDACTED]"
			default:
				x[k] = sanitize(value, depth+1)
			}
		}
	case []any:
		for i := range x {
			x[i] = sanitize(x[i], depth+1)
		}
	case string:
		// Protocol responses can wrap JSON in strings or base64 strings.
		if inner, ok := embeddedJSON([]byte(x)); ok {
			return sanitize(inner, depth+1)
		}
		if decoded, e := base64.StdEncoding.DecodeString(x); e == nil {
			if inner, ok := embeddedJSON(decoded); ok {
				return sanitize(inner, depth+1)
			}
		}
	}
	return v
}

func embeddedJSON(raw []byte) (any, bool) {
	s := strings.TrimSpace(string(raw))
	if len(s) == 0 || (s[0] != '{' && s[0] != '[') || !json.Valid(raw) {
		return nil, false
	}
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	var value any
	err := d.Decode(&value)
	return value, err == nil
}
