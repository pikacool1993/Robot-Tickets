package protocols

import (
	"encoding/base64"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestUserEncryptionKeyUsesBoundProtocolAndRejectsStaleResponse(t *testing.T) {
	for _, kind := range []string{"wechat", "yyb"} {
		t.Run(kind, func(t *testing.T) {
			calls := 0
			bad := false
			staleDuringCall := false
			var m *Manager
			raw := `{"encrypt_key":"MDEyMzQ1Njc4OWFiY2RlZg==","iv":"0123456789abcdef","version":54,"create_time":1000,"expire_in":50}`
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") != "Bearer service-token" {
					t.Error("missing protocol authentication")
				}
				var body map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&body)
				var payload map[string]any
				if kind == "wechat" {
					var ref, app, data string
					_ = json.Unmarshal(body["Wxid"], &ref)
					_ = json.Unmarshal(body["Appid"], &app)
					_ = json.Unmarshal(body["Data"], &data)
					_ = json.Unmarshal([]byte(data), &payload)
					if ref != "account-ref" || app != "test-app" || r.URL.Path != "/api/Wxapp/CloudCallFunction" {
						t.Error("wrong WeChat key request")
					}
				} else {
					var ref, app string
					_ = json.Unmarshal(body["ref"], &ref)
					_ = json.Unmarshal(body["app_id"], &app)
					_ = json.Unmarshal(body["payload"], &payload)
					if ref != "account-ref" || app != "test-app" || r.URL.Path != "/wxapp/operateWxData" {
						t.Error("wrong YYB key request")
					}
				}
				if payload["api_name"] != "webapi_getuserencryptkey" || payload["env"] != float64(1) || payload["operate_directly"] != false || payload["data"] == nil || payload["tid"] == nil {
					t.Error("wrong key payload")
				}
				errNo := 0
				if bad {
					errNo = 3
				}
				result := map[string]any{"err_no": errNo, "data": raw}
				if staleDuringCall {
					m.mu.Lock()
					b := m.doc.Bindings[1]
					b.Generation = "rebound"
					m.doc.Bindings[1] = b
					m.mu.Unlock()
				}
				var data any = map[string]any{"result": result}
				if kind == "wechat" {
					b, _ := json.Marshal(result)
					data = map[string]any{"data": base64.StdEncoding.EncodeToString(b), "BaseResponse": map[string]any{"ret": 0}}
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "success": true, "data": data})
			}))
			defer srv.Close()
			_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
			n, _ := strconv.Atoi(port)
			binding := Binding{Protocol: kind, Account: Account{Ref: "account-ref"}, Generation: "original", Status: "alive"}
			m = &Manager{active: kind, state: map[string]string{kind: "running"}, accountLocks: map[string]*sync.Mutex{}, doc: persisted{Runtime: map[string]runtimeRecord{kind: {Port: n, Token: "service-token"}}, Bindings: map[int64]Binding{1: binding}}}
			key, err := m.UserEncryptionKey(t.Context(), 1, binding, "test-app")
			if err != nil || !key.Valid(time.Now()) || key.Version != 54 || key.Remaining(time.Now()) > 50*time.Second {
				t.Fatalf("valid response rejected: %v", err)
			}
			old := binding
			old.Generation = "old"
			if _, err := m.UserEncryptionKey(t.Context(), 1, old, "test-app"); err == nil || calls != 1 {
				t.Fatal("stale binding made a request")
			}
			bad = true
			if _, err := m.UserEncryptionKey(t.Context(), 1, binding, "test-app"); err == nil {
				t.Fatal("inner failure accepted")
			}
			bad = false
			staleDuringCall = true
			if _, err := m.UserEncryptionKey(t.Context(), 1, binding, "test-app"); err == nil {
				t.Fatal("late response crossed account rebinding")
			}
		})
	}
}
