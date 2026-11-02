package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"robot/apps/cloud-server/internal/auth"
)

type payload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data"`
}

func (s *Server) request(w http.ResponseWriter, r *http.Request, keyID string, out any) bool {
	if err := s.decrypt(r, keyID, out); err != nil {
		s.failure(w, http.StatusBadRequest, keyID, "invalid_envelope", "Invalid encrypted request")
		return false
	}
	return true
}
func (s *Server) adminLogin(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !s.request(w, r, s.adminKeyID, &in) {
		return
	}
	var id, hash, status string
	err := s.db.QueryRowContext(r.Context(), "SELECT id,password_hash,status FROM admin_users WHERE username=?", strings.ToLower(strings.TrimSpace(in.Username))).Scan(&id, &hash, &status)
	if err != nil || status != "enabled" || bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)) != nil {
		s.audit(r.Context(), "admin", nil, "admin_login_failed", clientIP(r), "failure", nil)
		s.failure(w, 401, s.adminKeyID, "invalid_credentials", "Invalid credentials")
		return
	}
	token, tokenHash, err := randomToken()
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	expires, err := writeSession(r.Context(), s.db, "admin_sessions", "admin_user_id", id, token, tokenHash, clientIP(r))
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	s.audit(r.Context(), "admin", nil, "admin_login_succeeded", clientIP(r), "success", map[string]any{"admin_id": id})
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"access_token": token, "token_type": "Bearer", "expires_at": expires, "admin": map[string]string{"id": id, "username": strings.ToLower(in.Username)}}})
}
func bearer(r *http.Request) string {
	a := strings.SplitN(r.Header.Get("Authorization"), " ", 2)
	if len(a) != 2 || !strings.EqualFold(a[0], "Bearer") {
		return ""
	}
	return a[1]
}
func (s *Server) adminID(r *http.Request) (string, bool) {
	var id string
	err := s.db.QueryRowContext(r.Context(), "SELECT u.id FROM admin_sessions x JOIN admin_users u ON u.id=x.admin_user_id WHERE x.token_hash=? AND x.revoked_at IS NULL AND x.expires_at>NOW(6) AND u.status='enabled'", hashToken(bearer(r))).Scan(&id)
	return id, err == nil
}
func (s *Server) agentID(r *http.Request) (string, bool) {
	var id string
	err := s.db.QueryRowContext(r.Context(), "SELECT a.id FROM agent_sessions x JOIN agents a ON a.id=x.agent_id WHERE x.token_hash=? AND x.revoked_at IS NULL AND x.expires_at>NOW(6) AND a.status='enabled'", hashToken(bearer(r))).Scan(&id)
	return id, err == nil
}
func (s *Server) adminKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := s.adminID(r)
	if !ok {
		s.failure(w, 401, s.adminKeyID, "unauthorized", "Authentication required")
	}
	return id, ok
}
func (s *Server) agentKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	id, ok := s.agentID(r)
	if !ok {
		s.failure(w, 401, agentKeyID(), "unauthorized", "Authentication required")
	}
	return id, ok
}
func agentKeyID() string { return strings.TrimSpace(os.Getenv("AGENT_TRANSPORT_KEY_ID")) }
func (s *Server) adminLogout(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	_, _ = s.db.ExecContext(r.Context(), "UPDATE admin_sessions SET revoked_at=now() WHERE token_hash=?", hashToken(bearer(r)))
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", nil})
}
func (s *Server) adminMe(w http.ResponseWriter, r *http.Request) {
	id, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var username string
	_ = s.db.QueryRowContext(r.Context(), "SELECT username FROM admin_users WHERE id=?", id).Scan(&username)
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", map[string]string{"id": id, "username": username}})
}
func (s *Server) createAgent(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var in struct {
		Name       string `json:"name"`
		Username   string `json:"username"`
		MaxDevices int    `json:"max_devices"`
	}
	if !s.request(w, r, s.adminKeyID, &in) {
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	if in.Name == "" || len(in.Name) > 200 || !validUsername(in.Username) || in.MaxDevices < 1 || in.MaxDevices > 10000 {
		s.failure(w, 400, s.adminKeyID, "invalid_input", "Invalid agent fields")
		return
	}
	secret, err := randomPassword()
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	ciphertext, nonce, err := auth.SealSecret(s.credentialKey, secret)
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	var id string
	id = newID()
	_, err = s.db.ExecContext(r.Context(), "INSERT INTO agents(id,name,username,password_ciphertext,password_nonce,password_key_version,max_devices,created_by) VALUES(?,?,?,?,?,?,?,?)", id, in.Name, in.Username, ciphertext, nonce, s.credentialKeyVersion, in.MaxDevices, admin)
	if err != nil {
		s.failure(w, 409, s.adminKeyID, "username_conflict", "Username already exists")
		return
	}
	s.audit(r.Context(), "admin", &id, "agent_created", clientIP(r), "success", map[string]any{"username": in.Username, "max_devices": in.MaxDevices})
	s.respond(w, 201, s.adminKeyID, payload{"ok", "", map[string]any{"id": id, "name": in.Name, "username": in.Username, "max_devices": in.MaxDevices, "password": secret}})
}
func (s *Server) listAgents(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	rows, err := s.db.QueryContext(r.Context(), "SELECT id,name,username,status,max_devices,last_login_at,last_login_ip,created_at FROM agents ORDER BY created_at DESC LIMIT 200")
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id, name, user, status string
		var max int
		var last *time.Time
		var ip *string
		var created time.Time
		if rows.Scan(&id, &name, &user, &status, &max, &last, &ip, &created) == nil {
			items = append(items, map[string]any{"id": id, "name": name, "username": user, "status": status, "max_devices": max, "last_login_at": last, "last_login_ip": ip, "created_at": created})
		}
	}
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", items})
}
func (s *Server) getAgent(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	var out map[string]any = map[string]any{}
	var id, name, user, status string
	var max int
	var last *time.Time
	var ip *string
	err := s.db.QueryRowContext(r.Context(), "SELECT id,name,username,status,max_devices,last_login_at,last_login_ip FROM agents WHERE id=?", r.PathValue("id")).Scan(&id, &name, &user, &status, &max, &last, &ip)
	if err != nil {
		s.failure(w, 404, s.adminKeyID, "not_found", "Agent not found")
		return
	}
	out = map[string]any{"id": id, "name": name, "username": user, "status": status, "max_devices": max, "last_login_at": last, "last_login_ip": ip}
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", out})
}
func (s *Server) viewCredential(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var ct, nonce []byte
	var ver string
	var name, user string
	err := s.db.QueryRowContext(r.Context(), "SELECT password_ciphertext,password_nonce,password_key_version,name,username FROM agents WHERE id=?", r.PathValue("id")).Scan(&ct, &nonce, &ver, &name, &user)
	if err != nil {
		s.failure(w, 404, s.adminKeyID, "not_found", "Agent not found")
		return
	}
	secret, err := auth.OpenSecret(s.credentialKey, ct, nonce)
	if err != nil || ver != s.credentialKeyVersion {
		s.failure(w, 500, s.adminKeyID, "credential_unavailable", "Credential unavailable")
		return
	}
	id := r.PathValue("id")
	s.audit(r.Context(), "admin", &id, "credential_viewed", clientIP(r), "success", map[string]any{"admin_id": admin})
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", map[string]string{"name": name, "username": user, "password": secret}})
}
func (s *Server) resetCredential(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	id := r.PathValue("id")
	secret, err := randomPassword()
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	ct, n, err := auth.SealSecret(s.credentialKey, secret)
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	defer tx.Rollback()
	tag, err := tx.ExecContext(r.Context(), "UPDATE agents SET password_ciphertext=?,password_nonce=?,password_key_version=?,updated_at=now() WHERE id=?", ct, n, s.credentialKeyVersion, id)
	rows, _ := tag.RowsAffected()
	if err != nil || rows == 0 {
		s.failure(w, 404, s.adminKeyID, "not_found", "Agent not found")
		return
	}
	_, _ = tx.ExecContext(r.Context(), "UPDATE agent_sessions SET revoked_at=now() WHERE agent_id=? AND revoked_at IS NULL", id)
	_, _ = tx.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,agent_id,event_type,source_ip,result,details) VALUES('admin',?,?,'credential_reset',?,'success',?)", admin, id, clientIP(r), jsonBytes(map[string]any{}))
	if err = tx.Commit(); err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", map[string]string{"password": secret}})
}
func (s *Server) setAgentStatus(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if !s.request(w, r, s.adminKeyID, &in) {
		return
	}
	if in.Status != "enabled" && in.Status != "disabled" && in.Status != "locked" && in.Status != "suspended" {
		s.failure(w, 400, s.adminKeyID, "invalid_status", "Invalid status")
		return
	}
	id := r.PathValue("id")
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	defer tx.Rollback()
	var old string
	e = tx.QueryRowContext(r.Context(), "SELECT status FROM agents WHERE id=? FOR UPDATE", id).Scan(&old)
	if e != nil {
		s.failure(w, 404, s.adminKeyID, "not_found", "Agent not found")
		return
	}
	_, e = tx.ExecContext(r.Context(), "UPDATE agents SET status=?,updated_at=now() WHERE id=?", in.Status, id)
	if e == nil && in.Status != "enabled" {
		_, e = tx.ExecContext(r.Context(), "UPDATE agent_sessions SET revoked_at=now() WHERE agent_id=? AND revoked_at IS NULL", id)
	}
	if e == nil {
		_, e = tx.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,agent_id,event_type,source_ip,result,details) VALUES('admin',?,?,'agent_status_changed',?,'success',?)", admin, id, clientIP(r), jsonBytes(map[string]string{"old_status": old, "new_status": in.Status}))
	}
	if e != nil || tx.Commit() != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", map[string]string{"status": in.Status}})
}
func (s *Server) setDeviceLimit(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var in struct {
		MaxDevices int `json:"max_devices"`
	}
	if !s.request(w, r, s.adminKeyID, &in) {
		return
	}
	if in.MaxDevices < 1 || in.MaxDevices > 10000 {
		s.failure(w, 400, s.adminKeyID, "invalid_input", "Invalid device limit")
		return
	}
	id := r.PathValue("id")
	var old int
	err := s.db.QueryRowContext(r.Context(), "SELECT max_devices FROM agents WHERE id=?", id).Scan(&old)
	if err != nil {
		s.failure(w, 404, s.adminKeyID, "not_found", "Agent not found")
		return
	}
	if _, err = s.db.ExecContext(r.Context(), "UPDATE agents SET max_devices=?,updated_at=NOW(6) WHERE id=?", in.MaxDevices, id); err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	s.audit(r.Context(), "admin", &id, "device_limit_changed", clientIP(r), "success", map[string]any{"max_devices": in.MaxDevices, "admin_id": admin})
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", map[string]int{"max_devices": in.MaxDevices}})
}
func (s *Server) deleteAgent(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	tag, err := s.db.ExecContext(r.Context(), "DELETE FROM agents WHERE id=?", r.PathValue("id"))
	rowsAffected, _ := tag.RowsAffected()
	if err != nil || rowsAffected == 0 {
		s.failure(w, 404, s.adminKeyID, "not_found", "Agent not found")
		return
	}
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", nil})
}
func (s *Server) agentAudit(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	rows, err := s.db.QueryContext(r.Context(), "SELECT event_type,occurred_at,source_ip,result,details FROM audit_events WHERE agent_id=? ORDER BY occurred_at DESC LIMIT 200", r.PathValue("id"))
	if err != nil {
		s.failure(w, 500, s.adminKeyID, "internal_error", "Internal error")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var ev, res string
		var at time.Time
		var ip *string
		var d []byte
		if rows.Scan(&ev, &at, &ip, &res, &d) == nil {
			var details any
			_ = json.Unmarshal(d, &details)
			out = append(out, map[string]any{"event_type": ev, "occurred_at": at, "source_ip": ip, "result": res, "details": details})
		}
	}
	s.respond(w, 200, s.adminKeyID, payload{"ok", "", out})
}

func (s *Server) agentLogin(w http.ResponseWriter, r *http.Request) {
	keyID := agentKeyID()
	var in struct {
		Username         string `json:"username"`
		Password         string `json:"password"`
		ClientNumber     string `json:"client_number"`
		ClientInstanceID string `json:"client_instance_id"`
	}
	if !s.request(w, r, keyID, &in) {
		return
	}
	in.Username = strings.ToLower(strings.TrimSpace(in.Username))
	in.ClientNumber = strings.TrimSpace(in.ClientNumber)
	var id, name, ctver, status string
	var ct, nonce []byte
	var max int
	err := s.db.QueryRowContext(r.Context(), "SELECT id,name,password_ciphertext,password_nonce,password_key_version,status,max_devices FROM agents WHERE username=?", in.Username).Scan(&id, &name, &ct, &nonce, &ctver, &status, &max)
	if err != nil || status != "enabled" {
		s.agentLoginFailure(r, id, in, "invalid_credentials")
		s.failure(w, 401, keyID, "invalid_credentials", "Invalid credentials")
		return
	}
	stored, e := auth.OpenSecret(s.credentialKey, ct, nonce)
	if e != nil || ctver != s.credentialKeyVersion || stored != in.Password {
		s.agentLoginFailure(r, id, in, "invalid_credentials")
		s.failure(w, 401, keyID, "invalid_credentials", "Invalid credentials")
		return
	}
	var instance string
	if _, e = fmt.Sscan(in.ClientInstanceID, &instance); e != nil || !validUUID(in.ClientInstanceID) || in.ClientNumber == "" || len(in.ClientNumber) > 128 {
		s.failure(w, 400, keyID, "invalid_input", "Invalid client identity")
		return
	}
	tx, e := s.db.BeginTx(r.Context(), nil)
	if e != nil {
		s.failure(w, 500, keyID, "internal_error", "Internal error")
		return
	}
	defer tx.Rollback()
	var lockedStatus string
	var limit int
	if e = tx.QueryRowContext(r.Context(), "SELECT status,max_devices FROM agents WHERE id=? FOR UPDATE", id).Scan(&lockedStatus, &limit); e != nil || lockedStatus != "enabled" {
		s.failure(w, 401, keyID, "invalid_credentials", "Invalid credentials")
		return
	}
	var owner string
	e = tx.QueryRowContext(r.Context(), "SELECT agent_id FROM client_registrations WHERE client_number=?", in.ClientNumber).Scan(&owner)
	if e == nil && owner != id {
		s.failure(w, 409, keyID, "client_number_conflict", "Client number unavailable")
		return
	}
	if e == sql.ErrNoRows {
		_, e = tx.ExecContext(r.Context(), "INSERT INTO client_registrations(client_number,agent_id) VALUES(?,?)", in.ClientNumber, id)
		if e != nil {
			s.failure(w, 409, keyID, "client_number_conflict", "Client number unavailable")
			return
		}
	}
	_, _ = tx.ExecContext(r.Context(), "UPDATE agent_sessions SET revoked_at=now() WHERE agent_id=? AND client_instance_id=? AND revoked_at IS NULL", id, in.ClientInstanceID)
	_, _ = tx.ExecContext(r.Context(), "DELETE FROM agent_sessions WHERE agent_id=? AND expires_at<=now()", id)
	var count int
	if e = tx.QueryRowContext(r.Context(), "SELECT count(*) FROM agent_sessions WHERE agent_id=? AND revoked_at IS NULL AND expires_at>now()", id).Scan(&count); e != nil {
		s.failure(w, 500, keyID, "internal_error", "Internal error")
		return
	}
	if count >= limit {
		s.failure(w, 409, keyID, "device_limit_reached", "Login device limit reached")
		return
	}
	token, th, e := randomToken()
	if e != nil {
		s.failure(w, 500, keyID, "internal_error", "Internal error")
		return
	}
	expires := time.Now().UTC().Add(7 * 24 * time.Hour)
	_, e = tx.ExecContext(r.Context(), "INSERT INTO agent_sessions(id,agent_id,client_instance_id,client_number,token_hash,expires_at,login_ip,user_agent) VALUES(?,?,?,?,?,?,?,?)", newID(), id, in.ClientInstanceID, in.ClientNumber, th, expires, clientIP(r), r.UserAgent())
	if e != nil {
		s.failure(w, 500, keyID, "internal_error", "Internal error")
		return
	}
	_, _ = tx.ExecContext(r.Context(), "UPDATE agents SET last_login_at=now(),last_login_ip=?,updated_at=now() WHERE id=?", clientIP(r), id)
	_, _ = tx.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,agent_id,event_type,source_ip,client_number,client_instance_id,result) VALUES('agent',?,'agent_login_succeeded',?,?,?,'success')", id, clientIP(r), in.ClientNumber, in.ClientInstanceID)
	if e = tx.Commit(); e != nil {
		s.failure(w, 500, keyID, "internal_error", "Internal error")
		return
	}
	s.respond(w, 200, keyID, payload{"ok", "", map[string]any{"access_token": token, "token_type": "Bearer", "expires_at": expires, "agent": map[string]string{"id": id, "name": name}, "client_number": in.ClientNumber, "client_instance_id": in.ClientInstanceID, "agent_key_id": fmt.Sprintf("agent-%s-v1", id)}})
}
func (s *Server) agentLoginFailure(r *http.Request, id string, in struct {
	Username         string `json:"username"`
	Password         string `json:"password"`
	ClientNumber     string `json:"client_number"`
	ClientInstanceID string `json:"client_instance_id"`
}, reason string) {
	var p *string
	if id != "" {
		p = &id
	}
	s.audit(r.Context(), "agent", p, "agent_login_failed", clientIP(r), "failure", map[string]any{"reason": reason, "client_number": in.ClientNumber})
}
func (s *Server) agentLogout(w http.ResponseWriter, r *http.Request) {
	id, ok := s.agentKey(w, r)
	if !ok {
		return
	}
	_, _ = s.db.ExecContext(r.Context(), "UPDATE agent_sessions SET revoked_at=now() WHERE token_hash=?", hashToken(bearer(r)))
	s.audit(r.Context(), "agent", &id, "agent_logout", clientIP(r), "success", nil)
	s.respond(w, 200, agentKeyID(), payload{"ok", "", nil})
}
func (s *Server) agentMe(w http.ResponseWriter, r *http.Request) {
	id, ok := s.agentKey(w, r)
	if !ok {
		return
	}
	var name, client, instance string
	var exp time.Time
	err := s.db.QueryRowContext(r.Context(), "SELECT a.name,x.client_number,x.client_instance_id,x.expires_at FROM agent_sessions x JOIN agents a ON a.id=x.agent_id WHERE x.token_hash=?", hashToken(bearer(r))).Scan(&name, &client, &instance, &exp)
	if err != nil {
		s.failure(w, 401, agentKeyID(), "unauthorized", "Authentication required")
		return
	}
	s.respond(w, 200, agentKeyID(), payload{"ok", "", map[string]any{"agent_id": id, "agent_name": name, "client_number": client, "client_instance_id": instance, "expires_at": exp}})
}

func (s *Server) audit(ctx context.Context, actor string, agentID *string, event, ip, result string, details any) {
	var id any
	if agentID != nil {
		id = *agentID
	}
	_, _ = s.db.ExecContext(ctx, "INSERT INTO audit_events(actor_type,agent_id,event_type,source_ip,result,details) VALUES(?,?,?,?,?,?)", actor, id, event, ip, result, jsonBytes(details))
}
func jsonBytes(v any) []byte {
	b, _ := json.Marshal(v)
	if len(b) == 0 {
		return []byte(`{}`)
	}
	return b
}
func hashToken(token string) []byte { h := sha256.Sum256([]byte(token)); return h[:] }
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}
func randomPassword() (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b)[:32], nil
}
func validUsername(s string) bool {
	if len(s) < 3 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.') {
			return false
		}
	}
	return true
}
func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}
