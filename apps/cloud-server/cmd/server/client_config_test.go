package main

import (
	"bytes"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"robot/apps/cloud-server/internal/auth"
	"robot/apps/cloud-server/internal/clientconfig"
)

func TestExportClientConfig(t *testing.T) {
	t.Setenv("AGENT_TRANSPORT_KEY_ID", "client-v1")
	private := ed25519.NewKeyFromSeed(bytes.Repeat([]byte{1}, 32))
	fileKey := bytes.Repeat([]byte{2}, 32)
	transport := bytes.Repeat([]byte{3}, 32)
	for _, tc := range []struct {
		name                         string
		authorized, enabled, auditOK bool
		body                         string
		status                       int
	}{
		{"success", true, true, true, "{}", 200},
		{"unauthorized", false, true, true, "{}", 401},
		{"unconfigured", true, false, true, "{}", 503},
		{"malformed request", true, true, true, "{", 400},
		{"audit failure", true, true, false, "{}", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s := &Server{db: db, keys: auth.Keyring{"client-v1": transport}}
			if tc.enabled {
				s.configExport = &configExportSettings{clientconfig.Exporter{EncryptionKeyID: "enc-v1", EncryptionKey: fileKey, SigningKeyID: "sign-v1", SigningKey: private}, "http://example.test:8080"}
			}
			query := mock.ExpectQuery("SELECT u.id FROM admin_sessions").WithArgs(hashToken("test-token"))
			if tc.authorized {
				query.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("admin-id"))
			} else {
				query.WillReturnError(sql.ErrNoRows)
			}
			if tc.status == 200 || tc.name == "audit failure" {
				exec := mock.ExpectExec("INSERT INTO audit_events").WithArgs("admin-id", sqlmock.AnyArg(), sqlmock.AnyArg())
				if tc.auditOK {
					exec.WillReturnResult(sqlmock.NewResult(1, 1))
				} else {
					exec.WillReturnError(errors.New("database unavailable"))
				}
			}
			r := httptest.NewRequest(http.MethodPost, "/api/admin/v1/client-config/export", strings.NewReader(tc.body))
			r.Header.Set("Authorization", "Bearer test-token")
			w := httptest.NewRecorder()
			s.exportClientConfig(w, r)
			if w.Code != tc.status {
				t.Fatalf("status %d, expected %d", w.Code, tc.status)
			}
			if tc.status == 200 {
				c, err := clientconfig.Open(w.Body.Bytes(), map[string][]byte{"enc-v1": fileKey}, map[string]ed25519.PublicKey{"sign-v1": private.Public().(ed25519.PublicKey)})
				if err != nil || c.TransportKey != base64.StdEncoding.EncodeToString(transport) || c.ServerURL != "http://example.test:8080" {
					t.Fatalf("invalid exported config: %v", err)
				}
				if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Disposition") != `attachment; filename="bootstrap.cfg"` {
					t.Fatal("missing download headers")
				}
			} else if bytes.HasPrefix(w.Body.Bytes(), []byte(clientconfig.Magic)) {
				t.Fatal("file disclosed on failure")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLoadConfigExport(t *testing.T) {
	for _, name := range []string{"CLIENT_CONFIG_SERVER_URL", "CLIENT_CONFIG_ENCRYPTION_KEY_ID", "CLIENT_CONFIG_ENCRYPTION_KEY", "CLIENT_CONFIG_SIGNING_KEY_ID", "CLIENT_CONFIG_SIGNING_SEED"} {
		t.Setenv(name, "")
	}
	if settings, err := loadConfigExport(nil, nil); err != nil || settings != nil {
		t.Fatal("empty configuration must disable export")
	}
	t.Setenv("CLIENT_CONFIG_SERVER_URL", "http://example.test:8080/")
	if _, err := loadConfigExport(nil, nil); err == nil {
		t.Fatal("partial settings accepted")
	}
	key := bytes.Repeat([]byte{2}, 32)
	t.Setenv("CLIENT_CONFIG_ENCRYPTION_KEY_ID", "enc-v1")
	t.Setenv("CLIENT_CONFIG_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(key))
	t.Setenv("CLIENT_CONFIG_SIGNING_KEY_ID", "sign-v1")
	t.Setenv("CLIENT_CONFIG_SIGNING_SEED", base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)))
	if _, err := loadConfigExport(map[string][]byte{"client-v1": key}, nil); err == nil {
		t.Fatal("reused transport key accepted")
	}
	if _, err := loadConfigExport(nil, key); err == nil {
		t.Fatal("reused credential key accepted")
	}
	settings, err := loadConfigExport(nil, nil)
	if err != nil || settings == nil || settings.serverURL != "http://example.test:8080" {
		t.Fatalf("valid settings rejected: %v", err)
	}
	t.Setenv("CLIENT_CONFIG_SIGNING_SEED", "bad")
	if _, err := loadConfigExport(nil, nil); err == nil {
		t.Fatal("invalid signing seed accepted")
	}
}
