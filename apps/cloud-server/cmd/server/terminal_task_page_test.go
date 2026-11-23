package main

import (
	"database/sql"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTerminalTaskPage(t *testing.T) {
	for _, mode := range []string{"page", "search", "unsynced", "invalid-status", "invalid-limit", "invalid-query"} {
		t.Run(mode, func(t *testing.T) {
			s, mock := syncFixture(t)
			expectAdmin(mock)
			query := "?page=2&limit=60"
			if mode == "search" {
				query = "?page=1&limit=120&status=expired&q=12"
			}
			if mode == "invalid-status" {
				query += "&status=invalid"
			}
			if mode == "invalid-limit" {
				query = "?page=1&limit=500"
			}
			if mode == "invalid-query" {
				query += "&q=%25"
			}
			want := 200
			if mode == "invalid-status" || mode == "invalid-limit" || mode == "invalid-query" {
				want = 400
			} else {
				state := mock.ExpectQuery("SELECT tasks_synced_at FROM agent_task_sync_state").WithArgs("agent", "instance")
				if mode == "unsynced" {
					state.WillReturnError(sql.ErrNoRows)
				} else {
					at := time.Date(2026, 11, 23, 0, 0, 0, 0, time.UTC)
					state.WillReturnRows(sqlmock.NewRows([]string{"tasks_synced_at"}).AddRow(at))
					mock.ExpectQuery(regexp.QuoteMeta("SELECT authorization_status,COUNT(*) FROM agent_terminal_tasks WHERE agent_id=? AND client_instance_id=? GROUP BY authorization_status")).WithArgs("agent", "instance").WillReturnRows(sqlmock.NewRows([]string{"status", "count"}).AddRow("online", 200).AddRow("expired", 168))
					where := " WHERE agent_id=? AND client_instance_id=?"
					if mode == "search" {
						where += " AND authorization_status=? AND CAST(task_id AS CHAR) LIKE ?"
					}
					count := mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM agent_terminal_tasks" + where))
					tasks := mock.ExpectQuery(regexp.QuoteMeta("SELECT task_id,authorization_status,updated_at FROM agent_terminal_tasks" + where + " ORDER BY task_id LIMIT ? OFFSET ?"))
					if mode == "search" {
						count.WithArgs("agent", "instance", "expired", "%12%")
						tasks.WithArgs("agent", "instance", "expired", "%12%", 120, 0)
					} else {
						count.WithArgs("agent", "instance")
						tasks.WithArgs("agent", "instance", 60, 60)
					}
					count.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(168))
					tasks.WillReturnRows(sqlmock.NewRows([]string{"task_id", "authorization_status", "updated_at"}).AddRow(112, "expired", at))
				}
			}
			r := httptest.NewRequest("GET", "/api/admin/v1/agents/agent/terminals/instance/tasks"+query, nil)
			r.SetPathValue("id", "agent")
			r.SetPathValue("instance", "instance")
			w := httptest.NewRecorder()
			s.getTerminalTasks(w, r)
			if w.Code != want {
				t.Fatalf("%d: %s", w.Code, w.Body)
			}
			if want == 200 && mode != "unsynced" {
				var result struct {
					Data struct {
						Total  int                `json:"total"`
						Counts map[string]int     `json:"counts"`
						Tasks  []taskSnapshotItem `json:"tasks"`
					} `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Data.Total != 168 || result.Data.Counts["online"] != 200 || len(result.Data.Tasks) != 1 {
					t.Fatalf("bad pagination: %s", w.Body)
				}
			}
		})
	}
}
