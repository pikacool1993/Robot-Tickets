package protocols

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"robot/apps/local-agent/internal/store"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This child implements only local test endpoints, never real protocol/upstream calls.
func TestMain(m *testing.M) {
	if os.Getenv("ROBOT_TEST_HELPER") == "1" {
		mux := http.NewServeMux()
		var count atomic.Int64
		mux.HandleFunc("/robot/health", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"instance": os.Getenv("ROBOT_PROTOCOL_INSTANCE"), "service": "yyb", "pid": os.Getpid()})
		})
		mux.HandleFunc("/qr", func(w http.ResponseWriter, r *http.Request) {
			id := count.Add(1)
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"session_id": strconv.FormatInt(id, 10), "image_base64": "data:image/png;base64,dGVzdA=="}})
		})
		mux.HandleFunc("/qr/", func(w http.ResponseWriter, r *http.Request) {
			data := any(map[string]string{"status": "authorized"})
			if r.URL.Path[len(r.URL.Path)-7:] == "confirm" {
				data = map[string]any{"id": 12, "nickname": "测试账号"}
			}
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
		})
		mux.HandleFunc("/accounts/refresh", func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"status": "alive"}})
		})
		mux.HandleFunc("/accounts", func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(map[string]any{"code": 0}) })
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer "+os.Getenv("ROBOT_PROTOCOL_TOKEN") {
				w.WriteHeader(401)
				return
			}
			mux.ServeHTTP(w, r)
		})
		if e := http.ListenAndServe("127.0.0.1:"+os.Getenv("ROBOT_PROTOCOL_PORT"), handler); e != nil {
			os.Exit(2)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func TestRuntimeReconnectAuthorizationAndExplicitStop(t *testing.T) {
	probe, err := net.Listen("tcp", "127.0.0.1:8088")
	if err != nil {
		t.Skip("YYB 固定端口 8088 已被占用，跳过需要启动真实测试子进程的生命周期测试")
	}
	probe.Close()
	t.Setenv("ROBOT_TEST_HELPER", "1")
	root := t.TempDir()
	dir := filepath.Join(root, "protocols", "yyb")
	if e := os.MkdirAll(dir, 0700); e != nil {
		t.Fatal(e)
	}
	source, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	binary, e := os.ReadFile(source)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(dir, "yyb-go.exe"), binary, 0700); e != nil {
		t.Fatal(e)
	}
	db, e := store.Open(filepath.Join(root, "data"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	first, e := New(root, "test-instance", db)
	if e != nil {
		t.Fatal(e)
	}
	defer first.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if e = first.Ensure(ctx, "yyb", "", false); e != nil {
		t.Fatal(e)
	}
	if first.Status("yyb").Address != "127.0.0.1:8088" {
		t.Fatal("YYB did not use its fixed port")
	}
	first.mu.Lock()
	pid := first.doc.Runtime["yyb"].PID
	record := first.doc.Runtime["yyb"]
	first.mu.Unlock()
	defer func() { p, _ := os.FindProcess(pid); _ = p.Kill() }()
	wrong := record
	wrong.Created++
	if first.processAlive("yyb", wrong) {
		t.Fatal("accepted a reused PID")
	}
	if e = first.Ensure(ctx, "yyb", "http://user:pass@127.0.0.1:2022", false); !errors.Is(e, ErrRestartRequired) {
		t.Fatalf("proxy change was not confirmed: %v", e)
	}
	first.Activate("yyb")
	if _, e = first.Begin(1, "yyb"); e != nil {
		t.Fatal(e)
	}
	deadline := time.Now().Add(8 * time.Second)
	for !terminal(first.Attempt(1).Status) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if first.Attempt(1).Status != "confirmed" || first.Bindings()[1].Account.Ref != "12" {
		t.Fatalf("authorization did not bind account: %+v", first.Attempt(1))
	}
	// New QR sessions cancel the previous operation without clearing a usable old binding.
	old, _ := first.Begin(1, "yyb")
	newer, _ := first.Begin(1, "yyb")
	if old.ID == newer.ID || first.Bindings()[1].Account.Ref != "12" {
		t.Fatal("re-authorization lost the old binding")
	}
	first.Deactivate()
	first.Close()
	if !first.identity(ctx, "yyb") {
		t.Fatal("closing client terminated child service")
	}
	second, e := New(root, "test-instance", db)
	if e != nil {
		t.Fatal(e)
	}
	defer second.Close()
	if e = second.Ensure(ctx, "yyb", "", false); e != nil {
		t.Fatal(e)
	}
	second.mu.Lock()
	pid2 := second.doc.Runtime["yyb"].PID
	second.mu.Unlock()
	if pid2 != pid {
		t.Fatal("reconnect created a duplicate service")
	}
	if second.Bindings()[1].Account.Ref != "12" {
		t.Fatal("shared account did not survive restart")
	}
	second.Activate("yyb")
	if e = second.DeleteTask(ctx, 1); e != nil {
		t.Fatal(e)
	}
	if len(second.Bindings()) != 0 {
		t.Fatal("deleted task still bound")
	}
	if e = second.Control(ctx, "yyb", "stop", false); e == nil {
		t.Fatal("stop did not require confirmation")
	}
	if e = second.Control(ctx, "yyb", "stop", true); e != nil {
		t.Fatal(e)
	}
	if second.processAlive("yyb", record) || second.Status("yyb").State != "stopped" {
		t.Fatal("explicit stop did not stop owned service")
	}
}
func TestYYBPortConflictDoesNotReuseSavedOrRandomPort(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:8088")
	if err == nil {
		defer listener.Close()
	} else {
		connection, dialErr := net.DialTimeout("tcp", "127.0.0.1:8088", time.Second)
		if dialErr != nil {
			t.Fatalf("cannot establish a fixed-port conflict: %v", err)
		}
		connection.Close()
	}
	root := t.TempDir()
	dir := filepath.Join(root, "protocols", "yyb")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	// The executable must never be launched when the fixed port is occupied.
	if err := os.WriteFile(filepath.Join(dir, "yyb-go.exe"), []byte("not an executable"), 0600); err != nil {
		t.Fatal(err)
	}
	db, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := New(root, "fixed-port-test", db)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.doc.Runtime["yyb"] = runtimeRecord{Port: 51546}
	err = m.Ensure(context.Background(), "yyb", "", false)
	if err == nil || !strings.Contains(err.Error(), "127.0.0.1:8088") || !strings.Contains(err.Error(), "占用") {
		t.Fatalf("expected fixed-port conflict, got %v", err)
	}
	if m.doc.Runtime["yyb"].PID != 0 || m.state["yyb"] != "failed" {
		t.Fatal("port conflict launched a service")
	}
	if m.Status("yyb").Address != "127.0.0.1:8088" {
		t.Fatal("status still displays the saved random port")
	}
}
func TestProxyValidation(t *testing.T) {
	for _, input := range []string{"", "http://user:p%40ss@127.0.0.1:2022", "socks5://127.0.0.1:1080"} {
		if _, e := ValidateProxy(input); e != nil {
			t.Fatalf("valid proxy rejected: %v", e)
		}
	}
	for _, input := range []string{"http://user:secret@host", "http://host:0", "http://host:99999", "http://host:8080/path", "socks5://user:pass@host:1080"} {
		if _, e := ValidateProxy(input); e == nil {
			t.Fatal("invalid proxy accepted")
		}
	}
	_, e := ValidateProxy("http://user:secret%ZZ@host:2022")
	if e == nil || fmt.Sprint(e) == "" {
		t.Fatal("invalid escaping accepted")
	}
}
