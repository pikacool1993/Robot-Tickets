package main

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Called after administrator authentication by getTerminalTasks.
func (s *Server) getPagedTerminalTasks(w http.ResponseWriter, r *http.Request) {
	fail := func() { s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read terminal tasks") }
	query := r.URL.Query()
	page, err := strconv.Atoi(query.Get("page"))
	if err != nil || page < 1 || page > 100000 {
		s.adminFailure(w, 400, s.adminKeyID, "invalid_input", "Invalid page")
		return
	}
	limit := 60
	if query.Get("limit") != "" {
		limit, err = strconv.Atoi(query.Get("limit"))
		if err != nil || (limit != 60 && limit != 120) {
			s.adminFailure(w, 400, s.adminKeyID, "invalid_input", "Invalid page size")
			return
		}
	}
	status, search := query.Get("status"), strings.TrimSpace(query.Get("q"))
	if (status != "" && !taskAuthorizationStatuses[status]) || len(search) > 20 || strings.IndexFunc(search, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		s.adminFailure(w, 400, s.adminKeyID, "invalid_input", "Invalid task filter")
		return
	}
	agent, instance := r.PathValue("id"), r.PathValue("instance")
	var synced sql.NullTime
	err = s.db.QueryRowContext(r.Context(), "SELECT tasks_synced_at FROM agent_task_sync_state WHERE agent_id=? AND client_instance_id=?", agent, instance).Scan(&synced)
	if err != nil && err != sql.ErrNoRows {
		fail()
		return
	}
	counts := map[string]int{"online": 0, "expired": 0, "pending_authorization": 0, "protocol_offline": 0, "maintenance_paused": 0, "unknown": 0}
	if err == sql.ErrNoRows {
		s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"synced": false, "tasks": []any{}, "counts": counts, "total": 0, "page": page, "limit": limit}})
		return
	}
	rows, err := s.db.QueryContext(r.Context(), "SELECT authorization_status,COUNT(*) FROM agent_terminal_tasks WHERE agent_id=? AND client_instance_id=? GROUP BY authorization_status", agent, instance)
	if err != nil {
		fail()
		return
	}
	for rows.Next() {
		var key string
		var count int
		if rows.Scan(&key, &count) != nil {
			rows.Close()
			fail()
			return
		}
		if !taskAuthorizationStatuses[key] {
			key = "unknown"
		}
		counts[key] += count
	}
	if rows.Err() != nil {
		rows.Close()
		fail()
		return
	}
	if rows.Close() != nil {
		fail()
		return
	}
	where := " WHERE agent_id=? AND client_instance_id=?"
	args := []any{agent, instance}
	if status != "" {
		where += " AND authorization_status=?"
		args = append(args, status)
	}
	if search != "" {
		where += " AND CAST(task_id AS CHAR) LIKE ?"
		args = append(args, "%"+search+"%")
	}
	var total int
	if s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM agent_terminal_tasks"+where, args...).Scan(&total) != nil {
		fail()
		return
	}
	pageArgs := append(append([]any{}, args...), limit, (page-1)*limit)
	rows, err = s.db.QueryContext(r.Context(), "SELECT task_id,authorization_status,updated_at FROM agent_terminal_tasks"+where+" ORDER BY task_id LIMIT ? OFFSET ?", pageArgs...)
	if err != nil {
		fail()
		return
	}
	defer rows.Close()
	tasks := []map[string]any{}
	for rows.Next() {
		var id int64
		var key string
		var updated time.Time
		if rows.Scan(&id, &key, &updated) != nil {
			fail()
			return
		}
		if !taskAuthorizationStatuses[key] {
			key = "unknown"
		}
		tasks = append(tasks, map[string]any{"task_id": id, "authorization_status": key, "updated_at": updated})
	}
	if rows.Err() != nil {
		fail()
		return
	}
	s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"synced": true, "tasks": tasks, "counts": counts, "total": total, "page": page, "limit": limit, "tasks_synced_at": synced.Time}})
}
