package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"strings"

	"robot/apps/cloud-server/internal/auth"
	"robot/apps/cloud-server/pkg/feishu"
)

func (s *Server) feishuConfig(ctx context.Context, id string) (feishu.Config, error) {
	c := feishu.Default()
	var ciphertext, nonce []byte
	var version string
	err := s.db.QueryRowContext(ctx, "SELECT enabled,order_created,auth_expired,order_blocked,webhook_ciphertext,webhook_nonce,webhook_key_version,revision FROM agent_feishu_configs WHERE agent_id=?", id).Scan(&c.Enabled, &c.OrderCreated, &c.AuthExpired, &c.OrderBlocked, &ciphertext, &nonce, &version, &c.Revision)
	if errors.Is(err, sql.ErrNoRows) {
		return c, nil
	}
	if err != nil {
		return c, err
	}
	if version != s.credentialKeyVersion {
		return c, errors.New("unknown credential key version")
	}
	raw, err := auth.OpenSecret(s.credentialKey, ciphertext, nonce)
	if err != nil {
		return c, err
	}
	c.Webhook = raw
	return c, feishu.ValidateWebhook(c.Webhook)
}

func (s *Server) getAgentFeishuConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	var id string
	if err := s.db.QueryRowContext(r.Context(), "SELECT id FROM agents WHERE id=?", r.PathValue("id")).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.adminFailure(w, 404, "", "not_found", "代理商不存在")
		} else {
			s.feishuConfigError(w, true)
		}
		return
	}
	c, err := s.feishuConfig(r.Context(), id)
	if err != nil {
		s.feishuConfigError(w, true)
		return
	}
	// Existing secrets are never returned to the administration UI.
	masked := c.MaskedWebhook()
	configured := c.Webhook != ""
	c.Webhook = ""
	s.adminRespond(w, 200, "", payload{"ok", "", map[string]any{"config": c, "configured": configured, "masked_webhook": masked}})
}

func (s *Server) getClientFeishuConfig(w http.ResponseWriter, r *http.Request) {
	id, ok := s.agentKey(w, r)
	if !ok {
		return
	}
	c, err := s.feishuConfig(r.Context(), id)
	if err != nil {
		s.feishuConfigError(w, false)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.respond(w, 200, agentKeyID(), payload{"ok", "", c})
}

func (s *Server) feishuConfigError(w http.ResponseWriter, admin bool) {
	if admin {
		s.adminFailure(w, 500, "", "internal_error", "飞书配置操作失败，请重试")
	} else {
		s.failure(w, 500, agentKeyID(), "internal_error", "飞书配置读取失败")
	}
}

func (s *Server) saveAgentFeishuConfig(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var in struct {
		feishu.Config
		ClearWebhook bool `json:"clear_webhook"`
	}
	if !s.adminRequest(w, r, "", &in) {
		return
	}
	in.Webhook = strings.TrimSpace(in.Webhook)
	if err := feishu.ValidateWebhook(in.Webhook); err != nil {
		s.adminFailure(w, 400, "", "invalid_webhook", err.Error())
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.feishuConfigError(w, true)
		return
	}
	defer tx.Rollback()
	id := r.PathValue("id")
	var found string
	if err = tx.QueryRowContext(r.Context(), "SELECT id FROM agents WHERE id=? FOR UPDATE", id).Scan(&found); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			s.adminFailure(w, 404, "", "not_found", "代理商不存在")
		} else {
			s.feishuConfigError(w, true)
		}
		return
	}
	var revision uint64
	var ciphertext, nonce []byte
	var version string
	err = tx.QueryRowContext(r.Context(), "SELECT revision,webhook_ciphertext,webhook_nonce,webhook_key_version FROM agent_feishu_configs WHERE agent_id=?", id).Scan(&revision, &ciphertext, &nonce, &version)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.feishuConfigError(w, true)
		return
	}
	if revision != in.Revision {
		s.adminFailure(w, 409, "", "config_conflict", "配置已被修改，请重新加载")
		return
	}
	if in.Webhook != "" || in.ClearWebhook || revision == 0 {
		if in.ClearWebhook {
			in.Webhook = ""
		}
		ciphertext, nonce, err = auth.SealSecret(s.credentialKey, in.Webhook)
		version = s.credentialKeyVersion
		if err != nil {
			s.feishuConfigError(w, true)
			return
		}
	}
	_, err = tx.ExecContext(r.Context(), "INSERT INTO agent_feishu_configs(agent_id,enabled,order_created,auth_expired,order_blocked,webhook_ciphertext,webhook_nonce,webhook_key_version,revision) VALUES(?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE enabled=VALUES(enabled),order_created=VALUES(order_created),auth_expired=VALUES(auth_expired),order_blocked=VALUES(order_blocked),webhook_ciphertext=VALUES(webhook_ciphertext),webhook_nonce=VALUES(webhook_nonce),webhook_key_version=VALUES(webhook_key_version),revision=VALUES(revision)", id, in.Enabled, in.OrderCreated, in.AuthExpired, in.OrderBlocked, ciphertext, nonce, version, revision+1)
	if err != nil {
		s.feishuConfigError(w, true)
		return
	}
	_, err = tx.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,agent_id,event_type,source_ip,result,details) VALUES('admin',?,?,'agent_feishu_config_updated',?,'success',?)", admin, id, clientIP(r), jsonBytes(map[string]any{"revision": revision + 1, "enabled": in.Enabled}))
	if err != nil || tx.Commit() != nil {
		s.feishuConfigError(w, true)
		return
	}
	s.adminRespond(w, 200, "", payload{"ok", "", map[string]any{"revision": revision + 1}})
}
