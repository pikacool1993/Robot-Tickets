package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"golang.org/x/crypto/bcrypt"

	"robot/apps/cloud-server/internal/auth"
)

type Server struct {
	db                   *sql.DB
	keys                 auth.Keyring
	credentialKey        []byte
	credentialKeyVersion string
	adminKeyID           string
	configExport         *configExportSettings
}

func main() {
	ctx := context.Background()
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		log.Fatal("MYSQL_DSN is required (for example user:password@tcp(127.0.0.1:3306)/robot?parseTime=true)")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}
	for _, migration := range []string{"migrations/001_auth.sql", "migrations/002_task_order_sync.sql"} {
		sql, readErr := os.ReadFile(migration)
		if readErr != nil {
			log.Fatal(readErr)
		}
		for _, statement := range strings.Split(string(sql), ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err = db.ExecContext(ctx, statement); err != nil {
				log.Fatal(err)
			}
		}
	}
	if err = ensureSimulatedOrderColumn(ctx, db); err != nil {
		log.Fatal(err)
	}
	if err = ensureOrderIPColumn(ctx, db); err != nil {
		log.Fatal(err)
	}
	if err = seedBusinessTemplates(ctx, db); err != nil {
		log.Fatal(err)
	}
	if err = bootstrapAdmin(ctx, db); err != nil {
		log.Fatal(err)
	}

	keys, err := auth.DecodeKeyring(os.Getenv("TRANSPORT_KEYS_JSON"))
	if err != nil {
		log.Fatal(err)
	}
	credentialKey, err := base64.StdEncoding.DecodeString(os.Getenv("AGENT_CREDENTIAL_ENCRYPTION_KEY"))
	if err != nil || len(credentialKey) != 32 {
		log.Fatal("AGENT_CREDENTIAL_ENCRYPTION_KEY must be base64-encoded 32-byte key")
	}
	version := os.Getenv("AGENT_CREDENTIAL_KEY_VERSION")
	if version == "" {
		version = "v1"
	}
	if os.Getenv("AGENT_TRANSPORT_KEY_ID") == "" || keys[os.Getenv("AGENT_TRANSPORT_KEY_ID")] == nil {
		log.Fatal("AGENT_TRANSPORT_KEY_ID must identify a configured transport key for agent login")
	}

	s := &Server{db: db, keys: keys, credentialKey: credentialKey, credentialKeyVersion: version, adminKeyID: "admin-v1"}
	s.configExport, err = loadConfigExport(keys, credentialKey)
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	webDir := os.Getenv("ADMIN_WEB_DIR")
	if webDir == "" {
		webDir = "admin-web/dist"
	}
	mux.Handle("/", adminWebHandler(webDir))
	mux.HandleFunc("POST /api/admin/v1/auth/login", s.adminLogin)
	mux.HandleFunc("POST /api/admin/v1/auth/logout", s.adminLogout)
	mux.HandleFunc("GET /api/admin/v1/auth/me", s.adminMe)
	mux.HandleFunc("POST /api/admin/v1/client-config/export", s.exportClientConfig)
	mux.HandleFunc("POST /api/admin/v1/agents", s.createAgent)
	mux.HandleFunc("GET /api/admin/v1/agents", s.listAgents)
	mux.HandleFunc("GET /api/admin/v1/agents/{id}", s.getAgent)
	mux.HandleFunc("GET /api/admin/v1/agents/{id}/terminals/{instance}/tasks", s.getTerminalTasks)
	mux.HandleFunc("GET /api/admin/v1/agents/{id}/orders", s.listAdminOrders)
	mux.HandleFunc("POST /api/admin/v1/agents/{id}/orders/{order}/reveal-viewers", s.revealOrderViewers)
	mux.HandleFunc("GET /api/admin/v1/orders", s.listAllAdminOrders)
	mux.HandleFunc("POST /api/admin/v1/orders/viewer-roster", s.adminViewerRoster)
	mux.HandleFunc("POST /api/admin/v1/orders/{order}/reveal-viewers", s.revealAllAdminOrderViewers)
	mux.HandleFunc("PATCH /api/admin/v1/agents/{id}", s.updateAgent)
	mux.HandleFunc("POST /api/admin/v1/agents/{id}/credential/view", s.viewCredential)
	mux.HandleFunc("POST /api/admin/v1/agents/{id}/credential/reset", s.resetCredential)
	mux.HandleFunc("PATCH /api/admin/v1/agents/{id}/status", s.setAgentStatus)
	mux.HandleFunc("PATCH /api/admin/v1/agents/{id}/device-limit", s.setDeviceLimit)
	mux.HandleFunc("DELETE /api/admin/v1/agents/{id}", s.deleteAgent)
	mux.HandleFunc("GET /api/admin/v1/agents/{id}/audit-events", s.agentAudit)
	mux.HandleFunc("GET /api/admin/v1/business-device-templates", s.listBusinessTemplates)
	mux.HandleFunc("PUT /api/admin/v1/business-device-templates/{device}", s.saveBusinessTemplate)
	mux.HandleFunc("GET /api/admin/v1/agents/{id}/business-config", s.getAgentBusinessConfig)
	mux.HandleFunc("PUT /api/admin/v1/agents/{id}/business-config", s.saveAgentBusinessConfig)
	mux.HandleFunc("GET /api/admin/v1/current-match", s.getCurrentMatch)
	mux.HandleFunc("PUT /api/admin/v1/current-match", s.saveCurrentMatch)
	mux.HandleFunc("GET /api/agent/v1/time", s.serverTime)
	mux.HandleFunc("POST /api/agent/v1/auth/login", s.agentLogin)
	mux.HandleFunc("POST /api/agent/v1/auth/logout", s.agentLogout)
	mux.HandleFunc("GET /api/agent/v1/auth/me", s.agentMe)
	mux.HandleFunc("PUT /api/agent/v1/sync/tasks", s.syncTasks)
	mux.HandleFunc("POST /api/agent/v1/orders", s.syncOrder)
	mux.HandleFunc("GET /api/agent/v1/orders", s.listAgentOrders)
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("cloud-server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, securityHeaders(mux)))
}

func adminWebHandler(webDir string) http.Handler {
	files := http.FileServer(http.Dir(webDir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.NotFound(w, r)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/healthz" {
			http.NotFound(w, r)
			return
		}

		filePath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if filePath != "" && filePath != "." && fs.ValidPath(filePath) {
			if _, err := os.Stat(filepath.Join(webDir, filepath.FromSlash(filePath))); err == nil {
				files.ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, webDir+string(os.PathSeparator)+"index.html")
	})
}

func bootstrapAdmin(ctx context.Context, db *sql.DB) error {
	var count int
	if err := db.QueryRowContext(ctx, "SELECT count(*) FROM admin_users").Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return nil
	}
	username, password := os.Getenv("ADMIN_BOOTSTRAP_USERNAME"), os.Getenv("ADMIN_BOOTSTRAP_PASSWORD")
	if username == "" || password == "" {
		return errors.New("admin table is empty: ADMIN_BOOTSTRAP_USERNAME and ADMIN_BOOTSTRAP_PASSWORD are required")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, "INSERT IGNORE INTO admin_users(id,username,password_hash) VALUES(?,?,?)", newID(), strings.ToLower(username), string(hash))
	return err
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) decrypt(r *http.Request, keyID string, out any) error {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(nil, r.Body, 1<<20)
	var env auth.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		return err
	}
	if keyID != "" && env.KeyID != keyID {
		return errors.New("invalid key")
	}
	plain, err := s.keys.Open(env)
	if err != nil {
		return err
	}
	return json.Unmarshal(plain, out)
}

func (s *Server) respond(w http.ResponseWriter, status int, keyID string, value any) {
	plain, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	env, err := s.keys.Seal(keyID, plain)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(env)
}

func (s *Server) failure(w http.ResponseWriter, status int, keyID, code, message string) {
	s.respond(w, status, keyID, map[string]any{"code": code, "message": message, "data": nil})
}

func randomToken() (string, []byte, error) {
	b := make([]byte, 32)
	return tokenFromRandom(b)
}

func tokenFromRandom(dst []byte) (string, []byte, error) {
	if _, err := randRead(dst); err != nil {
		return "", nil, err
	}
	token := base64.RawURLEncoding.EncodeToString(dst)
	return token, sha256Bytes([]byte(token)), nil
}

func randRead(dst []byte) (int, error) { return cryptographicRead(dst) }

var cryptographicRead = func(dst []byte) (int, error) { return io.ReadFull(rand.Reader, dst) }

func sha256Bytes(b []byte) []byte { h := sha256.Sum256(b); return h[:] }

func newID() string {
	b := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic(err)
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func writeSession(ctx context.Context, db *sql.DB, table, ownerCol, ownerID, token string, tokenHash []byte, ip string) (time.Time, error) {
	expires := time.Now().UTC().Add(7 * 24 * time.Hour)
	query := "INSERT INTO " + table + "(id," + ownerCol + ",token_hash,expires_at,login_ip) VALUES(?,?,?,?,?)"
	_, err := db.ExecContext(ctx, query, newID(), ownerID, tokenHash, expires, ip)
	return expires, err
}
