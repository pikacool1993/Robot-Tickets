package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"robot/apps/cloud-server/pkg/feishu"
	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/cloud"
	"robot/apps/local-agent/internal/feishunotify"
	"robot/apps/local-agent/internal/protocols"
	"robot/apps/local-agent/internal/tasks"
)

type authNoticeState struct {
	Binding  string
	Online   bool
	Sequence uint64
}

func (s *Service) initFeishuLocked() {
	s.feishu = feishunotify.New(s.ctx)
	s.authNotice = map[int64]authNoticeState{}
	// The local store already encrypts values at rest. Only configuration is
	// cached; delivery events and retry state never survive process/session exit.
	if s.store != nil {
		raw, _ := s.store.Get(s.feishuCacheKeyLocked())
		var c feishu.Config
		if len(raw) > 0 && json.Unmarshal(raw, &c) == nil {
			_ = s.feishu.Configure(c)
		}
	}
}
func (s *Service) feishuCacheKeyLocked() string {
	sum := sha256.Sum256([]byte(s.config.ServerURL + "\x00" + s.session.Agent.ID))
	return "feishu-config.v1." + hex.EncodeToString(sum[:])
}
func (s *Service) syncFeishu(ctx context.Context, session *cloud.Session) {
	s.mu.Lock()
	if s.session != session || s.feishu == nil {
		s.mu.Unlock()
		return
	}
	client, manager := s.client, s.feishu
	s.mu.Unlock()
	var c feishu.Config
	err := client.Call(ctx, "GET", "/feishu-config", session.Token, nil, &c)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.session != session || s.feishu != manager {
		return
	}
	if err != nil {
		manager.SyncFailed()
		return
	}
	if manager.Configure(c) != nil {
		manager.SyncFailed()
		return
	}
	raw, _ := json.Marshal(c)
	if s.store != nil {
		_ = s.store.Put(s.feishuCacheKeyLocked(), raw)
	}
}

func (s *Service) GetFeishuStatus() (feishunotify.View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return feishunotify.View{}, err
	}
	if s.feishu == nil {
		return feishunotify.View{}, errors.New("飞书通知尚未初始化")
	}
	return s.feishu.View(), nil
}
func (s *Service) TestFeishu() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.requireTaskSession(); err != nil {
		return err
	}
	if s.feishu == nil {
		return errors.New("飞书通知尚未初始化")
	}
	view := s.feishu.View()
	if !view.Enabled {
		return errors.New("飞书通知已关闭，请在服务端开启")
	}
	if !view.Configured {
		return errors.New("机器人未配置，请在服务端配置")
	}
	if !s.feishu.Enqueue(feishunotify.Event{ID: fmt.Sprintf("test:%d", time.Now().UnixNano()), Kind: "test", Text: fmt.Sprintf("【飞书测试】\n客户端：%s\n机器人连接测试", s.state.ClientNumber)}) {
		return errors.New("测试消息未能加入发送队列")
	}
	return nil
}

func selectedNames(t tasks.Task) string {
	names := strings.Join(t.SpectatorNames, " ")
	if names == "" {
		return "未选择"
	}
	return names
}
func (s *Service) authNoticeLocked(t tasks.Task, b protocols.Binding) {
	if s.feishu == nil || b.Protocol != s.state.AuthProtocol {
		return
	}
	key := b.Protocol + ":" + b.Account.Ref + ":" + b.Generation
	previous := s.authNotice[t.ID]
	if previous.Binding != key {
		previous = authNoticeState{Binding: key}
	}
	if b.Status == "alive" {
		previous.Online = true
	}
	if b.Status == "expired" && previous.Online {
		previous.Online = false
		previous.Sequence++
		s.feishu.Enqueue(feishunotify.Event{ID: fmt.Sprintf("auth:%d:%s:%d", t.ID, key, previous.Sequence), Kind: "auth_expired", Text: fmt.Sprintf("【授权失效】\n客户端：%s\n任务：#%d\n微信账号：%s\n观演人：%s\n原因：微信授权已失效，请重新授权", s.state.ClientNumber, t.ID, b.Account.Nickname, selectedNames(t))})
	}
	s.authNotice[t.ID] = previous
}
func (s *Service) blockedNoticeLocked(id int64, r *inventoryRun, reason string, viewers []OrderViewer) {
	if s.feishu == nil || r.simulated || r.feishuBlocked {
		return
	}
	r.feishuBlocked = true
	names := []string{}
	for _, v := range viewers {
		names = append(names, v.Name)
	}
	s.feishu.Enqueue(feishunotify.Event{ID: fmt.Sprintf("blocked:%d:%d", id, time.Now().UnixNano()), Kind: "order_blocked", Text: fmt.Sprintf("【下单受阻】\n客户端：%s\n任务：#%d\n观演人：%s\n原因：%s", s.state.ClientNumber, id, strings.Join(names, " "), reason)})
}

func (s *Service) orderNoticeLocked(record *OrderRecord, cfg business.Config) {
	if s.feishu == nil || record == nil || record.Simulated {
		return
	}
	// Do not notify orders which could not be saved locally.
	if current, _ := s.findOrderLocked(record.TaskID, record.ID); current == nil {
		return
	}
	session := s.session
	manager := s.feishu
	clientNumber := s.state.ClientNumber
	taskID, orderID := record.TaskID, record.ID
	manager.Enqueue(feishunotify.Event{ID: fmt.Sprintf("order:%d:%s", taskID, orderID), Kind: "order_created", Resolve: func(ctx context.Context) (feishunotify.Resolved, error) {
		s.mu.Lock()
		if s.session != session || s.feishu != manager {
			s.mu.Unlock()
			return feishunotify.Resolved{Stop: "会话已结束，通知已停止"}, nil
		}
		current, _ := s.findOrderLocked(taskID, orderID)
		if current == nil {
			s.mu.Unlock()
			return feishunotify.Resolved{Stop: "订单已删除，通知已停止"}, nil
		}
		copy := *current
		copy.Viewers = append([]OrderViewer{}, current.Viewers...)
		ready := completeFeishuDetail(copy.Details)
		s.mu.Unlock()
		knownDeadline, _ := parseFeishuDeadline(copy.Details.OrderEndTime)
		if !knownDeadline.IsZero() && !time.Now().Before(knownDeadline) {
			return feishunotify.Resolved{Deadline: knownDeadline}, nil
		}
		if !ready {
			detail, err := s.businessClient.MatchOrderInfo(s.taskNetworkContext(ctx, taskID), cfg, business.Session{Token: copy.Token, UserID: copy.UserID}, orderID)
			if err != nil {
				return feishunotify.Resolved{Deadline: knownDeadline}, errors.New("订单详情查询失败，等待重试")
			}
			copy.Details = detail
			s.mu.Lock()
			if s.session == session {
				if record, key := s.findOrderLocked(taskID, orderID); record != nil {
					_ = s.persistOrderDetailLocked(record, key, detail, "")
				}
			}
			s.mu.Unlock()
		}
		d := copy.Details
		deadline, _ := parseFeishuDeadline(d.OrderEndTime)
		if !completeFeishuDetail(d) {
			return feishunotify.Resolved{Deadline: deadline}, nil
		}
		return feishunotify.Resolved{Text: feishuOrderMessage(copy, clientNumber), Deadline: deadline, Ready: true}, nil
	}})
}
func feishuOrderMessage(record OrderRecord, clientNumber string) string {
	names := []string{}
	for _, v := range record.Viewers {
		names = append(names, v.Name)
	}
	lines := []string{"【下单成功】", "客户端：" + clientNumber, fmt.Sprintf("任务：#%d", record.TaskID), "比赛：" + record.Match.ID, "观演人：" + strings.Join(names, " ")}
	for _, ticket := range record.Details.Bill {
		seat := fmt.Sprintf("%d区", ticket.Region)
		if ticket.EstateName != "" {
			seat += " · " + ticket.EstateName
		}
		if ticket.Row != 0 {
			seat += fmt.Sprintf(" · %d排%d座", ticket.Row, ticket.Seat)
		}
		lines = append(lines, "区域/座位："+seat+" ￥"+ticket.Price)
	}
	return strings.Join(append(lines, "支付截止："+record.Details.OrderEndTime), "\n")
}
func parseFeishuDeadline(value string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, value); err == nil {
		return t, nil
	}
	return time.ParseInLocation("2006-01-02 15:04:05", value, time.FixedZone("Asia/Shanghai", 8*3600))
}
func completeFeishuDetail(d business.OrderDetail) bool {
	if d.ID == 0 || len(d.Bill) == 0 {
		return false
	}
	if _, err := parseFeishuDeadline(d.OrderEndTime); err != nil {
		return false
	}
	for _, ticket := range d.Bill {
		if ticket.Price == "" {
			return false
		}
	}
	return true
}
