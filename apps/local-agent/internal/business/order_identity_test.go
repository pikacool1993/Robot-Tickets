package business

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOrderIdentityRetainsDistinctUIDAndLoginCode(t *testing.T) {
	var p Person
	if err := json.Unmarshal([]byte(`{"id":11,"uid":99,"realname":"测试"}`), &p); err != nil || p.ID != "11" || p.UID != "99" {
		t.Fatalf("identity lost: %+v %v", p, err)
	}
	raw, _ := json.Marshal(p)
	var restored Person
	if err := json.Unmarshal(raw, &restored); err != nil || restored.UID != "99" {
		t.Fatal("uid lost on task persistence")
	}
	if err := json.Unmarshal([]byte(`{"id":12}`), &p); err != nil || p.UID != "" {
		t.Fatal("missing UID inherited stale value")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"code":200,"token":"synthetic-token","info":{"id":99}}`)
	}))
	defer srv.Close()
	c := NewClient()
	defer c.Close()
	c.base = srv.URL
	session, err := c.Login(t.Context(), config(), "synthetic-device", "synthetic-code")
	if err != nil || session.AuthCode != "synthetic-code" {
		t.Fatal("successful login code lost")
	}
	raw, _ = json.Marshal(session)
	var exposed map[string]any
	_ = json.Unmarshal(raw, &exposed)
	if _, ok := exposed["AuthCode"]; ok {
		t.Fatal("login code exposed through JSON")
	}
}

func TestLoginObserverRunsBeforePeopleRequest(t *testing.T) {
	observed := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fc/wx_api/v1/Login/index" {
			io.WriteString(w, `{"code":200,"token":"test-token","info":{"id":99}}`)
			return
		}
		if !observed {
			t.Error("people request preceded login observer")
		}
		io.WriteString(w, `{"code":1,"data":[]}`)
	}))
	defer srv.Close()
	c := NewClient()
	defer c.Close()
	c.base = srv.URL
	_, _, err := c.RefreshWithLogin(t.Context(), config(), "device", Session{}, func(context.Context) (string, error) { return "test-code", nil }, func(string, string) {}, func(s Session) { observed = s.AuthCode == "test-code" })
	if err != nil || !observed {
		t.Fatal("login observer did not run")
	}
}
