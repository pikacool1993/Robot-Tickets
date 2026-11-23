package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"robot/apps/cloud-server/internal/auth"
)

func syncFixture(t *testing.T) (*Server, sqlmock.Sqlmock) {
	t.Helper()
	t.Setenv("AGENT_TRANSPORT_KEY_ID", "client-v1")
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Error(err)
		}
		db.Close()
	})
	return &Server{db: db, keys: auth.Keyring{"client-v1": bytes.Repeat([]byte{1}, 32)}, credentialKey: bytes.Repeat([]byte{2}, 32), credentialKeyVersion: "v1"}, mock
}

func TestTaskSyncRejectsStaleSnapshot(t *testing.T) {
	s, mock := syncFixture(t)
	mock.ExpectQuery("SELECT a.id FROM agent_sessions").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent"))
	mock.ExpectQuery("SELECT x.client_number,x.client_instance_id FROM agent_sessions").WithArgs(hashToken("token"), "agent").WillReturnRows(sqlmock.NewRows([]string{"number", "instance"}).AddRow("A01", "instance"))
	mock.ExpectBegin()
	mock.ExpectExec("INSERT INTO agent_task_sync_state").WithArgs("agent", "A01", "instance").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT revision FROM agent_task_sync_state").WithArgs("agent", "instance").WillReturnRows(sqlmock.NewRows([]string{"revision"}).AddRow(4))
	mock.ExpectRollback()
	env, err := s.keys.Seal("client-v1", []byte(`{"revision":3,"tasks":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("PUT", "/api/agent/v1/sync/tasks", bytes.NewReader(jsonBytes(env)))
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	s.syncTasks(w, r)
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &env) != nil {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
	}
	plain, err := s.keys.Open(env)
	if err != nil || !bytes.Contains(plain, []byte(`"applied":false`)) || !bytes.Contains(plain, []byte(`"revision":4`)) {
		t.Fatalf("unexpected response: %s %v", plain, err)
	}
}

func TestOrderRetryPreservesRecordAndUsesSessionOwner(t *testing.T) {
	s, mock := syncFixture(t)
	mock.ExpectQuery("SELECT a.id FROM agent_sessions").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent"))
	mock.ExpectQuery("SELECT x.client_number,x.client_instance_id,a.name FROM agent_sessions").WithArgs(hashToken("token"), "agent").WillReturnRows(sqlmock.NewRows([]string{"number", "instance", "name"}).AddRow("A01", "instance", "owner"))
	at := time.Date(2026, 11, 21, 6, 0, 0, 0, time.UTC)
	mock.ExpectExec("INSERT INTO agent_orders.*ON DUPLICATE KEY UPDATE id=id").
		WithArgs("agent", "owner", "A01", "instance", int64(1), "123", "match", "example", 1, "[]", "[]", at, nil, "unavailable", true, sqlmock.AnyArg(), sqlmock.AnyArg(), "v1").
		WillReturnResult(sqlmock.NewResult(0, 0))
	env, err := s.keys.Seal("client-v1", []byte(`{"order_id":"123","task_id":1,"match_id":"match","match_name":"example","ticket_count":1,"created_at":"2026-11-21T14:00:00+08:00","detail_status":"unavailable","simulated":true,"agent_id":"other","client_number":"A99"}`))
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("POST", "/api/agent/v1/orders", bytes.NewReader(jsonBytes(env)))
	r.Header.Set("Authorization", "Bearer token")
	w := httptest.NewRecorder()
	s.syncOrder(w, r)
	if w.Code != 200 {
		t.Fatalf("unexpected response: %d %s", w.Code, w.Body)
	}
}
