package protocols

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"robot/apps/local-agent/internal/store"
	"strconv"
	"testing"
	"time"
)

func TestYYBAuthorizationRequiresConfirm(t *testing.T) {
	confirmed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			t.Error("missing local service authentication")
		}
		var d any
		switch r.URL.Path {
		case "/qr":
			d = map[string]string{"session_id": "session", "image_base64": "data:image/png;base64,dGVzdA=="}
		case "/qr/session/poll":
			d = map[string]string{"status": "authorized"}
		case "/qr/session/confirm":
			confirmed = true
			d = map[string]any{"id": 42, "nickname": "昵称"}
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": d})
	}))
	defer srv.Close()
	var p Protocol = adapt(&adapter{kind: "yyb", base: srv.URL, token: "test-token", client: localClient(time.Second)})
	qr, e := p.Begin(context.Background())
	if e != nil || qr.ID != "session" {
		t.Fatalf("begin: %v", e)
	}
	result, e := p.Poll(context.Background(), qr.ID)
	if e != nil || !confirmed || result.Account == nil || result.Account.Ref != "42" {
		t.Fatalf("confirm not completed: %+v %v", result, e)
	}
}

func TestFailedReauthorizationAndLogoutPreserveBinding(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		json.NewEncoder(w).Encode(map[string]any{"code": 502})
	}))
	defer server.Close()
	root := t.TempDir()
	db, err := store.Open(filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager, err := New(root, "test", db)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	_, portText, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	manager.mu.Lock()
	manager.doc.Runtime["yyb"] = runtimeRecord{Port: port}
	manager.state["yyb"] = "running"
	manager.doc.Bindings[1] = Binding{Protocol: "yyb", Account: Account{Ref: "old-account"}, Status: "alive"}
	manager.mu.Unlock()
	manager.mu.Lock()
	manager.active = "yyb"
	manager.workCtx, manager.workCancel = context.WithCancel(manager.ctx)
	manager.mu.Unlock()
	if _, err = manager.Begin(1, "yyb"); err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(3 * time.Second)
	for !terminal(manager.Attempt(1).Status) && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if manager.Attempt(1).Status != "failed" || manager.Bindings()[1].Account.Ref != "old-account" {
		t.Fatal("failed reauthorization destroyed old account")
	}
	if err = manager.DeleteTask(context.Background(), 1); err == nil || manager.Bindings()[1].Account.Ref != "old-account" {
		t.Fatal("failed logout removed binding")
	}
	manager.mu.Lock()
	manager.workCancel()
	manager.workCtx, manager.workCancel = context.WithDeadline(manager.ctx, time.Now().Add(-time.Second))
	manager.mu.Unlock()
	if _, err = manager.Begin(1, "yyb"); err != nil {
		t.Fatal(err)
	}
	until = time.Now().Add(3 * time.Second)
	for !terminal(manager.Attempt(1).Status) && time.Now().Before(until) {
		time.Sleep(10 * time.Millisecond)
	}
	if manager.Attempt(1).Status != "timeout" || manager.Bindings()[1].Account.Ref != "old-account" {
		t.Fatal("deadline did not time out while preserving the old account")
	}
}
func TestWeChatResponseCodesAndTransientFailure(t *testing.T) {
	stage := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var response any
		switch r.URL.Path {
		case "/api/Login/LoginGetQRCar":
			response = map[string]any{"Code": 1, "Success": true, "Data": map[string]string{"Uuid": "qr", "QrBase64": "data:image/png;base64,dGVzdA=="}}
		case "/api/Login/LoginCheckQR":
			response = map[string]any{"Code": 0, "Success": true, "Data": map[string]any{"acctSectResp": map[string]string{"userName": "wx-test", "nickName": "测试"}}}
		case "/robot/account":
			response = map[string]any{"Code": 0, "Success": true, "Data": map[string]string{"status": "alive"}}
		case "/api/Login/HeartBeat":
			if stage == 0 {
				response = map[string]any{"Code": -8, "Success": false}
			} else {
				response = map[string]any{"Code": 0, "Success": true, "Data": map[string]any{"baseResponse": map[string]int{"ret": 0}}}
			}
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()
	p := adapt(&adapter{kind: "wechat", base: srv.URL, client: localClient(time.Second)})
	if _, e := p.Begin(context.Background()); e != nil {
		t.Fatalf("QR success uses code 1: %v", e)
	}
	scan, e := p.Poll(context.Background(), "qr")
	if e != nil || scan.Account == nil || scan.Account.Ref != "wx-test" {
		t.Fatalf("account not decoded: %+v %v", scan, e)
	}
	state, e := p.Maintain(context.Background(), *scan.Account)
	if e == nil || state != "unknown" {
		t.Fatal("generic -8 must not erase or expire credentials")
	}
	stage = 1
	state, e = p.Maintain(context.Background(), *scan.Account)
	if e != nil || state != "alive" {
		t.Fatalf("heartbeat failed: %s %v", state, e)
	}
}
