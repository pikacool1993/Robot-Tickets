package business

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func config() Config {
	return Config{Ready: true, AppID: AppID, DeviceType: "ios", Template: &Template{UserAgent: "iPhone", Referer: "https://servicewechat.com/" + AppID + "/78/page-frame.html", E: map[string]any{"sessionId": "must-be-removed"}, S: map[string]any{"platform": "ios"}, V: "240222"}}
}
func TestDeviceIDUnpadded(t *testing.T) {
	for _, n := range []int64{0, 1, 42, 9999999} {
		got := formatDeviceID(1794449258716, n)
		want := map[int64]string{0: "17944492587160", 1: "17944492587161", 42: "179444925871642", 9999999: "17944492587169999999"}[n]
		if got != want {
			t.Fatalf("%s != %s", got, want)
		}
	}
	for i := 0; i < 100; i++ {
		id, err := NewDeviceID()
		if err != nil || len(id) < 14 || len(id) > 20 {
			t.Fatalf("invalid generated ID %s: %v", id, err)
		}
	}
}
func TestLoginAndPeopleContract(t *testing.T) {
	cfg := config()
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("User-Agent") != cfg.Template.UserAgent || r.Header.Get("Referer") != cfg.Template.Referer {
			t.Error("missing device headers")
		}
		raw, _ := io.ReadAll(r.Body)
		switch r.URL.Path {
		case "/fc/wx_api/v1/Login/index":
			var body map[string]any
			_ = json.Unmarshal(raw, &body)
			if body["code"] != "short-lived" || body["s"].(map[string]any)["deviceId"] != "17944492587160" {
				t.Error("missing dynamic fields")
			}
			if _, ok := body["e"].(map[string]any)["sessionId"]; ok {
				t.Error("iOS sessionId leaked")
			}
			io.WriteString(w, `{"code":200,"token":"private-token","info":{"id":123}}`)
		case "/fc/wx_api/v1/Member/getCrewsList":
			if r.URL.Query().Get("lid2") != "123" || r.Header.Get("Authorization") != "Bearer private-token" || string(raw) != "{}" {
				t.Error("invalid spectators request")
			}
			io.WriteString(w, `{"code":1,"data":[{"id":7,"realname":"测试","real_card_id":"123***456","phone":"138****0000","real_card_id2":"sensitive-card","phone2":"sensitive-phone","signature":"secret"}]}`)
		default:
			t.Error("wrong URL")
		}
	}))
	defer srv.Close()
	c := NewClient()
	defer c.Close()
	c.base = srv.URL
	s, err := c.Login(t.Context(), cfg, "17944492587160", "short-lived")
	if err != nil {
		t.Fatal(err)
	}
	people, err := c.People(t.Context(), cfg, s)
	if err != nil || len(people) != 1 || people[0].ID != "7" || people[0].Signature != "secret" {
		t.Fatalf("%+v %v", people, err)
	}
	if calls != 2 || cfg.Template.S["deviceId"] != nil || cfg.Template.E["sessionId"] == nil {
		t.Fatal("mutated session template")
	}
}
func TestBusinessMalformedResponses(t *testing.T) {
	for _, raw := range []string{`{"code":200,"info":{"id":1}}`, `{"code":200,"token":"secret","info":{}}`, `<html>bad</html>`, `{"code":40029,"msg":"sensitive raw error"}`} {
		t.Run(raw, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, raw) }))
			defer srv.Close()
			c := NewClient()
			defer c.Close()
			c.base = srv.URL
			_, err := c.Login(t.Context(), config(), "123", "code")
			if err == nil || strings.Contains(err.Error(), "sensitive") {
				t.Fatalf("unsafe error %v", err)
			}
			if IsCodeExpired(err) {
				t.Fatal("guessed N1 business code")
			}
		})
	}
}

type tempError struct{}

func (tempError) Error() string { return "network" }
func (tempError) Timeout() bool { return true }
func TestRetryBoundAndCancellation(t *testing.T) {
	n := 0
	err := Retry(t.Context(), time.Millisecond, func() error { n++; return tempError{} })
	if err == nil || n != 4 {
		t.Fatalf("attempts=%d", n)
	}
	n = 0
	_ = Retry(t.Context(), 0, func() error { n++; return errors.New("ordinary") })
	if n != 1 {
		t.Fatal("retried ordinary failure")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	n = 0
	err = Retry(ctx, time.Hour, func() error { n++; return tempError{} })
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Fatal("did not cancel retry")
	}
}
func TestDirectTransportAndTokenFailure(t *testing.T) {
	c := NewClient()
	defer c.Close()
	tr := c.http.Transport.(*http.Transport)
	if tr.Proxy != nil {
		t.Fatal("business requests must be direct")
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(401); io.WriteString(w, `{"code":9}`) }))
	defer srv.Close()
	c.base = srv.URL
	_, err := c.People(t.Context(), config(), Session{"private", "1"})
	if !IsTokenExpired(err) {
		t.Fatal(err)
	}
}

func TestFlowLoginAndReplayBound(t *testing.T) {
	for _, mode := range []string{"first", "cached", "expired", "expired-twice", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			loginCalls, peopleCalls, codeCalls := 0, 0, 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.URL.Path, "Login") {
					loginCalls++
					io.WriteString(w, `{"code":200,"token":"new-token","info":{"id":"123"}}`)
					return
				}
				peopleCalls++
				if (mode == "expired" && peopleCalls == 1) || mode == "expired-twice" {
					w.WriteHeader(401)
					return
				}
				if mode == "unknown" {
					io.WriteString(w, `{"code":999,"msg":"do not expose"}`)
					return
				}
				io.WriteString(w, `{"code":1,"data":[]}`)
			}))
			defer srv.Close()
			c := NewClient()
			defer c.Close()
			c.base = srv.URL
			session := Session{"cached-token", "123"}
			if mode == "first" {
				session = Session{}
			}
			_, _, err := c.Refresh(t.Context(), config(), "123", session, func(context.Context) (string, error) { codeCalls++; return "ephemeral-code", nil }, func(_, text string) {
				if strings.Contains(text, "token") || strings.Contains(text, "ephemeral") {
					t.Error("leaked secret to logs")
				}
			})
			wantLogins, wantPeople := 0, 1
			if mode == "first" {
				wantLogins = 1
			}
			if strings.HasPrefix(mode, "expired") {
				wantLogins = 1
				wantPeople = 2
			}
			if loginCalls != wantLogins || codeCalls != wantLogins || peopleCalls != wantPeople {
				t.Fatalf("login=%d code=%d people=%d", loginCalls, codeCalls, peopleCalls)
			}
			if (mode == "expired-twice" || mode == "unknown") != (err != nil) {
				t.Fatalf("unexpected result %v", err)
			}
		})
	}
}
