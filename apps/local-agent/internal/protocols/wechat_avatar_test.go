package protocols

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"robot/apps/local-agent/internal/store"
)

func TestWeChatScanAvatarSurvivesConfirmationAndReload(t *testing.T) {
	polls := 0
	avatar := "https://example.com/avatar.jpg"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var data any
		code := 0
		switch r.URL.Path {
		case "/api/Login/LoginGetQRCar":
			code = 1
			data = map[string]string{"Uuid": "scan", "QrBase64": "data:image/png;base64,dGVzdA=="}
		case "/api/Login/LoginCheckQR":
			polls++
			if polls == 1 {
				data = map[string]any{"status": 1, "headImgUrl": avatar}
			} else {
				data = map[string]any{"acctSectResp": map[string]string{"userName": "wx-test", "nickName": "测试"}}
			}
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"Code": code, "Success": true, "Data": data})
	}))
	defer srv.Close()
	db, err := store.Open(filepath.Join(t.TempDir(), "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	m, err := New(t.TempDir(), "avatar-test", db)
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	_, portText, _ := net.SplitHostPort(srv.Listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	m.mu.Lock()
	m.doc.Runtime["wechat"] = runtimeRecord{Port: port}
	m.state["wechat"] = "running"
	m.active = "wechat"
	m.workCtx, m.workCancel = context.WithCancel(m.ctx)
	m.mu.Unlock()
	if _, err := m.Begin(1, "wechat"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(8 * time.Second)
	for !terminal(m.Attempt(1).Status) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if m.Attempt(1).Status != "confirmed" || m.Bindings()[1].Account.Avatar != avatar {
		t.Fatalf("avatar not bound: %+v", m.Bindings())
	}
	m.Close()
	reloaded, err := New(t.TempDir(), "avatar-test", db)
	if err != nil {
		t.Fatal(err)
	}
	defer reloaded.Close()
	if reloaded.Bindings()[1].Account.Avatar != avatar {
		t.Fatal("avatar lost after reload")
	}
}
