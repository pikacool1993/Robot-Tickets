package main

import (
	"bytes"
	"context"
	"database/sql"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

const businessAppID = "wxffa42ecd6c0e693d"

//go:embed business_templates/*.json
var businessDefaults embed.FS

var businessDevices = []string{"windows", "android", "ios"}
var businessReferer = regexp.MustCompile(`^https://servicewechat\.com/` + businessAppID + `/[0-9]+/page-frame\.html$`)

// Dynamic code and deviceId belong to each client task, never to a shared template.
type businessTemplate struct {
	UserAgent string         `json:"user_agent"`
	Referer   string         `json:"referer"`
	E         map[string]any `json:"e"`
	S         map[string]any `json:"s"`
	V         string         `json:"v"`
}

type businessSnapshot struct {
	Ready            bool              `json:"ready"`
	ErrorCode        string            `json:"error_code,omitempty"`
	Message          string            `json:"message,omitempty"`
	AppID            string            `json:"app_id"`
	DeviceType       string            `json:"device_type,omitempty"`
	ConfigRevision   uint64            `json:"config_revision,omitempty"`
	TemplateRevision uint64            `json:"template_revision,omitempty"`
	Template         *businessTemplate `json:"template,omitempty"`
}

func validBusinessDevice(device string) bool {
	return device == "windows" || device == "android" || device == "ios"
}

func parseBusinessTemplate(device string, raw []byte) (businessTemplate, error) {
	var t businessTemplate
	if !validBusinessDevice(device) {
		return t, errors.New("设备类型必须是 windows、android 或 ios")
	}
	if len(raw) > 64*1024 {
		return t, errors.New("模板不得超过 64 KB")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&t); err != nil {
		return t, errors.New("模板仅支持 user_agent、referer、e、s、v，且字段类型必须正确")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return t, errors.New("模板必须是单个 JSON 对象")
	}
	if strings.TrimSpace(t.UserAgent) == "" || len(t.UserAgent) > 4096 || strings.ContainsAny(t.UserAgent, "\r\n\x00") {
		return t, errors.New("User-Agent 不能为空、超过 4096 字节或包含换行")
	}
	ua := strings.ToLower(t.UserAgent)
	matchesDevice := (device == "windows" && strings.Contains(ua, "windows") && !strings.Contains(ua, "android")) ||
		(device == "android" && strings.Contains(ua, "android")) ||
		(device == "ios" && (strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad")))
	if !matchesDevice {
		return t, errors.New("User-Agent 必须与设备类型一致")
	}
	if !businessReferer.MatchString(t.Referer) {
		return t, errors.New("Referer 必须对应指定小程序的 page-frame.html 地址")
	}
	if t.E == nil || t.S == nil || strings.TrimSpace(t.V) == "" || len(t.V) > 128 {
		return t, errors.New("e、s 必须是对象，v 必须是非空版本字符串（最多 128 字节）")
	}
	if t.S["platform"] != device {
		return t, errors.New("s.platform 必须与设备类型一致")
	}
	if os, ok := t.S["osName"]; ok && os != device {
		return t, errors.New("s.osName 必须与设备类型一致")
	}
	if _, ok := t.S["deviceId"]; ok {
		return t, errors.New("请移除 s.deviceId，由客户端按任务生成")
	}
	if _, ok := t.E["sessionId"]; device == "ios" && ok {
		return t, errors.New("iOS 模板必须移除 e.sessionId")
	}
	return t, nil
}

// INSERT IGNORE keeps administrator edits intact across server restarts.
func seedBusinessTemplates(ctx context.Context, db *sql.DB) error {
	for _, device := range businessDevices {
		raw, err := businessDefaults.ReadFile("business_templates/" + device + ".json")
		if err != nil {
			return err
		}
		t, err := parseBusinessTemplate(device, raw)
		if err != nil {
			return fmt.Errorf("%s default: %w", device, err)
		}
		if _, err = db.ExecContext(ctx, "INSERT IGNORE INTO business_device_templates(device_type,template) VALUES(?,?)", device, jsonBytes(t)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) businessConfig(ctx context.Context, agentID string) businessSnapshot {
	out := businessSnapshot{AppID: businessAppID}
	var device sql.NullString
	var configRevision, templateRevision sql.NullInt64
	var raw []byte
	err := s.db.QueryRowContext(ctx, `SELECT c.device_type,c.revision,t.revision,t.template FROM agents a LEFT JOIN agent_business_configs c ON c.agent_id=a.id LEFT JOIN business_device_templates t ON t.device_type=c.device_type WHERE a.id=?`, agentID).Scan(&device, &configRevision, &templateRevision, &raw)
	if err != nil {
		out.ErrorCode = "business_config_unavailable"
		out.Message = "业务设备配置读取失败，请联系管理员"
		return out
	}
	out.DeviceType = device.String
	out.ConfigRevision = uint64(configRevision.Int64)
	out.TemplateRevision = uint64(templateRevision.Int64)
	if !device.Valid {
		out.ErrorCode = "business_config_missing"
		out.Message = "尚未配置业务登录设备，请联系管理员"
		return out
	}
	t, err := parseBusinessTemplate(device.String, raw)
	if err != nil || !templateRevision.Valid {
		out.ErrorCode = "business_template_invalid"
		out.Message = "业务设备模板缺失或无效，请联系管理员"
		return out
	}
	out.Ready = true
	out.Template = &t
	return out
}

func (s *Server) listBusinessTemplates(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	rows, err := s.db.QueryContext(r.Context(), "SELECT device_type,template,revision FROM business_device_templates ORDER BY device_type")
	if err != nil {
		s.businessInternal(w)
		return
	}
	defer rows.Close()
	items := []any{}
	for rows.Next() {
		var device string
		var raw json.RawMessage
		var revision uint64
		if rows.Scan(&device, &raw, &revision) != nil {
			s.businessInternal(w)
			return
		}
		items = append(items, map[string]any{"device_type": device, "template": raw, "revision": revision})
	}
	if rows.Err() != nil {
		s.businessInternal(w)
		return
	}
	s.adminRespond(w, 200, "", payload{"ok", "", items})
}
func (s *Server) businessInternal(w http.ResponseWriter) {
	s.adminFailure(w, 500, "", "internal_error", "业务配置操作失败，请重试")
}

func (s *Server) saveBusinessTemplate(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var in struct {
		Revision uint64          `json:"revision"`
		Template json.RawMessage `json:"template"`
	}
	if !s.adminRequest(w, r, "", &in) {
		return
	}
	device := r.PathValue("device")
	t, err := parseBusinessTemplate(device, in.Template)
	if err != nil {
		s.adminFailure(w, 400, "", "invalid_business_template", err.Error())
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.businessInternal(w)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), "UPDATE business_device_templates SET template=?,revision=revision+1,updated_at=NOW(6) WHERE device_type=? AND revision=?", jsonBytes(t), device, in.Revision)
	if err != nil {
		s.businessInternal(w)
		return
	}
	n, err := result.RowsAffected()
	if err != nil {
		s.businessInternal(w)
		return
	}
	if n != 1 {
		s.adminFailure(w, 409, "", "config_conflict", "模板已被修改或不存在，请重新加载")
		return
	}
	_, err = tx.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,event_type,source_ip,result,details) VALUES('admin',?,'business_template_updated',?,'success',?)", admin, clientIP(r), jsonBytes(map[string]any{"device_type": device, "revision": in.Revision + 1}))
	if err != nil {
		s.businessInternal(w)
		return
	}
	if tx.Commit() != nil {
		s.businessInternal(w)
		return
	}
	s.adminRespond(w, 200, "", payload{"ok", "", map[string]any{"revision": in.Revision + 1}})
}

func (s *Server) getAgentBusinessConfig(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	var id string
	err := s.db.QueryRowContext(r.Context(), "SELECT id FROM agents WHERE id=?", r.PathValue("id")).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		s.adminFailure(w, 404, "", "not_found", "代理商不存在")
		return
	}
	if err != nil {
		s.businessInternal(w)
		return
	}
	out := s.businessConfig(r.Context(), id)
	if out.ErrorCode == "business_config_unavailable" {
		s.businessInternal(w)
		return
	}
	s.adminRespond(w, 200, "", payload{"ok", "", out})
}

func (s *Server) saveAgentBusinessConfig(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var in struct {
		DeviceType string `json:"device_type"`
		Revision   uint64 `json:"revision"`
	}
	if !s.adminRequest(w, r, "", &in) {
		return
	}
	if !validBusinessDevice(in.DeviceType) {
		s.adminFailure(w, 400, "", "invalid_device_type", "请选择 Windows、Android 或 iOS")
		return
	}
	id := r.PathValue("id")
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.businessInternal(w)
		return
	}
	defer tx.Rollback()
	var found string
	err = tx.QueryRowContext(r.Context(), "SELECT id FROM agents WHERE id=? FOR UPDATE", id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		s.adminFailure(w, 404, "", "not_found", "代理商不存在")
		return
	}
	if err != nil {
		s.businessInternal(w)
		return
	}
	var revision uint64
	err = tx.QueryRowContext(r.Context(), "SELECT revision FROM agent_business_configs WHERE agent_id=?", id).Scan(&revision)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.businessInternal(w)
		return
	}
	if revision != in.Revision {
		s.adminFailure(w, 409, "", "config_conflict", "设备配置已被修改，请重新加载")
		return
	}
	var raw []byte
	err = tx.QueryRowContext(r.Context(), "SELECT template FROM business_device_templates WHERE device_type=? FOR UPDATE", in.DeviceType).Scan(&raw)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		s.businessInternal(w)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		s.adminFailure(w, 400, "", "invalid_business_template", "对应模板尚未配置")
		return
	}
	if _, err = parseBusinessTemplate(in.DeviceType, raw); err != nil {
		s.adminFailure(w, 400, "", "invalid_business_template", err.Error())
		return
	}
	if revision == 0 {
		_, err = tx.ExecContext(r.Context(), "INSERT INTO agent_business_configs(agent_id,device_type,revision) VALUES(?,?,1)", id, in.DeviceType)
	} else {
		_, err = tx.ExecContext(r.Context(), "UPDATE agent_business_configs SET device_type=?,revision=revision+1,updated_at=NOW(6) WHERE agent_id=?", in.DeviceType, id)
	}
	if err != nil {
		s.businessInternal(w)
		return
	}
	_, err = tx.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,agent_id,event_type,source_ip,result,details) VALUES('admin',?,?,'agent_business_config_updated',?,'success',?)", admin, id, clientIP(r), jsonBytes(map[string]any{"device_type": in.DeviceType, "revision": revision + 1}))
	if err != nil {
		s.businessInternal(w)
		return
	}
	if tx.Commit() != nil {
		s.businessInternal(w)
		return
	}
	s.adminRespond(w, 200, "", payload{"ok", "", map[string]any{"revision": revision + 1}})
}
