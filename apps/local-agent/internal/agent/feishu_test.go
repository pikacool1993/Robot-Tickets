package agent

import (
	"context"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"strings"
	"testing"
	"time"

	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/feishunotify"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/tasks"
)

func TestAuthNotificationRequiresObservedOnlineAndExplicitExpiry(t *testing.T) {
	m := feishunotify.New(context.Background())
	defer m.Close()
	s := &Service{feishu: m, authNotice: map[int64]authNoticeState{}, state: State{AuthProtocol: "wechat"}}
	task := tasks.Task{ID: 1, SpectatorNames: []string{"张三", "李四"}}
	b := protocols.Binding{Protocol: "wechat", Generation: "one", Status: "expired", Account: protocols.Account{Ref: "account"}}
	s.authNoticeLocked(task, b)
	if s.authNotice[1].Sequence != 0 {
		t.Fatal("startup expiry was replayed")
	}
	b.Status = "alive"
	s.authNoticeLocked(task, b)
	b.Status = "offline"
	s.authNoticeLocked(task, b)
	if !s.authNotice[1].Online || s.authNotice[1].Sequence != 0 {
		t.Fatal("transient offline triggered auth notification")
	}
	b.Status = "expired"
	s.authNoticeLocked(task, b)
	s.authNoticeLocked(task, b)
	if s.authNotice[1].Sequence != 1 {
		t.Fatal("continuous expiry duplicated")
	}
	b.Status = "alive"
	s.authNoticeLocked(task, b)
	b.Status = "expired"
	s.authNoticeLocked(task, b)
	if s.authNotice[1].Sequence != 2 {
		t.Fatal("recovered task did not rearm")
	}
	if selectedNames(task) != "张三 李四" || selectedNames(tasks.Task{}) != "未选择" {
		t.Fatal("selected names formatting")
	}
}

func TestLateBlockedResponseDoesNotNotifyNewSessionManager(t *testing.T) {
	s, runs, key := orderFixture(t, 1)
	oldManager := feishunotify.New(context.Background())
	defer oldManager.Close()
	nextManager := feishunotify.New(context.Background())
	defer nextManager.Close()
	s.feishu = oldManager
	s.orderClient = orderSenderFunc(func(context.Context, business.Config, business.Session, string, any) (business.OrderResult, error) {
		s.mu.Lock()
		s.feishu = nextManager
		s.mu.Unlock()
		return business.OrderResult{Code: 5, Message: "您有未支付的订单"}, nil
	})
	s.submitInventoryOrder(runs[0].ctx, runs[0].task.ID, runs[0], key)
	if runs[0].feishuBlocked {
		t.Fatal("late response was routed through new account notifier")
	}
}
func TestOrderNoticeMessagePairsSeatsAndPricesAndExcludesSecrets(t *testing.T) {
	record := OrderRecord{TaskID: 7, Token: "private-token", UserID: "private-user", OrderIP: "192.0.2.1", Viewers: []OrderViewer{{Name: "张三", Card: "private-card", Phone: "private-phone"}, {Name: "李四"}}, Details: business.OrderDetail{ID: 71, OrderEndTime: "2026-11-25 20:15:00", Bill: []business.OrderTicket{{Region: 1, EstateName: "南区", Row: 2, Seat: 18, Price: "180.00"}, {Region: 2, EstateName: "北区", Row: 3, Seat: 19, Price: "280.00"}}}}
	record.Match.ID = "456"
	text := feishuOrderMessage(record, "A")
	for _, want := range []string{"任务：#7", "比赛：456", "观演人：张三 李四", "南区 · 2排18座 ￥180.00\n区域/座位：2区 · 北区 · 3排19座 ￥280.00", "支付截止：2026-11-25 20:15:00"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q: %s", want, text)
		}
	}
	for _, secret := range []string{record.Token, record.UserID, record.OrderIP, record.Viewers[0].Card, record.Viewers[0].Phone} {
		if strings.Contains(text, secret) {
			t.Error("sensitive field in notification")
		}
	}
	deadline, err := parseFeishuDeadline(record.Details.OrderEndTime)
	if err != nil || deadline.UTC().Hour() != 12 {
		t.Fatalf("deadline did not use Beijing time: %s %v", deadline, err)
	}
	if !completeFeishuDetail(record.Details) {
		t.Fatal("complete details rejected")
	}
	record.Details.Bill[0].Price = ""
	if completeFeishuDetail(record.Details) {
		t.Fatal("sent incomplete price")
	}
	record.Details.OrderEndTime = ""
	if completeFeishuDetail(record.Details) {
		t.Fatal("sent without deadline")
	}
}
func TestSimulatedOrderAndBlockedEventsAreExcluded(t *testing.T) {
	s, runs, _ := orderFixture(t, 1)
	m := feishunotify.New(context.Background())
	defer m.Close()
	s.feishu = m
	r := runs[0]
	r.simulated = true
	s.blockedNoticeLocked(r.task.ID, r, "您有未支付的订单", nil)
	if r.feishuBlocked {
		t.Fatal("simulation treated as real event")
	}
	s.orderNoticeLocked(&OrderRecord{Simulated: true}, business.Config{})
	if m.View().Pending != 0 {
		t.Fatal("simulation queued notification")
	}
	r.simulated = false
	s.blockedNoticeLocked(r.task.ID, r, "您有未支付的订单", nil)
	if !r.feishuBlocked {
		t.Fatal("real blocked event not handled")
	}
}
func TestFeishuCacheIsScopedToServerAndAgent(t *testing.T) {
	s, _, _ := orderFixture(t, 1)
	s.config = &protocol.Config{ServerURL: "https://cloud.example"}
	s.session.Agent.ID = "agent-one"
	first := s.feishuCacheKeyLocked()
	s.session.Agent.ID = "agent-two"
	if first == s.feishuCacheKeyLocked() {
		t.Fatal("cross-agent cache")
	}
	s.session.Agent.ID = "agent-one"
	saved := s.config.ServerURL
	s.config.ServerURL = "https://other.example"
	if first == s.feishuCacheKeyLocked() {
		t.Fatal("cross-server cache")
	}
	s.config.ServerURL = saved
}

// A future deadline is parsed as an absolute instant, including RFC3339 offsets.
func TestFeishuDeadlineRFC3339(t *testing.T) {
	want := time.Now().UTC().Truncate(time.Second)
	got, err := parseFeishuDeadline(want.Format(time.RFC3339))
	if err != nil || !got.Equal(want) {
		t.Fatal(got, err)
	}
}
