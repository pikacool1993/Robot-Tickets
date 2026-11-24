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
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	where, args := orderRosterScope(r.URL.Query())
	rows, err := s.db.QueryContext(r.Context(), "SELECT viewer_ciphertext,viewer_nonce,viewer_key_version FROM agent_orders"+where+" ORDER BY created_at DESC,id DESC", args...)
	if err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer roster")
		return
	}
	defer rows.Close()
	type rosterGroup struct {
		Names []string `json:"names"`
	}
	groups := []rosterGroup{}
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
		names := make([]string, 0, len(viewers))
		for _, viewer := range viewers {
			names = append(names, viewer.Name)
		}
		groups = append(groups, rosterGroup{Names: names})
	}
	if err := rows.Err(); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer roster")
		return
	}
	if err := rows.Close(); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer roster")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"groups": groups, "order_count": len(groups)}})
}
