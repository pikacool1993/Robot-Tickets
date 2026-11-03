package main

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func expectAdmin(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT u.id FROM admin_sessions").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("admin"))
}

func TestAgentDetailDevices(t *testing.T) {
	for _, mode := range []string{"active", "empty", "disabled", "query-failure", "row-failure"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			s := &Server{db: db}
			expectAdmin(mock)
			mock.ExpectBegin()
			created := time.Date(2026, 11, 3, 1, 0, 0, 0, time.UTC)
			status := "enabled"
			if mode == "disabled" {
				status = "disabled"
			}
			mock.ExpectQuery("SELECT id,name,username,status,max_devices,last_login_at,last_login_ip,created_at FROM agents").WithArgs("agent").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "username", "status", "max_devices", "last_login_at", "last_login_ip", "created_at"}).AddRow("agent", "name", "user", status, 5, created, "127.0.0.1", created))
			query := mock.ExpectQuery("SELECT id,client_number,client_instance_id,created_at,expires_at,login_ip FROM agent_sessions WHERE agent_id=\\? AND revoked_at IS NULL AND expires_at>NOW\\(6\\) AND \\?='enabled'").WithArgs("agent", status)
			want := 200
			if mode == "query-failure" {
				query.WillReturnError(errors.New("unavailable"))
				mock.ExpectRollback()
				want = 500
			} else {
				rows := sqlmock.NewRows([]string{"id", "client_number", "client_instance_id", "created_at", "expires_at", "login_ip"})
				if mode == "active" || mode == "row-failure" {
					rows.AddRow("session", "client-001", "instance", created, created.Add(7*24*time.Hour), "127.0.0.1")
				}
				if mode == "row-failure" {
					rows.RowError(0, errors.New("read failure"))
					want = 500
				}
				query.WillReturnRows(rows)
				if want == 200 {
					mock.ExpectCommit()
				} else {
					mock.ExpectRollback()
				}
			}
			r := httptest.NewRequest("GET", "/api/admin/v1/agents/agent", nil)
			r.SetPathValue("id", "agent")
			w := httptest.NewRecorder()
			s.getAgent(w, r)
			if w.Code != want {
				t.Fatalf("got %d expected %d: %s", w.Code, want, w.Body.String())
			}
			if want == 200 {
				var out struct {
					Data struct {
						Created time.Time        `json:"created_at"`
						Active  int              `json:"active_devices"`
						Devices []map[string]any `json:"devices"`
					} `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
					t.Fatal(err)
				}
				count := 0
				if mode == "active" {
					count = 1
				}
				if !out.Data.Created.Equal(created) || out.Data.Active != count || len(out.Data.Devices) != count || out.Data.Devices == nil {
					t.Fatal("creation time/device count/list mismatch")
				}
				if count > 0 {
					if out.Data.Devices[0]["client_number"] != "client-001" {
						t.Fatal("missing terminal identity")
					}
					if _, ok := out.Data.Devices[0]["token_hash"]; ok {
						t.Fatal("session secret exposed")
					}
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAgentListActiveDevices(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Server{db: db}
	expectAdmin(mock)
	// The count must use the same active-session predicate as the detail list.
	mock.ExpectQuery("SELECT a.id.*SELECT COUNT\\(\\*\\) FROM agent_sessions x WHERE x.agent_id=a.id AND x.revoked_at IS NULL AND x.expires_at>NOW\\(6\\) AND a.status='enabled'").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "username", "status", "max", "last", "ip", "created", "active"}).AddRow("agent", "name", "user", "enabled", 5, nil, nil, time.Now(), 2))
	w := httptest.NewRecorder()
	s.listAgents(w, httptest.NewRequest("GET", "/api/admin/v1/agents", nil))
	var out struct {
		Data []struct {
			Active int `json:"active_devices"`
		} `json:"data"`
	}
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &out) != nil || len(out.Data) != 1 || out.Data[0].Active != 2 {
		t.Fatalf("invalid list: %s", w.Body.String())
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
