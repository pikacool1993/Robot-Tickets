package ippool

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPProxyConnectAndCredentials(t *testing.T) {
	auth := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "CONNECT" || r.Host != "fccdn1.k4n.cc:443" {
			t.Errorf("unexpected proxy request %s %s", r.Method, r.Host)
		}
		auth <- r.Header.Get("Proxy-Authorization")
		w.WriteHeader(407)
	}))
	defer proxy.Close()
	m := manager(t)
	addr := strings.TrimPrefix(proxy.URL, "http://")
	parts := strings.Split(addr, ":")
	_, err := m.Import("N1", parts[0]+"|"+parts[1]+"|user|secret|2099-01-01")
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequestWithContext(WithTask(context.Background(), 1), "POST", "https://fccdn1.k4n.cc/no-business", nil)
	_, err = m.RoundTrip(req, http.DefaultTransport)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Fatal("proxy auth failure or redaction", err)
	}
	if got := <-auth; got != "Basic "+base64.StdEncoding.EncodeToString([]byte("user:secret")) {
		t.Fatal("missing CONNECT credentials")
	}
	if m.bindings[1]["N1"].Failures != 1 {
		t.Fatal("authentication failure not counted")
	}
}
