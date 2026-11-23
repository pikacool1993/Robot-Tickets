package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"robot/apps/cloud-server/internal/auth"
)

var taskAuthorizationStatuses = map[string]bool{
	"online": true, "expired": true, "pending_authorization": true,
	"protocol_offline": true, "maintenance_paused": true, "unknown": true,
}

type taskSnapshotItem struct {
	TaskID              int64  `json:"task_id"`
	AuthorizationStatus string `json:"authorization_status"`
}

func (s *Server) syncTasks(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.agentKey(w, r)
	if !ok {
		return
	}
	var in struct {
		Revision int64              `json:"revision"`
		Tasks    []taskSnapshotItem `json:"tasks"`
	}
	if !s.request(w, r, agentKeyID(), &in) {
		return
	}
	if in.Revision < 1 || in.Tasks == nil || len(in.Tasks) > 5000 {
		s.failure(w, 400, agentKeyID(), "invalid_input", "Invalid task snapshot")
		return
	}
	seen := make(map[int64]bool, len(in.Tasks))
	for _, task := range in.Tasks {
		if task.TaskID < 1 || seen[task.TaskID] || !taskAuthorizationStatuses[task.AuthorizationStatus] {
			s.failure(w, 400, agentKeyID(), "invalid_input", "Invalid task snapshot")
			return
		}
		seen[task.TaskID] = true
	}
	var clientNumber, instance string
	if err := s.db.QueryRowContext(r.Context(), "SELECT x.client_number,x.client_instance_id FROM agent_sessions x JOIN agents a ON a.id=x.agent_id WHERE x.token_hash=? AND x.agent_id=? AND x.revoked_at IS NULL AND x.expires_at>NOW(6) AND a.status='enabled'", hashToken(bearer(r)), agent).Scan(&clientNumber, &instance); err != nil {
		s.failure(w, 401, agentKeyID(), "unauthorized", "Authentication required")
		return
	}
	tx, err := s.db.BeginTx(r.Context(), nil)
	if err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), "INSERT INTO agent_task_sync_state(agent_id,client_number,client_instance_id) VALUES(?,?,?) ON DUPLICATE KEY UPDATE revision=revision", agent, clientNumber, instance); err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
		return
	}
	var revision int64
	if err := tx.QueryRowContext(r.Context(), "SELECT revision FROM agent_task_sync_state WHERE agent_id=? AND client_instance_id=? FOR UPDATE", agent, instance).Scan(&revision); err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
		return
	}
	if in.Revision <= revision {
		s.respond(w, 200, agentKeyID(), payload{"ok", "", map[string]any{"revision": revision, "applied": false}})
		return
	}
	rows, err := tx.QueryContext(r.Context(), "SELECT task_id,authorization_status FROM agent_terminal_tasks WHERE agent_id=? AND client_instance_id=? FOR UPDATE", agent, instance)
	if err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
		return
	}
	previous := map[int64]string{}
	for rows.Next() {
		var id int64
		var status string
		if err := rows.Scan(&id, &status); err != nil {
			rows.Close()
			s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
			return
		}
		previous[id] = status
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
		return
	}
	if err := rows.Close(); err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
		return
	}
	changed := 0
	for _, task := range in.Tasks {
		if previous[task.TaskID] != task.AuthorizationStatus {
			changed++
		}
		if _, err := tx.ExecContext(r.Context(), "INSERT INTO agent_terminal_tasks(agent_id,client_number,client_instance_id,task_id,authorization_status) VALUES(?,?,?,?,?) ON DUPLICATE KEY UPDATE updated_at=IF(authorization_status<>VALUES(authorization_status),NOW(6),updated_at),client_number=VALUES(client_number),authorization_status=VALUES(authorization_status)", agent, clientNumber, instance, task.TaskID, task.AuthorizationStatus); err != nil {
			s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
			return
		}
	}
	for id := range previous {
		if !seen[id] {
			if _, err := tx.ExecContext(r.Context(), "DELETE FROM agent_terminal_tasks WHERE agent_id=? AND client_instance_id=? AND task_id=?", agent, instance, id); err != nil {
				s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
				return
			}
			changed++
		}
	}
	if _, err := tx.ExecContext(r.Context(), "UPDATE agent_task_sync_state SET revision=?,client_number=?,tasks_synced_at=NOW(6) WHERE agent_id=? AND client_instance_id=?", in.Revision, clientNumber, agent, instance); err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
		return
	}
	if err := tx.Commit(); err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save tasks")
		return
	}
	s.respond(w, 200, agentKeyID(), payload{"ok", "", map[string]any{"revision": in.Revision, "applied": true, "total": len(in.Tasks), "changed": changed}})
}

func (s *Server) getTerminalTasks(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	if r.URL.Query().Get("page") != "" {
		s.getPagedTerminalTasks(w, r)
		return
	}
	var syncedAt sql.NullTime
	err := s.db.QueryRowContext(r.Context(), "SELECT tasks_synced_at FROM agent_task_sync_state WHERE agent_id=? AND client_instance_id=?", r.PathValue("id"), r.PathValue("instance")).Scan(&syncedAt)
	if err != nil && err != sql.ErrNoRows {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read terminal tasks")
		return
	}
	if err == sql.ErrNoRows {
		s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"synced": false, "tasks": []any{}}})
		return
	}
	rows, err := s.db.QueryContext(r.Context(), "SELECT task_id,authorization_status,updated_at FROM agent_terminal_tasks WHERE agent_id=? AND client_instance_id=? ORDER BY task_id", r.PathValue("id"), r.PathValue("instance"))
	if err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read terminal tasks")
		return
	}
	defer rows.Close()
	tasks := []map[string]any{}
	counts := map[string]int{"online": 0, "expired": 0, "pending_authorization": 0, "protocol_offline": 0, "maintenance_paused": 0, "unknown": 0}
	for rows.Next() {
		var id int64
		var status string
		var updated time.Time
		if err := rows.Scan(&id, &status, &updated); err != nil {
			s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read terminal tasks")
			return
		}
		if !taskAuthorizationStatuses[status] {
			status = "unknown"
		}
		counts[status]++
		tasks = append(tasks, map[string]any{"task_id": id, "authorization_status": status, "updated_at": updated})
	}
	if err := rows.Err(); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read terminal tasks")
		return
	}
	s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"synced": true, "tasks_synced_at": syncedAt.Time, "total": len(tasks), "counts": counts, "tasks": tasks}})
}

type orderSyncInput struct {
	OrderID         string        `json:"order_id"`
	TaskID          int64         `json:"task_id"`
	MatchID         string        `json:"match_id"`
	MatchName       string        `json:"match_name"`
	TicketCount     int           `json:"ticket_count"`
	Tiers           []string      `json:"tiers"`
	Seats           []orderSeat   `json:"seats"`
	CreatedAt       time.Time     `json:"created_at"`
	PaymentDeadline *time.Time    `json:"payment_deadline"`
	DetailStatus    string        `json:"detail_status"`
	Viewers         []orderViewer `json:"viewers"`
	Simulated       bool          `json:"simulated"`
}
type orderSeat struct {
	Tier string `json:"tier"`
	Row  int    `json:"row"`
	Seat int    `json:"seat"`
}
type orderViewer struct {
	Name  string `json:"name"`
	Phone string `json:"phone"`
}

func (in *orderSyncInput) validate() error {
	in.OrderID = strings.TrimSpace(in.OrderID)
	in.MatchID = strings.TrimSpace(in.MatchID)
	in.MatchName = strings.TrimSpace(in.MatchName)
	if in.OrderID == "" || len(in.OrderID) > 128 || in.TaskID < 1 || in.MatchID == "" || len(in.MatchID) > 128 || in.MatchName == "" || len(in.MatchName) > 255 || in.TicketCount < 1 || in.TicketCount > 100 || len(in.Tiers) > 100 || len(in.Seats) > 100 || len(in.Viewers) > 100 || in.CreatedAt.IsZero() {
		return errors.New("invalid order payload")
	}
	if in.DetailStatus != "complete" && in.DetailStatus != "unavailable" {
		return errors.New("invalid order detail status")
	}
	if in.DetailStatus == "complete" && (in.PaymentDeadline == nil || in.PaymentDeadline.IsZero()) {
		return errors.New("complete order details require payment deadline")
	}
	if in.Tiers == nil {
		in.Tiers = []string{}
	}
	if in.Seats == nil {
		in.Seats = []orderSeat{}
	}
	if in.Viewers == nil {
		in.Viewers = []orderViewer{}
	}
	for _, seat := range in.Seats {
		if len(seat.Tier) > 128 || seat.Row < 0 || seat.Seat < 0 {
			return errors.New("invalid seat details")
		}
		if in.DetailStatus == "complete" && (strings.TrimSpace(seat.Tier) == "" || seat.Row < 1 || seat.Seat < 1) {
			return errors.New("complete seat details require tier, row and seat number")
		}
	}
	if in.DetailStatus == "complete" && (len(in.Seats) != in.TicketCount || len(in.Viewers) != in.TicketCount) {
		return errors.New("complete order details are incomplete")
	}
	for _, viewer := range in.Viewers {
		if strings.TrimSpace(viewer.Name) == "" || len(viewer.Name) > 200 || len(viewer.Phone) > 64 {
			return errors.New("invalid viewer details")
		}
	}
	return nil
}

func (s *Server) syncOrder(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.agentKey(w, r)
	if !ok {
		return
	}
	var in orderSyncInput
	if !s.request(w, r, agentKeyID(), &in) {
		return
	}
	if err := in.validate(); err != nil {
		s.failure(w, 400, agentKeyID(), "invalid_input", "Invalid order record")
		return
	}
	var clientNumber, instance, agentName string
	if err := s.db.QueryRowContext(r.Context(), "SELECT x.client_number,x.client_instance_id,a.name FROM agent_sessions x JOIN agents a ON a.id=x.agent_id WHERE x.token_hash=? AND x.agent_id=? AND x.revoked_at IS NULL AND x.expires_at>NOW(6) AND a.status='enabled'", hashToken(bearer(r)), agent).Scan(&clientNumber, &instance, &agentName); err != nil {
		s.failure(w, 401, agentKeyID(), "unauthorized", "Authentication required")
		return
	}
	viewerRaw, _ := json.Marshal(in.Viewers)
	viewerCiphertext, viewerNonce, err := auth.SealSecret(s.credentialKey, string(viewerRaw))
	if err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save order")
		return
	}
	var deadline any
	if in.PaymentDeadline != nil {
		deadline = in.PaymentDeadline.UTC()
	}
	_, err = s.db.ExecContext(r.Context(), "INSERT INTO agent_orders(agent_id,agent_name,client_number,client_instance_id,task_id,order_id,match_id,match_name,ticket_count,tiers,seats,created_at,payment_deadline,detail_status,simulated,viewer_ciphertext,viewer_nonce,viewer_key_version) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE id=id", agent, agentName, clientNumber, instance, in.TaskID, in.OrderID, in.MatchID, in.MatchName, in.TicketCount, string(mustJSON(in.Tiers)), string(mustJSON(in.Seats)), in.CreatedAt.UTC(), deadline, in.DetailStatus, in.Simulated, viewerCiphertext, viewerNonce, s.credentialKeyVersion)
	if err != nil {
		s.failure(w, 500, agentKeyID(), "internal_error", "Unable to save order")
		return
	}
	s.respond(w, 200, agentKeyID(), payload{"ok", "", map[string]string{"order_id": in.OrderID}})
}

func (s *Server) listAgentOrders(w http.ResponseWriter, r *http.Request) {
	agent, ok := s.agentKey(w, r)
	if ok {
		s.readOrders(w, r, agent, false)
	}
}

func (s *Server) listAdminOrders(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); ok {
		s.readOrders(w, r, r.PathValue("id"), true)
	}
}

func (s *Server) listAllAdminOrders(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
		return
	}
	query := r.URL.Query()
	limit := 50
	if raw := query.Get("limit"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 200 {
			s.adminFailure(w, 400, s.adminKeyID, "invalid_input", "Invalid page size")
			return
		}
		limit = value
	}
	page := 1
	if raw := query.Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100000 {
			s.adminFailure(w, 400, s.adminKeyID, "invalid_input", "Invalid page")
			return
		}
		page = value
	}
	where := []string{}
	args := []any{}
	for _, filter := range []struct{ key, column string }{{"agent_id", "agent_id"}, {"client_number", "client_number"}, {"order_id", "order_id"}, {"match_id", "match_id"}} {
		key, column := filter.key, filter.column
		if value := strings.TrimSpace(query.Get(key)); value != "" {
			where = append(where, column+"=?")
			args = append(args, value)
		}
	}
	if value := strings.TrimSpace(query.Get("match_name")); value != "" {
		where = append(where, "match_name LIKE ?")
		args = append(args, "%"+value+"%")
	}
	if value := strings.TrimSpace(query.Get("task_id")); value != "" {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id < 1 {
			s.adminFailure(w, 400, s.adminKeyID, "invalid_input", "Invalid task ID")
			return
		}
		where = append(where, "task_id=?")
		args = append(args, id)
	}
	if value := query.Get("detail_status"); value != "" {
		if value != "complete" && value != "unavailable" {
			s.adminFailure(w, 400, s.adminKeyID, "invalid_input", "Invalid detail status")
			return
		}
		where = append(where, "detail_status=?")
		args = append(args, value)
	}
	beijing := time.FixedZone("CST", 8*60*60)
	for _, filter := range []struct{ key, op string }{{"created_from", ">="}, {"created_to", "<"}} {
		key, op := filter.key, filter.op
		if value := query.Get(key); value != "" {
			date, err := time.ParseInLocation("2006-01-02", value, beijing)
			if err != nil {
				s.adminFailure(w, 400, s.adminKeyID, "invalid_input", "Dates must use YYYY-MM-DD")
				return
			}
			if key == "created_to" {
				date = date.AddDate(0, 0, 1)
			}
			where = append(where, "created_at "+op+" ?")
			args = append(args, date)
		}
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}
	var total int
	if err := s.db.QueryRowContext(r.Context(), "SELECT COUNT(*) FROM agent_orders"+whereSQL, args...).Scan(&total); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to count orders")
		return
	}
	querySQL := "SELECT id,agent_name,client_number,client_instance_id,task_id,order_id,match_id,match_name,ticket_count,tiers,seats,created_at,payment_deadline,detail_status,simulated,viewer_ciphertext,viewer_nonce,viewer_key_version,received_at FROM agent_orders" + whereSQL + " ORDER BY created_at DESC,id DESC LIMIT ? OFFSET ?"
	pageArgs := append(append([]any{}, args...), limit, (page-1)*limit)
	rows, err := s.db.QueryContext(r.Context(), querySQL, pageArgs...)
	if err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read orders")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, taskID int64
		var agentName, client, instance, orderID, matchID, matchName, detailStatus, version string
		var ticketCount int
		var simulated bool
		var tiers, seats, cipherText, nonce []byte
		var created, received time.Time
		var deadline sql.NullTime
		if err := rows.Scan(&id, &agentName, &client, &instance, &taskID, &orderID, &matchID, &matchName, &ticketCount, &tiers, &seats, &created, &deadline, &detailStatus, &simulated, &cipherText, &nonce, &version, &received); err != nil || version != s.credentialKeyVersion {
			s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read orders")
			return
		}
		plain, err := auth.OpenSecret(s.credentialKey, cipherText, nonce)
		var viewers []orderViewer
		if err != nil || json.Unmarshal([]byte(plain), &viewers) != nil {
			s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read order viewers")
			return
		}
		for i := range viewers {
			viewers[i].Phone = maskPhone(viewers[i].Phone)
		}
		items = append(items, map[string]any{"id": id, "agent_name": agentName, "client_number": client, "client_instance_id": instance, "task_id": taskID, "order_id": orderID, "match_id": matchID, "match_name": matchName, "ticket_count": ticketCount, "tiers": json.RawMessage(tiers), "seats": json.RawMessage(seats), "created_at": created, "payment_deadline": nullableTime(deadline), "detail_status": detailStatus, "simulated": simulated, "viewers": viewers, "received_at": received})
	}
	if err := rows.Err(); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read orders")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"items": items, "page": page, "limit": limit, "total": total}})
}

func (s *Server) revealAllAdminOrderViewers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.adminKey(w, r); !ok {
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
	var orderID, version string
	var ciphertext, nonce []byte
	err := s.db.QueryRowContext(r.Context(), "SELECT order_id,viewer_ciphertext,viewer_nonce,viewer_key_version FROM agent_orders WHERE id=?", r.PathValue("order")).Scan(&orderID, &ciphertext, &nonce, &version)
	if err == sql.ErrNoRows {
		s.adminFailure(w, 404, s.adminKeyID, "not_found", "Order not found")
		return
	}
	if err != nil || version != s.credentialKeyVersion {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer details")
		return
	}
	plain, err := auth.OpenSecret(s.credentialKey, ciphertext, nonce)
	var viewers []orderViewer
	if err != nil || json.Unmarshal([]byte(plain), &viewers) != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer details")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"order_id": orderID, "viewers": viewers}})
}

func (s *Server) readOrders(w http.ResponseWriter, r *http.Request, agent string, admin bool) {
	fail := func() {
		if admin {
			s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read orders")
		} else {
			s.failure(w, 500, agentKeyID(), "internal_error", "Unable to read orders")
		}
	}
	rows, err := s.db.QueryContext(r.Context(), "SELECT id,agent_name,client_number,client_instance_id,task_id,order_id,match_id,match_name,ticket_count,tiers,seats,created_at,payment_deadline,detail_status,simulated,viewer_ciphertext,viewer_nonce,viewer_key_version,received_at FROM agent_orders WHERE agent_id=? ORDER BY created_at DESC,id DESC LIMIT 500", agent)
	if err != nil {
		fail()
		return
	}
	defer rows.Close()
	orders := []map[string]any{}
	for rows.Next() {
		var id, taskID int64
		var agentName, client, instance, orderID, matchID, matchName, detailStatus, version string
		var ticketCount int
		var simulated bool
		var tiers, seats, ciphertext, nonce []byte
		var created, received time.Time
		var deadline sql.NullTime
		err := rows.Scan(&id, &agentName, &client, &instance, &taskID, &orderID, &matchID, &matchName, &ticketCount, &tiers, &seats, &created, &deadline, &detailStatus, &simulated, &ciphertext, &nonce, &version, &received)
		if err != nil || version != s.credentialKeyVersion {
			fail()
			return
		}
		plain, err := auth.OpenSecret(s.credentialKey, ciphertext, nonce)
		var viewers []orderViewer
		if err != nil || json.Unmarshal([]byte(plain), &viewers) != nil {
			fail()
			return
		}
		if admin {
			for i := range viewers {
				viewers[i].Name = maskName(viewers[i].Name)
				viewers[i].Phone = maskPhone(viewers[i].Phone)
			}
		}
		orders = append(orders, map[string]any{"id": id, "agent_name": agentName, "client_number": client, "client_instance_id": instance, "task_id": taskID, "order_id": orderID, "match_id": matchID, "match_name": matchName, "ticket_count": ticketCount, "tiers": json.RawMessage(tiers), "seats": json.RawMessage(seats), "created_at": created, "payment_deadline": nullableTime(deadline), "detail_status": detailStatus, "simulated": simulated, "viewers": viewers, "received_at": received})
	}
	if rows.Err() != nil {
		fail()
		return
	}
	if admin {
		s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", orders})
	} else {
		s.respond(w, 200, agentKeyID(), payload{"ok", "", orders})
	}
}

func (s *Server) revealOrderViewers(w http.ResponseWriter, r *http.Request) {
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
	var client, instance, orderID, version, ciphertext string
	var nonce []byte
	err := s.db.QueryRowContext(r.Context(), "SELECT client_number,client_instance_id,order_id,viewer_ciphertext,viewer_nonce,viewer_key_version FROM agent_orders WHERE id=? AND agent_id=?", r.PathValue("order"), r.PathValue("id")).Scan(&client, &instance, &orderID, &ciphertext, &nonce, &version)
	if err == sql.ErrNoRows {
		s.adminFailure(w, 404, s.adminKeyID, "not_found", "Order not found")
		return
	}
	if err != nil || version != s.credentialKeyVersion {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer details")
		return
	}
	plain, err := auth.OpenSecret(s.credentialKey, []byte(ciphertext), nonce)
	if err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer details")
		return
	}
	var viewers []orderViewer
	if err := json.Unmarshal([]byte(plain), &viewers); err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to read viewer details")
		return
	}
	_, err = s.db.ExecContext(r.Context(), "INSERT INTO audit_events(actor_type,actor_admin_id,agent_id,event_type,source_ip,client_number,client_instance_id,result,details) VALUES('admin',?,?,'order_personal_data_viewed',?,?,?,'success',?)", admin, r.PathValue("id"), clientIP(r), client, instance, jsonBytes(map[string]string{"order_id": orderID}))
	if err != nil {
		s.adminFailure(w, 500, s.adminKeyID, "internal_error", "Unable to audit viewer access")
		return
	}
	s.adminRespond(w, 200, s.adminKeyID, payload{"ok", "", map[string]any{"order_id": orderID, "viewers": viewers}})
}

func nullableTime(value sql.NullTime) any {
	if !value.Valid {
		return nil
	}
	return value.Time
}
func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
func maskName(value string) string {
	r := []rune(value)
	if len(r) <= 1 {
		return "*"
	}
	return string(r[:1]) + strings.Repeat("*", len(r)-1)
}
func maskPhone(value string) string {
	if value == "" {
		return ""
	}
	digits := []rune(value)
	if len(digits) <= 7 {
		return "****"
	}
	return string(digits[:3]) + "****" + string(digits[len(digits)-4:])
}
