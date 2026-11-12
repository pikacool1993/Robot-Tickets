package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestBusinessTemplateValidation(t *testing.T) {
	for _, device := range businessDevices {
		raw, err := businessDefaults.ReadFile("business_templates/" + device + ".json")
		if err != nil {
			t.Fatal(err)
		}
		if _, err = parseBusinessTemplate(device, raw); err != nil {
			t.Fatal(err)
		}
		cases := []string{"dynamic-code", "device-id", "platform", "osName", "user-agent-device", "header", "referer", "empty-e", "empty-v", "unknown-field", "trailing-json", "oversize"}
		if device == "ios" {
			cases = append(cases, "session-id")
		}
		for _, name := range cases {
			t.Run(device+"/"+name, func(t *testing.T) {
				var obj map[string]any
				_ = json.Unmarshal(raw, &obj)
				switch name {
				case "dynamic-code":
					obj["code"] = "temporary-code"
				case "device-id":
					obj["s"].(map[string]any)["deviceId"] = "123"
				case "platform":
					obj["s"].(map[string]any)["platform"] = "mismatch"
				case "osName":
					obj["s"].(map[string]any)["osName"] = "mismatch"
				case "user-agent-device":
					obj["user_agent"] = "Mozilla/5.0 (OtherDevice)"
				case "header":
					obj["user_agent"] = "test\r\nInjected: true"
				case "referer":
					obj["referer"] = "https://example.com/page-frame.html"
				case "empty-e":
					obj["e"] = nil
				case "empty-v":
					obj["v"] = ""
				case "unknown-field":
					obj["url"] = "https://example.com"
				case "session-id":
					obj["e"].(map[string]any)["sessionId"] = "123"
				}
				candidate, _ := json.Marshal(obj)
				if name == "trailing-json" {
					candidate = append(candidate, []byte(` {}`)...)
				}
				if name == "oversize" {
					candidate = append(candidate, bytes.Repeat([]byte(" "), 65536)...)
				}
				if _, err := parseBusinessTemplate(device, candidate); err == nil {
					t.Fatal("accepted invalid template")
				}
			})
		}
	}
}

func expectBusinessAdmin(mock sqlmock.Sqlmock) {
	mock.ExpectQuery("SELECT u.id FROM admin_sessions").WithArgs(sqlmock.AnyArg()).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("admin"))
}

func TestSaveBusinessTemplateAtomic(t *testing.T) {
	for _, mode := range []string{"success", "conflict", "audit-failure", "invalid", "unauthorized"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			s := &Server{db: db}
			if mode == "unauthorized" {
				mock.ExpectQuery("SELECT u.id FROM admin_sessions").WillReturnRows(sqlmock.NewRows([]string{"id"}))
			} else {
				expectBusinessAdmin(mock)
			}
			raw, _ := businessDefaults.ReadFile("business_templates/windows.json")
			if mode == "invalid" {
				raw = []byte(`{}`)
			}
			body := jsonBytes(map[string]any{"revision": 2, "template": json.RawMessage(raw)})
			want := 200
			switch mode {
			case "invalid":
				want = 400
			case "unauthorized":
				want = 401
			default:
				mock.ExpectBegin()
				n := int64(1)
				if mode == "conflict" {
					n = 0
				}
				mock.ExpectExec("UPDATE business_device_templates").WithArgs(sqlmock.AnyArg(), "windows", uint64(2)).WillReturnResult(sqlmock.NewResult(0, n))
				if mode == "conflict" {
					want = 409
					mock.ExpectRollback()
				} else {
					audit := mock.ExpectExec("INSERT INTO audit_events").WithArgs("admin", sqlmock.AnyArg(), sqlmock.AnyArg())
					if mode == "audit-failure" {
						want = 500
						audit.WillReturnError(errors.New("audit failed"))
						mock.ExpectRollback()
					} else {
						audit.WillReturnResult(sqlmock.NewResult(1, 1))
						mock.ExpectCommit()
					}
				}
			}
			r := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(body))
			r.SetPathValue("device", "windows")
			w := httptest.NewRecorder()
			s.saveBusinessTemplate(w, r)
			if w.Code != want {
				t.Fatalf("%d: %s", w.Code, w.Body)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSaveAgentBusinessConfig(t *testing.T) {
	for _, mode := range []string{"create", "update", "conflict", "agent-missing", "template-invalid", "audit-failure"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, _ := sqlmock.New()
			defer db.Close()
			s := &Server{db: db}
			expectBusinessAdmin(mock)
			mock.ExpectBegin()
			agent := mock.ExpectQuery("SELECT id FROM agents WHERE id=\\? FOR UPDATE").WithArgs("agent")
			revision := uint64(0)
			if mode == "update" {
				revision = 3
			}
			want := 200
			if mode == "agent-missing" {
				agent.WillReturnError(sql.ErrNoRows)
				mock.ExpectRollback()
				want = 404
			} else {
				agent.WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("agent"))
				current := mock.ExpectQuery("SELECT revision FROM agent_business_configs").WithArgs("agent")
				if mode == "update" {
					current.WillReturnRows(sqlmock.NewRows([]string{"revision"}).AddRow(3))
				} else if mode == "conflict" {
					current.WillReturnRows(sqlmock.NewRows([]string{"revision"}).AddRow(1))
				} else {
					current.WillReturnError(sql.ErrNoRows)
				}
				if mode == "conflict" {
					want = 409
					mock.ExpectRollback()
				} else {
					raw, _ := businessDefaults.ReadFile("business_templates/ios.json")
					if mode == "template-invalid" {
						raw = []byte(`{}`)
					}
					mock.ExpectQuery("SELECT template FROM business_device_templates").WithArgs("ios").WillReturnRows(sqlmock.NewRows([]string{"template"}).AddRow(raw))
					if mode == "template-invalid" {
						want = 400
						mock.ExpectRollback()
					} else {
						if revision == 0 {
							mock.ExpectExec("INSERT INTO agent_business_configs").WithArgs("agent", "ios").WillReturnResult(sqlmock.NewResult(1, 1))
						} else {
							mock.ExpectExec("UPDATE agent_business_configs").WithArgs("ios", "agent").WillReturnResult(sqlmock.NewResult(0, 1))
						}
						audit := mock.ExpectExec("INSERT INTO audit_events").WithArgs("admin", "agent", sqlmock.AnyArg(), sqlmock.AnyArg())
						if mode == "audit-failure" {
							want = 500
							audit.WillReturnError(errors.New("audit failed"))
							mock.ExpectRollback()
						} else {
							audit.WillReturnResult(sqlmock.NewResult(1, 1))
							mock.ExpectCommit()
						}
					}
				}
			}
			r := httptest.NewRequest(http.MethodPut, "/", bytes.NewReader(jsonBytes(map[string]any{"device_type": "ios", "revision": revision})))
			r.SetPathValue("id", "agent")
			w := httptest.NewRecorder()
			s.saveAgentBusinessConfig(w, r)
			if w.Code != want {
				t.Fatalf("%d: %s", w.Code, w.Body)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSeedBusinessTemplatesPreservesEdits(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	for _, device := range businessDevices {
		mock.ExpectExec("INSERT IGNORE INTO business_device_templates").WithArgs(device, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 0))
	}
	if err := seedBusinessTemplates(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestBusinessConfigSnapshotChangesOnlyOnNextRead(t *testing.T) {
	db, mock, _ := sqlmock.New()
	defer db.Close()
	s := &Server{db: db}
	windows, _ := businessDefaults.ReadFile("business_templates/windows.json")
	ios, _ := businessDefaults.ReadFile("business_templates/ios.json")
	mock.ExpectQuery("SELECT c.device_type,c.revision,t.revision,t.template").WithArgs("agent").WillReturnRows(sqlmock.NewRows([]string{"device", "config_revision", "template_revision", "template"}).AddRow("windows", 1, 1, windows))
	first := s.businessConfig(t.Context(), "agent")
	mock.ExpectQuery("SELECT c.device_type,c.revision,t.revision,t.template").WithArgs("agent").WillReturnRows(sqlmock.NewRows([]string{"device", "config_revision", "template_revision", "template"}).AddRow("ios", 2, 3, ios))
	next := s.businessConfig(t.Context(), "agent")
	if !first.Ready || !next.Ready || first.DeviceType != "windows" || first.ConfigRevision != 1 || first.Template.S["platform"] != "windows" || next.DeviceType != "ios" || next.ConfigRevision != 2 || next.TemplateRevision != 3 {
		t.Fatalf("invalid snapshots: first=%+v next=%+v", first, next)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
