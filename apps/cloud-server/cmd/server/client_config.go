package main

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"net/http"
	"os"
	"time"

	"robot/apps/cloud-server/internal/clientconfig"
)

type configExportSettings struct {
	exporter  clientconfig.Exporter
	serverURL string
}

// No settings keeps existing installations working, with export disabled.
// Partial or invalid settings fail startup rather than silently using new keys.
func loadConfigExport(keys map[string][]byte, credentialKey []byte) (*configExportSettings, error) {
	names := []string{"CLIENT_CONFIG_SERVER_URL", "CLIENT_CONFIG_ENCRYPTION_KEY_ID", "CLIENT_CONFIG_ENCRYPTION_KEY", "CLIENT_CONFIG_SIGNING_KEY_ID", "CLIENT_CONFIG_SIGNING_SEED"}
	count := 0
	for _, name := range names {
		if os.Getenv(name) != "" {
			count++
		}
	}
	if count == 0 {
		return nil, nil
	}
	if count != len(names) {
		return nil, errors.New("all CLIENT_CONFIG_* settings are required to enable configuration export")
	}
	serverURL, err := clientconfig.NormalizeServerURL(os.Getenv(names[0]))
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(os.Getenv(names[2]))
	if err != nil || len(key) != 32 {
		return nil, errors.New("CLIENT_CONFIG_ENCRYPTION_KEY must be a Base64-encoded 32-byte key")
	}
	seed, err := base64.StdEncoding.DecodeString(os.Getenv(names[4]))
	if err != nil || len(seed) != ed25519.SeedSize {
		return nil, errors.New("CLIENT_CONFIG_SIGNING_SEED must be a Base64-encoded 32-byte Ed25519 seed")
	}
	if !clientconfig.ValidID(os.Getenv(names[1])) || !clientconfig.ValidID(os.Getenv(names[3])) {
		return nil, errors.New("invalid configuration encryption/signing key ID")
	}
	if bytes.Equal(key, credentialKey) || bytes.Equal(key, seed) {
		return nil, errors.New("configuration encryption, signing and credential keys must be independent")
	}
	for _, transportKey := range keys {
		if bytes.Equal(key, transportKey) || bytes.Equal(seed, transportKey) {
			return nil, errors.New("configuration keys must be independent of transport keys")
		}
	}
	if bytes.Equal(seed, credentialKey) {
		return nil, errors.New("signing seed must be independent of credential key")
	}
	return &configExportSettings{clientconfig.Exporter{EncryptionKeyID: os.Getenv(names[1]), EncryptionKey: key, SigningKeyID: os.Getenv(names[3]), SigningKey: ed25519.NewKeyFromSeed(seed)}, serverURL}, nil
}

func (s *Server) exportClientConfig(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var in struct{}
	if !s.adminRequest(w, r, s.adminKeyID, &in) {
		return
	}
	if s.configExport == nil {
		s.adminFailure(w, 503, "", "config_export_unavailable", "服务端尚未配置客户端配置导出参数")
		return
	}
	keyID := agentKeyID()
	key := s.keys[keyID]
	if len(key) != 32 || !clientconfig.ValidID(keyID) {
		s.adminFailure(w, 503, "", "config_export_unavailable", "客户端通信密钥配置不可用")
		return
	}
	id := newID()
	c := clientconfig.Config{Version: 1, ServerURL: s.configExport.serverURL, TransportKeyID: keyID, TransportKey: base64.StdEncoding.EncodeToString(key), ExportID: id, IssuedAt: time.Now().UTC()}
	file, err := s.configExport.exporter.Seal(c)
	if err != nil {
		s.adminFailure(w, 500, "", "config_export_failed", "无法生成客户端配置")
		return
	}
	// Fail closed: do not disclose a configuration unless its audit is persisted.
	_, err = s.db.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,event_type,source_ip,result,details) VALUES('admin',?,'client_config_exported',?,'success',?)", admin, clientIP(r), jsonBytes(map[string]any{"export_id": id, "transport_key_id": keyID, "encryption_key_id": s.configExport.exporter.EncryptionKeyID, "signing_key_id": s.configExport.exporter.SigningKeyID}))
	if err != nil {
		s.adminFailure(w, 500, "", "config_export_failed", "无法记录配置导出审计，请重试")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="bootstrap.cfg"`)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file)
}
