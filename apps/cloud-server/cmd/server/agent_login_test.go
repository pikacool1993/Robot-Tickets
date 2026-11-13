package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"robot/apps/cloud-server/internal/auth"
)

func TestAgentNumberSwitchIsAtomic(t *testing.T) {
	t.Setenv("AGENT_TRANSPORT_KEY_ID", "client-v1")
	for _, mode := range []string{"success", "config-missing", "config-invalid", "config-db-failure", "same-agent-other-instance", "device-limit", "audit-failure"} {
		t.Run(mode, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			key := bytes.Repeat([]byte{4}, 32)
			credentialKey := bytes.Repeat([]byte{5}, 32)
			ct, nonce, err := auth.SealSecret(credentialKey, "password")
			if err != nil {
				t.Fatal(err)
			}
			s := &Server{db: db, keys: auth.Keyring{"client-v1": key}, credentialKey: credentialKey, credentialKeyVersion: "v1"}
			id := "11111111-1111-4111-8111-111111111111"
			instance := "22222222-2222-4222-8222-222222222222"
			mock.ExpectQuery("SELECT id,name,password_ciphertext").WithArgs("user").WillReturnRows(sqlmock.NewRows([]string{"id", "name", "ct", "nonce", "version", "status", "max"}).AddRow(id, "name", ct, nonce, "v1", "enabled", 1))
			mock.ExpectBegin()
			mock.ExpectQuery("SELECT status,max_devices").WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"status", "max"}).AddRow("enabled", 1))
			mock.ExpectQuery("SELECT id FROM client_identity_lock").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1))
			query := mock.ExpectQuery("SELECT agent_id,client_instance_id FROM client_instance_bindings").WithArgs("B")
			want := 200
			if mode == "same-agent-other-instance" {
				query.WillReturnRows(sqlmock.NewRows([]string{"agent", "instance"}).AddRow(id, "33333333-3333-4333-8333-333333333333"))
				mock.ExpectRollback()
				want = 409
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"agent", "instance"}))
				mock.ExpectQuery("SELECT agent_id FROM client_registrations").WithArgs("B").WillReturnRows(sqlmock.NewRows([]string{"agent"}))
				mock.ExpectExec("INSERT INTO client_registrations").WithArgs("B", id).WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectQuery("SELECT client_number FROM client_instance_bindings").WithArgs(instance).WillReturnRows(sqlmock.NewRows([]string{"number"}).AddRow("A"))
				mock.ExpectExec("DELETE FROM agent_sessions WHERE client_number").WithArgs("A").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("DELETE FROM client_instance_bindings").WithArgs(instance).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("DELETE FROM client_registrations").WithArgs("A").WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("INSERT INTO client_instance_bindings").WithArgs(instance, id, "B").WillReturnResult(sqlmock.NewResult(1, 1))
				mock.ExpectExec("UPDATE agent_sessions SET revoked_at").WithArgs(id, instance).WillReturnResult(sqlmock.NewResult(0, 1))
				mock.ExpectExec("DELETE FROM agent_sessions WHERE agent_id").WithArgs(id).WillReturnResult(sqlmock.NewResult(0, 0))
				count := 0
				if mode == "device-limit" {
					count = 1
				}
				mock.ExpectQuery("SELECT count").WithArgs(id).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(count))
				if mode == "device-limit" {
					mock.ExpectRollback()
					want = 409
				} else {
					mock.ExpectExec("INSERT INTO agent_sessions").WithArgs(sqlmock.AnyArg(), id, instance, "B", sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg(), sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(1, 1))
					mock.ExpectExec("UPDATE agents SET last_login_at").WithArgs(sqlmock.AnyArg(), id).WillReturnResult(sqlmock.NewResult(0, 1))
					audit := mock.ExpectExec("INSERT INTO audit_events").WithArgs(id, sqlmock.AnyArg(), "B", instance, sqlmock.AnyArg())
					if mode == "audit-failure" {
						audit.WillReturnError(http.ErrAbortHandler)
						mock.ExpectRollback()
						want = 500
					} else {
						audit.WillReturnResult(sqlmock.NewResult(1, 1))
						mock.ExpectCommit()
						mock.ExpectQuery("SELECT config,revision FROM current_match").WillReturnRows(sqlmock.NewRows([]string{"config", "revision"}))
						config := mock.ExpectQuery("SELECT c.device_type,c.revision,t.revision,t.template").WithArgs(id)
						raw, _ := businessDefaults.ReadFile("business_templates/windows.json")
						switch mode {
						case "config-missing":
							config.WillReturnRows(sqlmock.NewRows([]string{"device", "config_revision", "template_revision", "template"}).AddRow(nil, nil, nil, nil))
						case "config-invalid":
							config.WillReturnRows(sqlmock.NewRows([]string{"device", "config_revision", "template_revision", "template"}).AddRow("ios", 1, 1, `{}`))
						case "config-db-failure":
							config.WillReturnError(http.ErrAbortHandler)
						default:
							config.WillReturnRows(sqlmock.NewRows([]string{"device", "config_revision", "template_revision", "template"}).AddRow("windows", 1, 2, raw))
						}
					}
				}
			}
			plain, _ := json.Marshal(map[string]string{"username": "user", "password": "password", "client_number": "B", "client_instance_id": instance})
			env, _ := s.keys.Seal("client-v1", plain)
			body, _ := json.Marshal(env)
			r := httptest.NewRequest("POST", "/api/agent/v1/auth/login", bytes.NewReader(body))
			w := httptest.NewRecorder()
			s.agentLogin(w, r)
			if w.Code != want {
				t.Fatalf("status %d expected %d", w.Code, want)
			}
			var response auth.Envelope
			if json.Unmarshal(w.Body.Bytes(), &response) != nil {
				t.Fatal("response not encrypted")
			}
			decoded, err := s.keys.Open(response)
			if err != nil {
				t.Fatal(err)
			}
			if want == 200 {
				var result struct {
					Data struct {
						Config businessSnapshot `json:"business_config"`
					} `json:"data"`
				}
				if err := json.Unmarshal(decoded, &result); err != nil {
					t.Fatal(err)
				}
				c := result.Data.Config
				if mode == "success" {
					if !c.Ready || c.Template == nil || c.TemplateRevision != 2 || c.DeviceType != "windows" {
						t.Fatalf("missing ready snapshot: %+v", c)
					}
				} else if c.Ready || c.ErrorCode == "" || c.Template != nil {
					t.Fatalf("unsafe error snapshot: %+v", c)
				}
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
