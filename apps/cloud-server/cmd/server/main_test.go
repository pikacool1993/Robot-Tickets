package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
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

func TestEnsureSimulatedOrderColumn(t *testing.T) {
	for _, tc := range []struct {
		name  string
		count int
	}{
		{name: "already exists", count: 1},
		{name: "upgrade existing database", count: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			mock.ExpectQuery("SELECT COUNT\\(\\*\\) FROM information_schema\\.COLUMNS").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(tc.count))
			if tc.count == 0 {
				mock.ExpectExec("ALTER TABLE agent_orders ADD COLUMN simulated BOOLEAN").WillReturnResult(sqlmock.NewResult(0, 0))
			}
			if err := ensureSimulatedOrderColumn(t.Context(), db); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
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
		{path: "/", code: http.StatusNotFound},
		{path: "/admin/login", want: "index", code: http.StatusOK},
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

func TestEnsureOrderIPColumn(t *testing.T) {
	for _, count := range []int{0, 1} {
		db, mock, err := sqlmock.New()
		if err != nil {
			t.Fatal(err)
		}
		mock.ExpectQuery("SELECT COUNT").WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
		if count == 0 {
			mock.ExpectExec("ALTER TABLE agent_orders ADD COLUMN order_ip").WillReturnResult(sqlmock.NewResult(0, 0))
		}
		if err := ensureOrderIPColumn(t.Context(), db); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
		db.Close()
	}
}
