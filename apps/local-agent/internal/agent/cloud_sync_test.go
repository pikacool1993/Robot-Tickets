package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/store"
)

func TestSyncOrderPayloadUsesActualOrderDetail(t *testing.T) {
	created := time.Date(2026, 11, 21, 14, 0, 0, 0, time.FixedZone("CST", 8*3600))
	viewer := OrderViewer{Name: "购票人", Phone: "13800000000"}
	record := OrderRecord{OrderIP: "192.0.2.10", ID: "9001", TaskID: 7, Match: clientprotocol.MatchConfig{ID: "42", Name: "主队 VS 客队"}, Plan: inventory.Plan{Parts: []inventory.Allocation{{Region: inventory.Region{Name: "VIP"}, Quantity: 1}}}, Viewers: []OrderViewer{viewer}, CreatedAt: created}
	detail := business.OrderDetail{OrderEndTime: "2026-11-21 14:15:00", Bill: []business.OrderTicket{{EstateName: "VIP", Row: 8, Seat: 12, RealName: viewer.Name, Phone: viewer.Phone}}}
	var payload map[string]any
	if err := json.Unmarshal(syncOrderPayload(record, detail, nil), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["order_ip"] != "192.0.2.10" {
		t.Fatal("order IP missing from upload", payload)
	}
	if payload["detail_status"] != "complete" || payload["payment_deadline"] == nil || payload["simulated"] != false {
		t.Fatalf("complete official detail not included: %#v", payload)
	}
	seats := payload["seats"].([]any)[0].(map[string]any)
	if seats["row"] != float64(8) || seats["seat"] != float64(12) {
		t.Fatalf("request plan was used instead of issued seat: %#v", seats)
	}
	if got, err := parsePaymentDeadline("2026-11-21T14:15:00+08:00"); err != nil || got.Hour() != 14 || got.Location().String() == "UTC" {
		t.Fatalf("RFC3339 deadline parsing lost its offset: %v %v", got, err)
	}
	var incomplete map[string]any
	if err := json.Unmarshal(syncOrderPayload(record, business.OrderDetail{}, os.ErrDeadlineExceeded), &incomplete); err != nil {
		t.Fatal(err)
	}
	if incomplete["detail_status"] != "unavailable" || incomplete["payment_deadline"] != nil || len(incomplete["seats"].([]any)) != 0 || len(incomplete["viewers"].([]any)) != 1 {
		t.Fatalf("unavailable detail should keep only known values: %#v", incomplete)
	}
}

func TestSimulatedOrderQueuesReadyPayloadWithMarker(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{store: db}
	owner := syncOwner{"https://cloud.example", "agent", "instance"}
	created := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	viewer := OrderViewer{Name: "模拟观演人", Phone: "13800000000"}
	plan := inventory.Plan{Parts: []inventory.Allocation{{Region: inventory.Region{Name: "A区", Price: "100"}, Quantity: 1}}}
	record := &OrderRecord{ID: "SIM-123", TaskID: 7, Match: clientprotocol.MatchConfig{ID: "42", Name: "测试场次"}, Plan: plan, Viewers: []OrderViewer{viewer}, CreatedAt: created, Simulated: true, Details: simulatedOrderDetail("SIM-123", []OrderViewer{viewer}, plan, created)}
	s.seedSyncOrderLocked(owner, record, business.Config{})
	doc, err := s.syncDoc(owner)
	if err != nil {
		t.Fatal(err)
	}
	entry := doc.Orders[record.ID]
	if entry == nil || entry.DetailPending || entry.DetailStarted || emptySyncPayload(entry.Payload) {
		t.Fatalf("simulated order is not immediately ready to sync: %+v", entry)
	}
	var payload map[string]any
	if err := json.Unmarshal(entry.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["simulated"] != true || payload["detail_status"] != "complete" {
		t.Fatalf("simulated order payload is missing its marker or local details: %#v", payload)
	}
}

func TestCloudSyncQueueOwnerIsolationAndRestartRecovery(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := &Service{store: db}
	a := syncOwner{"http://cloud", "agent-a", "instance"}
	b := syncOwner{"http://cloud", "agent-b", "instance"}
	if syncStorageKey(a) == syncStorageKey(b) {
		t.Fatal("sync queues from separate agents share storage")
	}
	doc := &syncDocument{Orders: map[string]*syncOrderEntry{"123": {Record: OrderRecord{ID: "123"}, DetailPending: true, DetailStarted: true}}}
	if err := s.saveSyncDoc(a, doc); err != nil {
		t.Fatal(err)
	}
	if err := s.recoverCloudSyncQueue(); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.syncDoc(a)
	if err != nil {
		t.Fatal(err)
	}
	if !recovered.Orders["123"].DetailPending || recovered.Orders["123"].DetailStarted {
		t.Fatalf("restart did not make unfinished initial detail query resumable: %+v", recovered.Orders["123"])
	}
}

func TestCloudSyncRetryBackoff(t *testing.T) {
	want := []time.Duration{5 * time.Second, 15 * time.Second, 30 * time.Second, time.Minute, time.Minute}
	for i, delay := range want {
		if got := syncRetryDelay(i + 1); got != delay {
			t.Fatalf("failure %d retry delay = %s, want %s", i+1, got, delay)
		}
	}
}
