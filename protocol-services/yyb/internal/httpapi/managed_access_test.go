package httpapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestManagedConsoleAllowsDirectBrowserAccess(t *testing.T) {
	t.Setenv("GIN_MODE", "test")
	t.Setenv("ROBOT_PROTOCOL_TOKEN", "managed-token")
	app, err := NewApp(Config{ResourceRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	handler := app.Handler()
	// Managed local-agent startup points resource-root at a fresh data directory,
	// not the packaged templates folder. That must still serve the complete UI.
	index := httptest.NewRecorder()
	handler.ServeHTTP(index, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, text := range []string{"选择账号，执行服务调用", "账号队列", "调用配置", "系统入口", "repeat(auto-fit, minmax(238px, 1fr))"} {
		if !strings.Contains(index.Body.String(), text) {
			t.Errorf("embedded console missing %q", text)
		}
	}
	if strings.Contains(index.Body.String(), "资源模板未找到") {
		t.Fatal("reduced fallback console was served")
	}
	for _, path := range []string{"/", "/scan", "/accounts", "/health", "/openapi.json", "/docs/index.html"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8063"+path, nil))
		if w.Code != http.StatusOK {
			t.Errorf("direct browser GET %s = %d", path, w.Code)
		}
		if len(w.Result().Cookies()) != 0 {
			t.Errorf("%s unexpectedly requires a browser session", path)
		}
	}
	// Browser actions must reach their original handlers without a process token.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/accounts/refresh", nil))
	if w.Code == http.StatusUnauthorized {
		t.Fatal("console account operation rejected by managed authentication")
	}
	for _, token := range []string{"", "incorrect", "managed-token"} {
		r := httptest.NewRequest(http.MethodGet, "/robot/health", nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		want := http.StatusUnauthorized
		if token == "managed-token" {
			want = http.StatusOK
		}
		if w.Code != want {
			t.Errorf("internal health status %d, want %d", w.Code, want)
		}
	}
}
