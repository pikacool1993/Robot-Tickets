package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestTokenHashMatchesBearerToken(t *testing.T) {
	raw := bytes.Repeat([]byte{0x5a}, 32)
	token, storedHash, err := tokenFromRandom(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got := hashToken(token); !bytes.Equal(got, storedHash) {
		t.Fatal("stored token hash does not match hash used during bearer authentication")
	}
}

func TestAdminWebHandlerServesAssetsAndSPA(t *testing.T) {
	webDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(webDir, "index.html"), []byte("index"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(webDir, "assets"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(webDir, "assets", "app.js"), []byte("asset"), 0600); err != nil {
		t.Fatal(err)
	}

	handler := adminWebHandler(webDir)
	for _, tc := range []struct {
		path string
		want string
		code int
	}{
		{path: "/", want: "index", code: http.StatusOK},
		{path: "/agents", want: "index", code: http.StatusOK},
		{path: "/assets/app.js", want: "asset", code: http.StatusOK},
		{path: "/api/unknown", code: http.StatusNotFound},
	} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.path, nil))
		if rec.Code != tc.code {
			t.Errorf("GET %s returned %d, want %d", tc.path, rec.Code, tc.code)
		}
		if tc.want != "" && rec.Body.String() != tc.want {
			t.Errorf("GET %s body = %q, want %q", tc.path, rec.Body.String(), tc.want)
		}
	}
}
