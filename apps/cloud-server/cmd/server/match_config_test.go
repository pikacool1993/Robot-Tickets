package main

import (
	"bytes"
	"encoding/json"
	"github.com/DATA-DOG/go-sqlmock"
	"net/http/httptest"
	"robot/apps/cloud-server/internal/auth"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"testing"
	"time"
)

func TestMatchConfigOptimisticSave(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "save", true: "conflict"}[conflict], func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			s := &Server{db: db}
			mock.ExpectQuery("SELECT u.id FROM admin_sessions").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("admin"))
			mock.ExpectBegin()
			count := int64(1)
			if conflict {
				count = 0
			}
			mock.ExpectExec("INSERT IGNORE INTO current_match").WillReturnResult(sqlmock.NewResult(1, count))
			if conflict {
				mock.ExpectRollback()
			} else {
				mock.ExpectExec("INSERT INTO audit_events").WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectCommit()
			}
			m := protocol.MatchConfig{ID: "101", Name: "测试比赛", SaleAt: time.Now().Add(time.Hour), MatchAt: time.Now().Add(49 * time.Hour), Enabled: true}
			raw, _ := json.Marshal(m)
			w := httptest.NewRecorder()
			s.saveCurrentMatch(w, httptest.NewRequest("PUT", "/", bytes.NewReader(raw)))
			want := 200
			if conflict {
				want = 409
			}
			if w.Code != want {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestTimeEndpointEncryptedAndAuthenticated(t *testing.T) {
	t.Setenv("AGENT_TRANSPORT_KEY_ID", "client-v1")
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := &Server{db: db, keys: auth.Keyring{"client-v1": bytes.Repeat([]byte{1}, 32)}}
	mock.ExpectQuery("SELECT a.id FROM agent_sessions").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent-id"))
	w := httptest.NewRecorder()
	s.serverTime(w, httptest.NewRequest("GET", "/", nil))
	var env auth.Envelope
	if json.Unmarshal(w.Body.Bytes(), &env) != nil {
		t.Fatal("not encrypted")
	}
	raw, err := s.keys.Open(env)
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Data struct {
			MS int64 `json:"unix_ms"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &out) != nil || time.Since(time.UnixMilli(out.Data.MS)).Abs() > time.Second {
		t.Fatalf("invalid time %s", raw)
	}
	mock.ExpectQuery("SELECT a.id FROM agent_sessions").WillReturnRows(sqlmock.NewRows([]string{"id"}))
	w = httptest.NewRecorder()
	s.serverTime(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 401 {
		t.Fatal("unauthorized allowed")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
