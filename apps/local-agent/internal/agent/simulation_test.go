package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/orders"
)

func TestSimulationPollCreatesLocalOrderWithoutOfficialClients(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	r := runs[0]
	s.root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(s.root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.root, "config", "bill-regions.json"), []byte(`{"code":1,"data":[{"region":1,"list":[{"id":1,"estate":1,"name":"109","price":"100.00","usable_count":2}]}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	s.simulation = SimulationSettings{SuccessPercent: 100, OrderSuccessPercent: 100}
	s.SetDebugMode(true)
	// Nil clients would panic if any official endpoint were used.
	s.businessClient, s.orderClient = nil, nil
	r.simulated, r.plan, r.status = true, nil, "monitoring"
	r.realtime = true
	s.pollInventory(r.task.ID, r, r.ctx, time.Now())
	s.inventory.wg.Wait()
	s.orderDetailWG.Wait()
	var requestURL, requestBody string
	for _, task := range s.tasks.Snapshot().Tasks {
		if task.ID != r.task.ID {
			continue
		}
		for _, entry := range task.Logs {
			if value, ok := strings.CutPrefix(entry.Text, "[DEBUG] 模拟验收拦截 N5（未发送） POST 下单请求链接："); ok {
				requestURL = value
			}
			if value, ok := strings.CutPrefix(entry.Text, "[DEBUG] 模拟验收拦截 N5（未发送） POST 下单请求参数："); ok {
				requestBody = value
			}
		}
	}
	if requestURL == "" || requestBody == "" {
		t.Fatal("simulated order request URL or body was not logged")
	}
	var request orders.Request
	if err := json.Unmarshal([]byte(requestBody), &request); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(requestURL)
	if err != nil {
		t.Fatal(err)
	}
	seconds, err := strconv.ParseInt(u.Query().Get("s_time"), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	body, err := orders.FromPlan(r.order.Plan, r.people, r.task.PeopleSelected)
	if err != nil {
		t.Fatal(err)
	}
	expected, path, err := orders.Build(body, key, r.business.session, time.UnixMilli(seconds*1000+request.ExpireTime%1000))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(expected)
	if err != nil || requestBody != string(raw) || requestURL != business.N5+path {
		t.Fatal("simulated request differs from real order request construction")
	}
	s.mu.Lock()
	if r.status != "order_created" || r.order == nil || !r.order.Simulated || r.order.Token != "LOCAL-SIMULATION" {
		t.Fatalf("unexpected simulation result: %s %#v", r.status, r.order)
	}
	id := r.order.ID
	s.mu.Unlock()
	items, err := s.GetOrderHistory()
	if err != nil || len(items) != 1 || !items[0].Simulated || len(items[0].Details.Bill) != 1 {
		t.Fatalf("missing local order: %#v %v", items, err)
	}
	if err := s.OrderAfterSale(r.task.ID, id, "cancel"); err != nil {
		t.Fatal(err)
	}
	// Persistence still identifies this as local after debug mode is disabled.
	s.SetDebugMode(false)
	if err := s.RefreshOrderDetail(r.task.ID, id); err != nil {
		t.Fatal(err)
	}
}

func TestSimulationFailuresDoNotMutateOrders(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	s.simulation = SimulationSettings{QueryFailurePercent: 100, CancelFailurePercent: 100, RefundFailurePercent: 100}
	for _, operation := range []string{"query", "cancel", "refund"} {
		if err := s.simulationOperation(context.Background(), r.task.ID, operation); err == nil {
			t.Fatalf("%s should fail", operation)
		}
	}
	record := &OrderRecord{Simulated: true, TaskID: r.task.ID, ID: "SIM-failure", Details: simulatedOrderDetail("SIM-failure", nil, *r.plan, time.Now())}
	key := fmt.Sprintf("created-order.v1.%d.%s", r.task.ID, record.ID)
	s.inventory.orders = map[string]*OrderRecord{key: record}
	if err := s.RefreshOrderDetail(r.task.ID, record.ID); err == nil {
		t.Fatal("query failure not surfaced")
	}
	s.simulation.QueryFailurePercent = 0
	if err := s.OrderAfterSale(r.task.ID, record.ID, "cancel"); err == nil {
		t.Fatal("cancel failure not surfaced")
	}
	if record.Details.Status != 5 {
		t.Fatal("failed operation mutated order")
	}
	if err := s.SaveSimulationSettings(defaultSimulationSettings()); err == nil {
		t.Fatal("non-debug settings accepted")
	}
	if err := s.StartSimulation(r.task.ID); err == nil {
		t.Fatal("non-debug simulation accepted")
	}
	settings := defaultSimulationSettings()
	settings.QueryFailurePercent = 101
	if settings.Validate() == nil {
		t.Fatal("invalid failure percentage accepted")
	}
}

func TestSimulatedCodeFiveUsesRandomDelayRange(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	s.simulation = defaultSimulationSettings()
	s.simulation.OrderSuccessPercent = 0
	s.simulation.OrderFailurePercents = make([]int, 12)
	s.simulation.OrderFailurePercents[1] = 100
	for i := 0; i < 100; i++ {
		result, err := s.simulationOrder(runs[0].ctx, runs[0].task.ID)
		if err != nil || result.Code != 5 {
			t.Fatalf("unexpected simulated response: %#v %v", result, err)
		}
		if result.Delay < 5*time.Second || result.Delay > 12*time.Second {
			t.Fatalf("code=5 delay outside 5-12 seconds: %v", result.Delay)
		}
	}
}

func TestSimulationNotificationDoesNotReachRealRun(t *testing.T) {
	s, runs, _ := orderFixture(t, 2)
	for _, r := range runs {
		r.status = "monitoring"
		r.plan = nil
		r.early = true
	}
	runs[0].simulated = true
	s.mu.Lock()
	s.deliverInventoryLocked(runs[0].task.ID, []inventory.Region{{ID: 1, Group: 1, Estate: 1, Name: "109", Price: "100.00", Count: 2}}, time.Now())
	if runs[0].plan == nil || runs[1].plan != nil {
		t.Error("simulation inventory leaked across modes")
	}
	s.mu.Unlock()
}

func TestSimulationStockRandomizesPerAreaAndGroupTotals(t *testing.T) {
	raw := []byte(`{"code":1,"data":[{"region":1,"usable_count":99,"list":[{"id":1,"usable_count":2},{"id":2,"usable_count":3},{"id":3,"usable_count":4},{"id":4,"usable_count":5},{"id":5,"usable_count":6}]}]}`)
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		result, err := randomizeSimulationStock(raw, true)
		if err != nil {
			t.Fatal(err)
		}
		var payload struct {
			Data []struct {
				Count int `json:"usable_count"`
				List  []struct {
					Count int `json:"usable_count"`
				} `json:"list"`
			} `json:"data"`
		}
		if err := json.Unmarshal(result, &payload); err != nil {
			t.Fatal(err)
		}
		group := payload.Data[0]
		total, active := 0, 0
		for i, area := range group.List {
			total += area.Count
			if area.Count > 0 {
				active++
			}
			if area.Count < 0 || area.Count > i+3 {
				t.Fatalf("area %d exceeds its randomized allowance: %s", i, result)
			}
		}
		if group.Count != total || active < 2 || active > 4 {
			t.Fatalf("invalid randomized inventory: %s", result)
		}
		seen[string(result)] = true
	}
	if len(seen) < 2 {
		t.Fatal("simulated stock did not vary across responses")
	}
}

func TestSimulationOpeningMinuteOverridesAllProbabilities(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	r.simulated = true
	r.match.SaleAt = time.Now().Add(-10 * time.Second)
	s.simulation = SimulationSettings{NetworkErrorPercent: 100}
	s.root = t.TempDir()
	if err := os.MkdirAll(filepath.Join(s.root, "config"), 0700); err != nil {
		t.Fatal(err)
	}
	sample := []byte(`{"code":1,"btn_status":1,"data":[{"region":1,"usable_count":123,"list":[{"id":1,"estate":1,"name":"109","price":"100.00","usable_count":123}]}]}`)
	if err := os.WriteFile(filepath.Join(s.root, "config", "bill-regions.json"), sample, 0600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		raw, err := s.simulationStock(r.ctx, r.task.ID)
		if err != nil || string(raw) != string(sample) {
			t.Fatalf("opening response changed: %s %v", raw, err)
		}
	}
	r.match.SaleAt = time.Now().Add(-2 * time.Minute)
	if _, err := s.simulationStock(r.ctx, r.task.ID); err == nil {
		t.Fatal("probability not restored after opening minute")
	}
	r.simulationQueries = 0
	if raw, err := s.simulationStock(r.ctx, r.task.ID); err != nil || string(raw) != string(sample) {
		t.Fatal("late first query should return full sample")
	}
}

func TestSimulationOpeningWindowBoundaries(t *testing.T) {
	sale := time.Now()
	for _, test := range []struct {
		now   time.Time
		count int
		full  bool
	}{
		{sale.Add(time.Second), 0, true}, {sale.Add(59 * time.Second), 2, true},
		{sale.Add(time.Minute), 2, false}, {sale.Add(2 * time.Minute), 0, true},
	} {
		if simulationFullStock(test.now, sale, test.count) != test.full {
			t.Fatalf("wrong full-stock window at %v", test.now.Sub(sale))
		}
	}
}

func TestSimulationPollWaitsForOpeningDeadline(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	r := runs[0]
	r.simulated = true
	r.plan = nil
	r.status = "waiting"
	ctx, cancel := context.WithCancel(r.ctx)
	done := make(chan struct{})
	go func() { defer close(done); s.pollInventory(r.task.ID, r, ctx, time.Now().Add(time.Hour)) }()
	cancel()
	<-done
	if r.simulationQueries != 0 {
		t.Fatal("queried before opening deadline")
	}
}

func TestOrderSimulationProbabilitySelection(t *testing.T) {
	for selected := -1; selected < 12; selected++ {
		settings := defaultSimulationSettings()
		settings.OrderSuccessPercent = 0
		settings.OrderFailurePercents = make([]int, 12)
		if selected == -1 {
			settings.OrderSuccessPercent = 100
		} else {
			settings.OrderFailurePercents[selected] = 100
		}
		if err := settings.Validate(); err != nil {
			t.Fatal(err)
		}
		for roll := 0; roll < 100; roll++ {
			if got := settings.orderOutcome(roll); got != selected {
				t.Fatalf("wanted %d, got %d for roll %d", selected, got, roll)
			}
		}
	}
	settings := defaultSimulationSettings()
	counts := map[int]int{}
	for roll := 0; roll < 100; roll++ {
		counts[settings.orderOutcome(roll)]++
	}
	if counts[-1] != settings.OrderSuccessPercent {
		t.Fatal("success probability mismatch")
	}
	for index, probability := range settings.OrderFailurePercents {
		if counts[index] != probability {
			t.Fatalf("outcome %d probability mismatch", index)
		}
	}
}

func TestOrderSimulationProbabilityValidationAndMigration(t *testing.T) {
	settings := defaultSimulationSettings()
	settings.OrderFailurePercents[0]++
	if settings.Validate() == nil {
		t.Fatal("invalid total accepted")
	}
	settings.OrderFailurePercents = []int{65}
	if settings.Validate() == nil {
		t.Fatal("missing outcomes accepted")
	}
	settings.OrderFailurePercents = make([]int, 12)
	settings.OrderFailurePercents[0] = -1
	if settings.Validate() == nil {
		t.Fatal("negative probability accepted")
	}
	settings.OrderFailurePercents = nil
	settings = settings.withOrderProbabilities()
	if len(settings.OrderFailurePercents) != 12 || settings.Validate() != nil {
		t.Fatal("old settings not migrated")
	}
}

func TestSimulationConfiguredOrderResponses(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	for selected := -1; selected < 12; selected++ {
		settings := defaultSimulationSettings()
		settings.OrderSuccessPercent = 0
		settings.OrderFailurePercents = make([]int, 12)
		if selected == -1 {
			settings.OrderSuccessPercent = 100
		} else {
			settings.OrderFailurePercents[selected] = 100
		}
		s.simulation = settings
		result, err := s.simulationOrder(runs[0].ctx, runs[0].task.ID)
		if selected == 6 || selected == 7 || selected == 10 {
			if err == nil {
				t.Fatalf("scenario %d did not fail", selected)
			}
			continue
		}
		codes := map[int]int{-1: 1, 0: 3, 1: 5, 2: 1, 3: 0, 4: 0, 5: 0, 8: 3, 9: 5, 11: 996}
		if err != nil || result.Code != codes[selected] {
			t.Fatalf("scenario %d: %#v %v", selected, result, err)
		}
		if (selected == -1) != (result.OrderID != "") {
			t.Fatalf("scenario %d wrong order ID", selected)
		}
		if (selected == 8 || selected == 9) && result.Delay != 2*time.Second {
			t.Fatal("timeout response lost")
		}
	}
}
