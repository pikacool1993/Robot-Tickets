package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"robot/apps/cloud-server/internal/auth"
)

func TestAdminViewerRoster(t *testing.T) {
	for _, mode := range []string{"all", "filtered", "confirmation", "audit-failure", "read-failure", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			s, mock := syncFixture(t)
			url := "/api/admin/v1/orders/viewer-roster?page=2&limit=50"
			body := `{"confirm":true}`
			want := 200
			if mode == "filtered" {
				url += "&agent_id=agent&client_number=A01&match_id=match"
			}
			if mode == "unauthorized" {
				mock.ExpectQuery("SELECT u.id FROM admin_sessions").WillReturnError(errors.New("expired"))
				want = 401
			} else {
				expectAdmin(mock)
			}
			if mode == "confirmation" {
				body = `{"confirm":false}`
				want = 400
			}
			if mode != "confirmation" && mode != "unauthorized" {
				cipher, nonce, err := auth.SealSecret(s.credentialKey, `[{"name":"同名观演人","phone":"13800000000"}]`)
				if err != nil {
					t.Fatal(err)
				}
				where := ""
				if mode == "filtered" {
					where = " WHERE agent_id=? AND client_number=? AND match_id=?"
				}
				q := mock.ExpectQuery(regexp.QuoteMeta("SELECT viewer_ciphertext,viewer_nonce,viewer_key_version FROM agent_orders" + where + " ORDER BY created_at DESC,id DESC"))
				if mode == "filtered" {
					q.WithArgs("agent", "A01", "match")
				} else {
					q.WithArgs()
				}
				rows := sqlmock.NewRows([]string{"ciphertext", "nonce", "version"})
				for i := 0; i < 205; i++ {
					rows.AddRow(cipher, nonce, "v1")
				}
				if mode == "read-failure" {
					rows.RowError(204, errors.New("read failed"))
					want = 500
				}
				q.WillReturnRows(rows).RowsWillBeClosed()
				if mode != "read-failure" {
					audit := mock.ExpectExec("INSERT INTO audit_events.*order_viewer_roster_viewed").WithArgs("admin", sqlmock.AnyArg(), sqlmock.AnyArg())
					if mode == "audit-failure" {
						audit.WillReturnError(errors.New("audit failed"))
						want = 500
					} else {
						audit.WillReturnResult(sqlmock.NewResult(1, 1))
					}
				}
			}
			w := httptest.NewRecorder()
			s.adminViewerRoster(w, httptest.NewRequest("POST", url, bytes.NewBufferString(body)))
			if w.Code != want {
				t.Fatalf("status=%d: %s", w.Code, w.Body)
			}
			if want == 200 {
				var result struct {
					Data struct {
						Names  []string `json:"names"`
						Orders int      `json:"order_count"`
					} `json:"data"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if len(result.Data.Names) != 205 || result.Data.Orders != 205 {
					t.Fatal("roster truncated or deduplicated")
				}
				if bytes.Contains(w.Body.Bytes(), []byte("13800000000")) {
					t.Fatal("roster leaked phone")
				}
				if w.Header().Get("Cache-Control") != "no-store" {
					t.Fatal("missing no-store")
				}
			} else if bytes.Contains(w.Body.Bytes(), []byte("同名观演人")) {
				t.Fatal("names returned without successful audit")
			}
		})
	}
}

func TestAdminOrdersMatchIDAndPagination(t *testing.T) {
	s, mock := syncFixture(t)
	expectAdmin(mock)
	mock.ExpectQuery(regexp.QuoteMeta("SELECT COUNT(*) FROM agent_orders WHERE agent_id=? AND client_number=? AND match_id=?")).WithArgs("agent", "A01", "match").WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(250))
	created := time.Date(2026, 11, 21, 6, 0, 0, 0, time.UTC)
	ciphertext, nonce, err := auth.SealSecret(s.credentialKey, `[{"name":"模拟用户","phone":"13800000000"}]`)
	if err != nil {
		t.Fatal(err)
	}
	rows := sqlmock.NewRows([]string{"id", "agent_name", "client_number", "client_instance_id", "task_id", "order_id", "match_id", "match_name", "ticket_count", "tiers", "seats", "created_at", "payment_deadline", "detail_status", "simulated", "viewer_ciphertext", "viewer_nonce", "viewer_key_version", "received_at"}).AddRow(8, "owner", "A01", "instance", 1, "SIM-123", "match", "example", 1, []byte(`["A区"]`), []byte(`[]`), created, nil, "complete", true, ciphertext, nonce, "v1", created)
	mock.ExpectQuery("SELECT id,agent_name,client_number.*WHERE agent_id=\\? AND client_number=\\? AND match_id=\\?.*LIMIT \\? OFFSET \\?").WithArgs("agent", "A01", "match", 200, 200).WillReturnRows(rows)
	w := httptest.NewRecorder()
	s.listAllAdminOrders(w, httptest.NewRequest("GET", "/api/admin/v1/orders?agent_id=agent&client_number=A01&match_id=match&page=2&limit=200", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d %s", w.Code, w.Body)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"simulated":true`)) {
		t.Fatalf("admin order response omitted simulated marker: %s", w.Body)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte(`"name":"模拟用户","phone":"138****0000"`)) {
		t.Fatalf("expected full name with masked phone: %s", w.Body)
	}
}

func TestAdminOrderRevealWithoutAudit(t *testing.T) {
	s, mock := syncFixture(t)
	expectAdmin(mock)
	ciphertext, nonce, err := auth.SealSecret(s.credentialKey, `[{"name":"示例用户","phone":"13800000000"}]`)
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectQuery(regexp.QuoteMeta("SELECT order_id,viewer_ciphertext,viewer_nonce,viewer_key_version FROM agent_orders WHERE id=?")).
		WithArgs("8").WillReturnRows(sqlmock.NewRows([]string{"order_id", "viewer_ciphertext", "viewer_nonce", "viewer_key_version"}).AddRow("SIM-123", ciphertext, nonce, "v1"))
	r := httptest.NewRequest("POST", "/api/admin/v1/orders/8/reveal-viewers", strings.NewReader(`{"confirm":true}`))
	r.SetPathValue("order", "8")
	w := httptest.NewRecorder()
	s.revealAllAdminOrderViewers(w, r)
	if w.Code != 200 || !bytes.Contains(w.Body.Bytes(), []byte(`"phone":"13800000000"`)) {
		t.Fatalf("expected full phone without audit write: %d %s", w.Code, w.Body)
	}
}
