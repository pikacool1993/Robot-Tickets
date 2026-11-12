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
	"sync"
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
			d = map[string]any{"id": 42, "openid": "openid-42", "nickname": "昵称"}
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
	if e != nil || !confirmed || result.Account == nil || result.Account.Ref != "openid-42" {
		t.Fatalf("confirm not completed: %+v %v", result, e)
	}
}

func TestProtocolSpecificClientCapabilities(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		var response any
		switch r.URL.Path {
		case "/api/User/GetContractProfile":
			response = map[string]any{"Code": 0, "Success": true, "Data": map[string]any{"userInfo": map[string]any{}}}
		case "/api/Login/AutoHeartBeat", "/api/Login/CloseAutoHeartBeat":
			response = map[string]any{"Code": 0, "Success": true}
		case "/health":
			response = map[string]any{"code": 0, "data": map[string]any{"ok": true}}
		case "/accounts/resync":
			response = map[string]any{"code": 0, "data": map[string]any{"openid": "openid-42", "nickname": "更新昵称"}}
		default:
			t.Errorf("unexpected endpoint: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer srv.Close()

	acc := Account{Ref: "openid-42"}
	w := adapt(&adapter{kind: "wechat", base: srv.URL, client: localClient(time.Second)})
	profile, ok := w.(AccountProfileReader)
	if !ok {
		t.Fatal("wechat adapter does not expose contract profile")
	}
	if _, err := profile.GetAccountProfile(context.Background(), acc); err != nil {
		t.Fatalf("get profile: %v", err)
	}
	heartbeat, ok := w.(AutoHeartbeatController)
	if !ok {
		t.Fatal("wechat adapter does not expose automatic heartbeat control")
	}
	if err := heartbeat.SetAutoHeartbeat(context.Background(), acc, true); err != nil {
		t.Fatalf("enable automatic heartbeat: %v", err)
	}
	if err := heartbeat.SetAutoHeartbeat(context.Background(), acc, false); err != nil {
		t.Fatalf("disable automatic heartbeat: %v", err)
	}

	y := adapt(&adapter{kind: "yyb", base: srv.URL, client: localClient(time.Second)})
	health, ok := y.(HealthChecker)
	if !ok || health.CheckHealth(context.Background()) != nil {
		t.Fatal("YYB health check failed")
	}
	syncer, ok := y.(AccountSynchronizer)
	if !ok {
		t.Fatal("YYB adapter does not expose account sync")
	}
	if _, err := syncer.SyncAccount(context.Background(), acc); err != nil {
		t.Fatalf("sync account: %v", err)
	}
	avatar, ok := y.(AvatarURLProvider)
	if !ok || avatar.AvatarURL(acc) != srv.URL+"/accounts/avatar?ref=openid-42" {
		t.Fatal("YYB avatar URL does not match the protocol client endpoint")
	}
	want := []string{
		"POST /api/User/GetContractProfile?wxid=openid-42",
		"POST /api/Login/AutoHeartBeat?wxid=openid-42",
		"POST /api/Login/CloseAutoHeartBeat?wxid=openid-42",
		"GET /health",
		"POST /accounts/resync",
	}
	if len(calls) != len(want) {
		t.Fatalf("unexpected protocol calls: %#v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("call %d = %q, want %q", i, calls[i], want[i])
		}
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
			response = map[string]any{"Code": 0, "Success": true, "Data": map[string]any{"headImgUrl": "https://example.com/avatar.jpg", "acctSectResp": map[string]string{"userName": "wx-test", "nickName": "测试"}}}
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
	if e != nil || scan.Account == nil || scan.Account.Ref != "wx-test" || scan.Account.Avatar != "https://example.com/avatar.jpg" {
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

func TestMaintenanceLogsEveryRoundForEachBoundTask(t *testing.T) {
	state := "alive"
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/accounts/refresh" {
			t.Errorf("unexpected endpoint %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]string{"status": state}})
	}))
	defer srv.Close()
	_, portText, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	db, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	manager, err := New(t.TempDir(), "test", db)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	manager.doc.Runtime["yyb"] = runtimeRecord{Port: port}
	manager.doc.Bindings[1] = Binding{Protocol: "yyb", Account: Account{Ref: "same-account"}, Status: "alive"}
	manager.doc.Bindings[2] = manager.doc.Bindings[1]
	logs := map[int64][]string{}
	levels := map[int64][]string{}
	manager.OnTaskLog = func(id int64, level, text string) {
		logs[id] = append(logs[id], text)
		levels[id] = append(levels[id], level)
	}
	for i := 0; i < 2; i++ {
		manager.checkAccounts(context.Background(), "yyb")
	}
	if calls != 2 {
		t.Fatalf("expected one shared-account request each round, got %d", calls)
	}
	for _, id := range []int64{1, 2} {
		if len(logs[id]) != 2 || logs[id][1] != "YYB 账号保活检查成功：账号在线" {
			t.Fatalf("missing repeated healthy logs: %v", logs)
		}
	}
	state = "expired"
	manager.checkAccounts(context.Background(), "yyb")
	if levels[1][2] != "error" || manager.Bindings()[1].Status != "expired" {
		t.Fatal("missing expired result")
	}
	state = "unknown"
	manager.checkAccounts(context.Background(), "yyb")
	if levels[1][3] != "warning" {
		t.Fatal("unknown result should warn")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	manager.checkAccounts(ctx, "yyb")
	if len(logs[1]) != 4 {
		t.Fatal("cancelled round emitted a completed heartbeat")
	}
}

func TestMiniProgramCodeUsesBoundAccount(t *testing.T) {
	for _, kind := range []string{"wechat", "yyb"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				if r.Header.Get("Authorization") != "Bearer service-token" {
					t.Error("unauthenticated local request")
				}
				if kind == "wechat" {
					if r.URL.Path != "/api/Wxapp/JSLogin" || body["Wxid"] != "account-ref" || body["Appid"] != "test-app" {
						t.Error("wrong WeChat request")
					}
					json.NewEncoder(w).Encode(map[string]any{"Success": true, "Code": 0, "Data": map[string]any{"BaseResponse": map[string]int{"ret": 0}, "code": "temporary"}})
				} else {
					if r.URL.Path != "/wxapp/getCode" || body["ref"] != "account-ref" || body["app_id"] != "test-app" {
						t.Error("wrong YYB request")
					}
					json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": map[string]any{"result": map[string]string{"code": "temporary", "errMsg": "login:ok"}}})
				}
			}))
			defer srv.Close()
			_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
			n, _ := strconv.Atoi(port)
			binding := Binding{Protocol: kind, Account: Account{Ref: "account-ref"}, Generation: "new", Status: "alive"}
			m := &Manager{active: kind, state: map[string]string{kind: "running"}, accountLocks: map[string]*sync.Mutex{}, doc: persisted{Runtime: map[string]runtimeRecord{kind: {Port: n, Token: "service-token"}}, Bindings: map[int64]Binding{1: binding}}}
			code, err := m.MiniProgramCode(t.Context(), 1, binding, "test-app")
			if err != nil || code != "temporary" {
				t.Fatalf("code=%q err=%v", code, err)
			}
			stale := binding
			stale.Generation = "old"
			if _, err = m.MiniProgramCode(t.Context(), 1, stale, "test-app"); err == nil {
				t.Fatal("accepted stale binding")
			}
			b := binding
			b.Status = "expired"
			m.doc.Bindings[1] = b
			if _, err = m.MiniProgramCode(t.Context(), 1, binding, "test-app"); err == nil {
				t.Fatal("accepted expired account")
			}
			if calls != 1 {
				t.Fatal("called protocol for stale account")
			}
		})
	}
}

func TestResumePausedTask(t *testing.T) {
	for _, mode := range []string{"online", "unknown", "expired", "stopped", "other-protocol", "cancelled", "save-failed"} {
		t.Run(mode, func(t *testing.T) {
			db, err := store.Open(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			binding := Binding{Protocol: "wechat", Account: Account{Ref: "account"}, Status: "alive", Paused: true, Generation: "binding"}
			m := &Manager{db: db, active: "wechat", state: map[string]string{"wechat": "running"}, manual: map[string]bool{}, workCtx: ctx, doc: persisted{Bindings: map[int64]Binding{1: binding}, Runtime: map[string]runtimeRecord{}}}
			switch mode {
			case "unknown", "expired":
				b := binding
				b.Status = mode
				m.doc.Bindings[1] = b
			case "stopped":
				m.state["wechat"] = "stopped"
			case "other-protocol":
				m.active = "yyb"
			case "cancelled":
				cancel()
			case "save-failed":
				db.Close()
			}
			err = m.ResumeTask(1)
			if mode == "online" {
				if err != nil {
					t.Fatal(err)
				}
				b := m.Bindings()[1]
				if b.Paused || b.Account != binding.Account || b.Generation != binding.Generation {
					t.Fatalf("unexpected binding: %+v", b)
				}
				raw, err := db.Get("protocols.v1")
				if err != nil {
					t.Fatal(err)
				}
				var saved persisted
				if json.Unmarshal(raw, &saved) != nil || saved.Bindings[1].Paused {
					t.Fatal("resume not persisted")
				}
				if err = m.ResumeTask(1); err != nil {
					t.Fatal("repeat resume failed", err)
				}
			} else {
				if err == nil || !m.Bindings()[1].Paused {
					t.Fatal("unsafe resume", err)
				}
			}
		})
	}
}
