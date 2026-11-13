package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"time"
)

func (s *Server) currentMatch(ctx context.Context) (protocol.MatchConfig, error) {
	var m protocol.MatchConfig
	var raw []byte
	var revision uint64
	err := s.db.QueryRowContext(ctx, "SELECT config,revision FROM current_match WHERE id=1").Scan(&raw, &revision)
	if err == sql.ErrNoRows {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if err = json.Unmarshal(raw, &m); err != nil {
		return m, err
	}
	m.Revision = revision
	return m, nil
}
func (s *Server) matchSnapshot(ctx context.Context) protocol.MatchConfig {
	m, err := s.currentMatch(ctx)
	if err != nil {
		return protocol.MatchConfig{Message: "当前比赛读取失败，请联系管理员后重新登录"}
	}
	return m
}
func (s *Server) getCurrentMatch(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	m, err := s.currentMatch(r.Context())
	if err != nil {
		s.businessInternal(w)
		return
	}
	s.adminRespond(w, 200, "", payload{"ok", "", m})
}
func (s *Server) saveCurrentMatch(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var m protocol.MatchConfig
	if !s.adminRequest(w, r, "", &m) {
		return
	}
	if err := m.Validate(); err != nil {
		s.adminFailure(w, 400, "", "invalid_match", err.Error())
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.businessInternal(w)
		return
	}
	defer tx.Rollback()
	m.Message = ""
	raw := jsonBytes(m)
	var result sql.Result
	if m.Revision == 0 {
		result, err = tx.ExecContext(r.Context(), "INSERT IGNORE INTO current_match(id,config,revision) VALUES(1,?,1)", raw)
	} else {
		result, err = tx.ExecContext(r.Context(), "UPDATE current_match SET config=?,revision=revision+1,updated_at=NOW(6) WHERE id=1 AND revision=?", raw, m.Revision)
	}
	if err != nil {
		s.businessInternal(w)
		return
	}
	count, err := result.RowsAffected()
	if err != nil {
		s.businessInternal(w)
		return
	}
	if count != 1 {
		s.adminFailure(w, 409, "", "config_conflict", "比赛配置已被修改，请重新加载")
		return
	}
	_, err = tx.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,event_type,source_ip,result,details) VALUES('admin',?,'current_match_updated',?,'success',?)", admin, clientIP(r), jsonBytes(map[string]any{"match_id": m.ID, "revision": m.Revision + 1}))
	if err != nil {
		s.businessInternal(w)
		return
	}
	if tx.Commit() != nil {
		s.businessInternal(w)
		return
	}
	m.Revision++
	s.adminRespond(w, 200, "", payload{"ok", "", m})
}
func (s *Server) serverTime(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.agentKey(w, r); !ok {
		return
	}
	s.respond(w, 200, agentKeyID(), payload{"ok", "", map[string]any{"unix_ms": time.Now().UnixMilli()}})
}
