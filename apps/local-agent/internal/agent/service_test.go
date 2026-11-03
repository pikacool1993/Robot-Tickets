package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/store"
	"sync/atomic"
	"testing"
	"time"
)

func TestLoginRememberRestartAndExpiry(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	keys := protocol.Keyring{"client-v1": key}
	var revoked atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		code := "ok"
		status := 200
		switch r.URL.Path {
		case "/api/agent/v1/auth/login":
			var env protocol.Envelope
			if json.NewDecoder(r.Body).Decode(&env) != nil {
				t.Error("invalid request envelope")
				return
			}
			plain, err := keys.Open(env)
			if err != nil {
				t.Error(err)
				return
			}
			var in struct {
				Username, Password string
				ClientNumber       string `json:"client_number"`
				InstanceID         string `json:"client_instance_id"`
			}
			if json.Unmarshal(plain, &in) != nil || in.Password != "test-password" {
				t.Error("invalid login payload")
				return
			}
			data = map[string]any{"access_token": "test-token", "expires_at": time.Now().Add(time.Hour), "agent": map[string]string{"id": "agent", "name": "测试代理商"}, "client_number": in.ClientNumber, "client_instance_id": in.InstanceID}
		case "/api/agent/v1/auth/me":
			if r.Header.Get("Authorization") != "Bearer test-token" {
				t.Error("missing bearer token")
			}
			if revoked.Load() {
				status = 401
				code = "unauthorized"
			}
		case "/api/agent/v1/auth/logout":
		default:
			t.Error("unexpected endpoint")
		}
		plain, _ := json.Marshal(map[string]any{"code": code, "data": data})
		env, err := keys.Seal("client-v1", plain)
		if err != nil {
			t.Error(err)
			return
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(env)
	}))
	defer server.Close()
	dir := t.TempDir()
	db, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := protocol.Config{Version: 1, ServerURL: server.URL, TransportKeyID: "client-v1", TransportKey: base64.StdEncoding.EncodeToString(key), ExportID: "test", IssuedAt: time.Now()}
	data, _ := json.Marshal(c)
	if err := db.ReplaceConfig(data); err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(dir, db, nil, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	form := Form{"user", "test-password", "A", true}
	if err := s.Login(form); err != nil {
		t.Fatal(err)
	}
	if state := s.Snapshot(); !state.LoggedIn || state.AgentName != "测试代理商" {
		t.Fatal("login state missing")
	}
	remembered, err := s.Remembered()
	if err != nil || remembered != form {
		t.Fatal("remembered form mismatch")
	}
	s.Close()
	s, err = New(dir, db, nil, nil, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if s.Snapshot().LoggedIn {
		t.Fatal("session persisted across restart")
	}
	if remembered, err := s.Remembered(); err != nil || remembered != form {
		t.Fatal("remembered form lost")
	}
	form.Remember = false
	form.ClientNumber = "B"
	if err := s.Login(form); err != nil {
		t.Fatal(err)
	}
	if remembered, _ := s.Remembered(); remembered.Remember || remembered.Password != "" {
		t.Fatal("unchecked form was not cleared")
	}
	// Expiry must return to login even without another business request.
	s.mu.Lock()
	s.session.ExpiresAt = time.Now().Add(-time.Second)
	s.mu.Unlock()
	deadline := time.Now().Add(3 * time.Second)
	for s.Snapshot().LoggedIn && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if s.Snapshot().LoggedIn {
		t.Fatal("expired session remained logged in")
	}
	if err := s.Login(form); err != nil {
		t.Fatal(err)
	}
	revoked.Store(true)
	s.mu.Lock()
	err = s.client.Call(context.Background(), "GET", "/auth/me", s.session.Token, nil, nil)
	s.mu.Unlock()
	if err == nil {
		t.Fatal("revoked session accepted")
	}
}
