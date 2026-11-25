package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"robot/apps/cloud-server/internal/auth"
)

func TestClientFeishuConfigEncryptedAndScopedToSession(t *testing.T) {
	t.Setenv("AGENT_TRANSPORT_KEY_ID", "client-v1")
	db, mock, _ := sqlmock.New()
	defer db.Close()
	key := bytes.Repeat([]byte{6}, 32)
	secretKey := bytes.Repeat([]byte{7}, 32)
	s := &Server{db: db, keys: auth.Keyring{"client-v1": key}, credentialKey: secretKey, credentialKeyVersion: "v1"}
	mock.ExpectQuery("SELECT a.id FROM agent_sessions").WithArgs(hashToken("session-token")).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("own-agent"))
	ct, nonce, _ := auth.SealSecret(secretKey, sampleHook)
	mock.ExpectQuery("SELECT enabled,order_created,auth_expired,order_blocked,webhook_ciphertext,webhook_nonce,webhook_key_version,revision FROM agent_feishu_configs").WithArgs("own-agent").WillReturnRows(sqlmock.NewRows([]string{"enabled", "created", "expired", "blocked", "ct", "nonce", "version", "revision"}).AddRow(true, true, true, true, ct, nonce, "v1", 2))
	r := httptest.NewRequest("GET", "/feishu-config?agent_id=other-agent", nil)
	r.Header.Set("Authorization", "Bearer session-token")
	w := httptest.NewRecorder()
	s.getClientFeishuConfig(w, r)
	if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte(sampleHook)) {
		t.Fatalf("unencrypted/failed response: %d", w.Code)
	}
	var env auth.Envelope
	if json.Unmarshal(w.Body.Bytes(), &env) != nil {
		t.Fatal("missing encrypted envelope")
	}
	raw, err := s.keys.Open(env)
	if err != nil || !bytes.Contains(raw, []byte(sampleHook)) {
		t.Fatal("client did not receive configured webhook", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

const sampleHook = "https://open.feishu.cn/open-apis/bot/v2/hook/test-hook-1234"

func TestFeishuConfigDefaultsAndAdminDoesNotRevealSecret(t *testing.T) {
	for _, configured := range []bool{false, true} {
		t.Run(map[bool]string{false: "defaults", true: "configured"}[configured], func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			key := bytes.Repeat([]byte{4}, 32)
			s := &Server{db: db, credentialKey: key, credentialKeyVersion: "v1"}
			expectBusinessAdmin(mock)
			mock.ExpectQuery("SELECT id FROM agents").WithArgs("agent").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent"))
			query := mock.ExpectQuery("SELECT enabled,order_created,auth_expired,order_blocked,webhook_ciphertext,webhook_nonce,webhook_key_version,revision FROM agent_feishu_configs").WithArgs("agent")
			if configured {
				ct, nonce, _ := auth.SealSecret(key, sampleHook)
				query.WillReturnRows(sqlmock.NewRows([]string{"enabled", "created", "expired", "blocked", "ct", "nonce", "version", "revision"}).AddRow(true, true, true, true, ct, nonce, "v1", 2))
			} else {
				query.WillReturnError(sql.ErrNoRows)
			}
			r := httptest.NewRequest("GET", "/", nil)
			r.SetPathValue("id", "agent")
			w := httptest.NewRecorder()
			s.getAgentFeishuConfig(w, r)
			if w.Code != 200 || bytes.Contains(w.Body.Bytes(), []byte("test-hook-1234")) {
				t.Fatalf("failed or leaked: %d %s", w.Code, w.Body)
			}
			if !bytes.Contains(w.Body.Bytes(), []byte(`"enabled":true`)) {
				t.Fatal("wrong defaults")
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestSaveFeishuConfigEncryptedAndRevisionConflict(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "conflict"}[conflict], func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			s := &Server{db: db, credentialKey: bytes.Repeat([]byte{4}, 32), credentialKeyVersion: "v1"}
			expectBusinessAdmin(mock)
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT id FROM agents").WithArgs("agent").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent"))
			query := mock.ExpectQuery("SELECT revision,webhook_ciphertext,webhook_nonce,webhook_key_version FROM agent_feishu_configs").WithArgs("agent")
			status := 200
			if conflict {
				query.WillReturnRows(sqlmock.NewRows([]string{"revision", "ct", "nonce", "version"}).AddRow(3, []byte{1}, []byte{2}, "v1"))
				mock.ExpectRollback()
				status = 409
			} else {
				query.WillReturnError(sql.ErrNoRows)
				mock.ExpectExec("INSERT INTO agent_feishu_configs").WithArgs("agent", true, true, true, true, sqlmock.AnyArg(), sqlmock.AnyArg(), "v1", 1).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("INSERT INTO audit_events").WithArgs("admin", "agent", sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			}
			raw, _ := json.Marshal(map[string]any{"enabled": true, "order_created": true, "auth_expired": true, "order_blocked": true, "webhook": sampleHook, "revision": 0})
			r := httptest.NewRequest("PUT", "/", bytes.NewReader(raw))
			r.SetPathValue("id", "agent")
			w := httptest.NewRecorder()
			s.saveAgentFeishuConfig(w, r)
			if w.Code != status {
				t.Fatalf("got %d %s", w.Code, w.Body)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestFeishuConfigRequiresAdmin(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := &Server{db: db}
	mock.ExpectQuery("SELECT u.id FROM admin_sessions").WithArgs(sqlmock.AnyArg()).WillReturnError(sql.ErrNoRows)
	w := httptest.NewRecorder()
	s.getAgentFeishuConfig(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 401 {
		t.Fatalf("unauthorized status %d", w.Code)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
