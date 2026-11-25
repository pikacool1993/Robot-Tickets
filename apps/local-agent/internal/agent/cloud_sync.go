package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/protocols"
)

type syncOwner struct{ Server, Agent, Instance string }
type syncTask struct {
	ID     int64  `json:"task_id"`
	Status string `json:"authorization_status"`
}
type syncTasks struct {
	Revision int64      `json:"revision"`
	Tasks    []syncTask `json:"tasks"`
}
type syncDocument struct {
	Tasks        syncTasks
	SentRevision int64
	TaskFailures int
	TaskRetryAt  time.Time
	Orders       map[string]*syncOrderEntry
}
type syncOrderEntry struct {
	Record        OrderRecord
	Config        business.Config
	Payload       json.RawMessage `json:"payload,omitempty"`
	DetailPending bool
	DetailStarted bool
	Sent          bool
	Failures      int
	RetryAt       time.Time
}

func (s *Service) syncOwnerLocked() syncOwner {
	if s.session == nil {
		return syncOwner{}
	}
	return syncOwner{s.state.ServerURL, s.session.Agent.ID, s.state.InstanceID}
}

func syncStorageKey(owner syncOwner) string {
	b, _ := json.Marshal(owner)
	h := sha256.Sum256(b)
	return "cloud-sync.v1." + hex.EncodeToString(h[:])
}

func (s *Service) syncDoc(owner syncOwner) (*syncDocument, error) {
	raw, err := s.store.Get(syncStorageKey(owner))
	if err != nil {
		return nil, err
	}
	d := &syncDocument{Orders: map[string]*syncOrderEntry{}}
	if len(raw) > 0 {
		if err = json.Unmarshal(raw, d); err != nil {
			return nil, err
		}
	}
	if d.Orders == nil {
		d.Orders = map[string]*syncOrderEntry{}
	}
	if d.Tasks.Tasks == nil {
		d.Tasks.Tasks = []syncTask{}
	}
	return d, nil
}

func (s *Service) saveSyncDoc(owner syncOwner, d *syncDocument) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	return s.store.Put(syncStorageKey(owner), raw)
}

// Called after N5 success, independently of the local order history lifecycle.
func (s *Service) seedSyncOrderLocked(owner syncOwner, record *OrderRecord, cfg business.Config) {
	if s.store == nil || owner.Agent == "" || record == nil {
		return
	}
	s.cloudSyncMu.Lock()
	defer s.cloudSyncMu.Unlock()
	d, err := s.syncDoc(owner)
	if err == nil {
		if d.Orders[record.ID] != nil {
			return
		}
		copy := *record
		entry := &syncOrderEntry{Record: copy, Config: cfg, DetailPending: true, DetailStarted: true}
		if record.Simulated {
			entry.Payload = syncOrderPayload(copy, copy.Details, nil)
			entry.DetailPending = false
			entry.DetailStarted = false
		}
		d.Orders[record.ID] = entry
		err = s.saveSyncDoc(owner, d)
	}
	if err != nil {
		s.log.Warn("订单同步队列保存失败", "order_id", record.ID)
	}
}

func (s *Service) recoverCloudSyncQueue() error {
	// Queue blobs are individually protected by the local store. Validate the
	// current owner blob during startup if a session is restored later in Login.
	entries, err := s.store.ListPrefix("cloud-sync.v1.")
	if err != nil {
		return err
	}
	for key, raw := range entries {
		var doc syncDocument
		if err := json.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("invalid pending cloud sync record: %w", err)
		}
		changed := false
		for _, entry := range doc.Orders {
			if !entry.Sent && entry.DetailPending && emptySyncPayload(entry.Payload) && entry.DetailStarted {
				entry.DetailStarted = false
				changed = true
			}
		}
		if changed {
			data, err := json.Marshal(&doc)
			if err != nil {
				return err
			}
			if err = s.store.Put(key, data); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Service) cloudSyncLoop() {
	defer close(s.cloudSyncDone)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.syncCloudOnce()
		}
	}
}

func (s *Service) syncCloudOnce() {
	s.mu.Lock()
	if s.requireTaskSession() != nil || s.client == nil || s.protocols == nil {
		s.mu.Unlock()
		return
	}
	owner := s.syncOwnerLocked()
	client, token, expiry := s.client, s.session.Token, s.session.ExpiresAt
	snapshot := make([]syncTask, 0)
	bindings := protocolBindings(s.protocols.Bindings())
	for _, task := range s.tasks.Snapshot().Tasks {
		status := "pending_authorization"
		if binding, ok := bindings[task.ID]; ok {
			switch {
			case binding.status == "expired":
				status = "expired"
			case binding.paused:
				status = "maintenance_paused"
			case binding.status == "alive":
				if s.protocols.Status(binding.protocol).State == "running" {
					status = "online"
				} else {
					status = "protocol_offline"
				}
			default:
				status = "unknown"
			}
		}
		snapshot = append(snapshot, syncTask{task.ID, status})
	}
	s.mu.Unlock()
	sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].ID < snapshot[j].ID })

	s.cloudSyncMu.Lock()
	d, err := s.syncDoc(owner)
	if err != nil {
		s.cloudSyncMu.Unlock()
		return
	}
	before, _ := json.Marshal(d.Tasks.Tasks)
	after, _ := json.Marshal(snapshot)
	if string(before) != string(after) || d.Tasks.Revision == 0 {
		d.Tasks.Revision++
		d.Tasks.Tasks = snapshot
		if s.saveSyncDoc(owner, d) != nil {
			s.cloudSyncMu.Unlock()
			return
		}
	}
	taskPayload := d.Tasks
	needsTasks := d.SentRevision < taskPayload.Revision
	tasksDue := d.TaskRetryAt.IsZero() || !time.Now().Before(d.TaskRetryAt)
	orderID := ""
	var order syncOrderEntry
	for id, entry := range d.Orders {
		if !entry.Sent && entry.RetryDue(time.Now()) && entry.DetailPending && emptySyncPayload(entry.Payload) && !entry.DetailStarted {
			entry.DetailStarted = true
			if s.saveSyncDoc(owner, d) != nil {
				s.cloudSyncMu.Unlock()
				return
			}
			orderID, order = id, *entry
			break
		}
		if !entry.Sent && entry.RetryDue(time.Now()) && !emptySyncPayload(entry.Payload) {
			orderID, order = id, *entry
			break
		}
	}
	s.cloudSyncMu.Unlock()

	ctx, cancel := context.WithDeadline(s.ctx, expiry)
	defer cancel()
	if needsTasks && tasksDue {
		var result struct {
			Revision int64 `json:"revision"`
			Applied  bool  `json:"applied"`
		}
		callCtx, stop := context.WithTimeout(ctx, 15*time.Second)
		err = client.Call(callCtx, "PUT", "/sync/tasks", token, taskPayload, &result)
		stop()
		if err != nil {
			s.cloudSyncMu.Lock()
			if d, e := s.syncDoc(owner); e == nil {
				d.TaskFailures++
				d.TaskRetryAt = time.Now().Add(syncRetryDelay(d.TaskFailures))
				_ = s.saveSyncDoc(owner, d)
			}
			s.cloudSyncMu.Unlock()
		} else {
			s.cloudSyncMu.Lock()
			d, err = s.syncDoc(owner)
			if err == nil {
				if result.Revision > d.Tasks.Revision {
					d.Tasks.Revision = result.Revision
				}
				d.SentRevision = taskPayload.Revision
				d.TaskFailures = 0
				d.TaskRetryAt = time.Time{}
				if result.Revision > taskPayload.Revision {
					d.Tasks.Revision = result.Revision + 1
					d.SentRevision = taskPayload.Revision
				}
				_ = s.saveSyncDoc(owner, d)
			}
			s.cloudSyncMu.Unlock()
		}
	}
	if orderID == "" {
		return
	}
	if emptySyncPayload(order.Payload) && order.DetailPending {
		detailCtx, stop := context.WithTimeout(ctx, 15*time.Second)
		detail, detailErr := s.businessClient.MatchOrderInfo(detailCtx, order.Config, business.Session{Token: order.Record.Token, UserID: order.Record.UserID}, order.Record.ID)
		stop()
		if ctx.Err() != nil {
			s.cloudSyncMu.Lock()
			if d, e := s.syncDoc(owner); e == nil && d.Orders[orderID] != nil {
				d.Orders[orderID].DetailStarted = false
				_ = s.saveSyncDoc(owner, d)
			}
			s.cloudSyncMu.Unlock()
			return
		}
		s.completeSyncOrderDetail(owner, order.Record, detail, detailErr)
		return
	}
	if emptySyncPayload(order.Payload) {
		return
	}
	callCtx, stop := context.WithTimeout(ctx, 15*time.Second)
	err = client.Call(callCtx, "POST", "/orders", token, json.RawMessage(order.Payload), nil)
	stop()
	if err != nil {
		s.cloudSyncMu.Lock()
		if d, e := s.syncDoc(owner); e == nil && d.Orders[orderID] != nil {
			d.Orders[orderID].Failures++
			d.Orders[orderID].RetryAt = time.Now().Add(syncRetryDelay(d.Orders[orderID].Failures))
			_ = s.saveSyncDoc(owner, d)
		}
		s.cloudSyncMu.Unlock()
		return
	}
	s.cloudSyncMu.Lock()
	defer s.cloudSyncMu.Unlock()
	d, err = s.syncDoc(owner)
	if err == nil && d.Orders[orderID] != nil {
		d.Orders[orderID].Sent = true
		d.Orders[orderID].Failures = 0
		d.Orders[orderID].RetryAt = time.Time{}
		d.Orders[orderID].Record = OrderRecord{}
		d.Orders[orderID].Config = business.Config{}
		d.Orders[orderID].Payload = nil
		_ = s.saveSyncDoc(owner, d)
	}
}

func (e *syncOrderEntry) RetryDue(now time.Time) bool {
	return e.RetryAt.IsZero() || !now.Before(e.RetryAt)
}

func emptySyncPayload(payload json.RawMessage) bool {
	trimmed := strings.TrimSpace(string(payload))
	return trimmed == "" || trimmed == "null"
}

func syncRetryDelay(failures int) time.Duration {
	delays := []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute}
	index := failures - 1
	if index < 0 {
		index = 0
	}
	if index >= len(delays) {
		index = len(delays) - 1
	}
	return delays[index]
}

func (s *Service) completeSyncOrderDetail(owner syncOwner, record OrderRecord, detail business.OrderDetail, detailErr error) {
	payload := syncOrderPayload(record, detail, detailErr)
	s.cloudSyncMu.Lock()
	d, err := s.syncDoc(owner)
	if err == nil && d.Orders[record.ID] != nil && !d.Orders[record.ID].Sent {
		d.Orders[record.ID].Payload = payload
		d.Orders[record.ID].DetailPending = false
		d.Orders[record.ID].DetailStarted = false
		err = s.saveSyncDoc(owner, d)
	}
	s.cloudSyncMu.Unlock()
	if err == nil && detailErr == nil {
		s.mu.Lock()
		if current, key := s.findOrderLocked(record.TaskID, record.ID); current != nil {
			_ = s.persistOrderDetailLocked(current, key, detail, "")
		}
		s.mu.Unlock()
	}
}

type protocolBinding struct {
	protocol string
	status   string
	paused   bool
}

func protocolBindings(bindings map[int64]protocols.Binding) map[int64]protocolBinding {
	out := make(map[int64]protocolBinding, len(bindings))
	for id, binding := range bindings {
		out[id] = protocolBinding{binding.Protocol, binding.Status, binding.Paused}
	}
	return out
}

func syncOrderPayload(record OrderRecord, detail business.OrderDetail, detailErr error) json.RawMessage {
	type seat struct {
		Tier string `json:"tier"`
		Row  int    `json:"row"`
		Seat int    `json:"seat"`
	}
	type viewer struct {
		Name  string `json:"name"`
		Phone string `json:"phone"`
	}
	seats := []seat{}
	viewers := []viewer{}
	tiers := []string{}
	for _, part := range record.Plan.Parts {
		tiers = append(tiers, part.Region.Name)
	}
	status := "unavailable"
	var deadline *time.Time
	if detailErr == nil {
		paymentDeadline, err := parsePaymentDeadline(detail.OrderEndTime)
		complete := err == nil && len(detail.Bill) == len(record.Viewers) && len(detail.Bill) > 0
		for _, ticket := range detail.Bill {
			if ticket.Row < 1 || ticket.Seat < 1 || ticket.EstateName == "" || ticket.RealName == "" || ticket.Phone == "" {
				complete = false
			}
		}
		if complete {
			status = "complete"
			deadline = &paymentDeadline
			tiers = []string{}
			for _, ticket := range detail.Bill {
				seats = append(seats, seat{ticket.EstateName, ticket.Row, ticket.Seat})
				viewers = append(viewers, viewer{ticket.RealName, ticket.Phone})
				tiers = append(tiers, ticket.EstateName)
			}
		}
	}
	if status == "unavailable" {
		for _, person := range record.Viewers {
			viewers = append(viewers, viewer{person.Name, person.Phone})
		}
	}
	raw, _ := json.Marshal(map[string]any{"order_ip": record.OrderIP, "order_id": record.ID, "task_id": record.TaskID, "match_id": record.Match.ID, "match_name": record.Match.Name, "ticket_count": len(record.Viewers), "tiers": tiers, "seats": seats, "created_at": record.CreatedAt, "payment_deadline": deadline, "detail_status": status, "viewers": viewers, "simulated": record.Simulated})
	return raw
}

func parsePaymentDeadline(value string) (time.Time, error) {
	value = strings.TrimSpace(value)
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed, nil
	}
	zone := time.FixedZone("CST", 8*3600)
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04:05.999999", "2006-01-02T15:04:05"} {
		if parsed, err := time.ParseInLocation(layout, value, zone); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported payment deadline")
}
