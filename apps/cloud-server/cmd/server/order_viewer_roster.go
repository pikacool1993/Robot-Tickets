package main

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"robot/apps/cloud-server/internal/auth"
)

// Keep the roster scope identical to the three filters exposed by order search.
func orderRosterScope(query url.Values) (string, []any) {
	where, args := []string{}, []any{}
	for _, key := range []string{"agent_id", "client_number", "match_id"} {
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			where = append(where, key+"=?")
			args = append(args, value)
		}
	}
	if len(where) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(where, " AND "), args
}

func (s *Server) adminViewerRoster(w http.ResponseWriter, r *http.Request) {
	admin, ok := s.adminKey(w, r)
	if !ok {
		return
	}
	var in struct {
		Confirm bool `json:"confirm"`
	}
	if !s.adminRequest(w, r, s.adminKeyID, &in) {
		return
	}
	if !in.Confirm {
		s.adminFailure(w, 400, s.adminKeyID, "confirmation_required", "Explicit confirmation is required")
		return
	}
	where, args := orderRosterScope(r.URL.Query())
	rows, err := s.db.QueryContext(r.Context(), "SELECT viewer_ciphertext,viewer_nonce,viewer_key_version FROM agent_orders"+where+" ORDER BY created_at DESC,id DESC", args...)
	if err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer roster")
		return
	}
	defer rows.Close()
	names := []string{}
	orderCount := 0
	for rows.Next() {
		var ciphertext, nonce []byte
		var version string
		if err := rows.Scan(&ciphertext, &nonce, &version); err != nil || version != s.credentialKeyVersion {
			s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer roster")
			return
		}
		plain, err := auth.OpenSecret(s.credentialKey, ciphertext, nonce)
		var viewers []orderViewer
		if err != nil || json.Unmarshal([]byte(plain), &viewers) != nil {
			s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer roster")
			return
		}
		for _, viewer := range viewers {
			names = append(names, viewer.Name)
		}
		orderCount++
	}
	if err := rows.Err(); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer roster")
		return
	}
	if err := rows.Close(); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer roster")
		return
	}
	details := map[string]any{"agent_id": strings.TrimSpace(r.URL.Query().Get("agent_id")), "client_number": strings.TrimSpace(r.URL.Query().Get("client_number")), "match_id": strings.TrimSpace(r.URL.Query().Get("match_id")), "order_count": orderCount, "viewer_count": len(names)}
	if _, err := s.db.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,event_type,source_ip,result,details) VALUES('admin',?,'order_viewer_roster_viewed',?,'success',?)", admin, clientIP(r), jsonBytes(details)); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to audit viewer access")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"names": names, "order_count": orderCount}})
}
