package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"time"

	"robot/apps/local-agent/internal/business"
	"robot/apps/local-agent/internal/inventory"
	"robot/apps/local-agent/internal/netdebug"
	"robot/apps/local-agent/internal/tasks"
)

type SimulationSettings struct {
	QueryFailurePercent  int   `json:"queryFailurePercent"`
	CancelFailurePercent int   `json:"cancelFailurePercent"`
	RefundFailurePercent int   `json:"refundFailurePercent"`
	Enabled              bool  `json:"enabled"`
	SuccessPercent       int   `json:"successPercent"`
	OrderSuccessPercent  int   `json:"orderSuccessPercent"`
	OrderFailurePercents []int `json:"orderFailurePercents"`
	PaidOrderPercent     int   `json:"paidOrderPercent"`
	EmptyPercent         int   `json:"emptyPercent"`
	UnavailablePercent   int   `json:"unavailablePercent"`
	NetworkErrorPercent  int   `json:"networkErrorPercent"`
	BusinessErrorPercent int   `json:"businessErrorPercent"`
}

func (s *Service) SetDebugMode(enabled bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.debugMode = enabled
}

func defaultSimulationSettings() SimulationSettings {
	return (SimulationSettings{SuccessPercent: 35, OrderSuccessPercent: 35, PaidOrderPercent: 50, EmptyPercent: 30, UnavailablePercent: 15, NetworkErrorPercent: 10, BusinessErrorPercent: 10}).withOrderProbabilities()
}

// Upgrade older settings that only stored the success percentage.
func (s SimulationSettings) withOrderProbabilities() SimulationSettings {
	if s.OrderFailurePercents == nil {
		s.OrderFailurePercents = make([]int, 12)
		remainder := 100 - s.OrderSuccessPercent
		for i := range s.OrderFailurePercents {
			s.OrderFailurePercents[i] = remainder / 12
			if i < remainder%12 {
				s.OrderFailurePercents[i]++
			}
		}
	}
	return s
}

func (s SimulationSettings) orderOutcome(roll int) int {
	s = s.withOrderProbabilities()
	if roll < s.OrderSuccessPercent {
		return -1
	}
	roll -= s.OrderSuccessPercent
	for i, probability := range s.OrderFailurePercents {
		if roll < probability {
			return i
		}
		roll -= probability
	}
	return -1
}

func (s SimulationSettings) Validate() error {
	if s.OrderSuccessPercent < 0 || s.OrderSuccessPercent > 100 {
		return errors.New("模拟下单成功概率须在 0 到 100 之间")
	}
	s = s.withOrderProbabilities()
	if len(s.OrderFailurePercents) != 12 {
		return errors.New("必须配置全部 12 种下单失败场景概率")
	}
	orderTotal := s.OrderSuccessPercent
	for _, probability := range s.OrderFailurePercents {
		if probability < 0 || probability > 100 {
			return errors.New("下单场景概率须在 0 到 100 之间")
		}
		orderTotal += probability
	}
	if orderTotal != 100 {
		return errors.New("下单成功和 12 种失败场景概率合计必须为 100%")
	}
	if s.PaidOrderPercent < 0 || s.PaidOrderPercent > 100 {
		return errors.New("模拟订单已付款概率须在 0 到 100 之间")
	}
	for _, v := range []int{s.QueryFailurePercent, s.CancelFailurePercent, s.RefundFailurePercent} {
		if v < 0 || v > 100 {
			return errors.New("模拟接口失败概率须在 0 到 100 之间")
		}
	}
	values := []int{s.SuccessPercent, s.EmptyPercent, s.UnavailablePercent, s.NetworkErrorPercent, s.BusinessErrorPercent}
	total := 0
	for _, v := range values {
		if v < 0 || v > 100 {
			return errors.New("模拟概率须在 0 到 100 之间")
		}
		total += v
	}
	if total != 100 {
		return errors.New("模拟场景概率总和必须为 100%")
	}
	return nil
}

func (s *Service) SimulationSettings() SimulationSettings {
	s.mu.Lock()
	defer s.mu.Unlock()
	v := s.simulation.withOrderProbabilities()
	v.OrderFailurePercents = append([]int{}, v.OrderFailurePercents...)
	return v
}

func (s *Service) SaveSimulationSettings(v SimulationSettings) error {
	v = v.withOrderProbabilities()
	if err := v.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.debugMode {
		return errors.New("当前不是调试模式")
	}
	for _, r := range s.inventory.runs {
		if r.status != "stopped" && r.status != "order_created" && r.status != "order_blocked" && r.status != "order_verifying" {
			return errors.New("请先停止库存任务，再修改模拟设置")
		}
	}
	raw, _ := json.Marshal(v)
	if err := s.store.Put("simulation-settings.v1", raw); err != nil {
		return errors.New("模拟设置保存失败")
	}
	v.OrderFailurePercents = append([]int{}, v.OrderFailurePercents...)
	s.simulation = v
	return nil
}

func (s *Service) StartSimulation(id int64) error {
	s.mu.Lock()
	if !s.debugMode {
		s.mu.Unlock()
		return errors.New("当前不是调试模式")
	}
	if !s.simulation.Enabled {
		s.mu.Unlock()
		return errors.New("请先在客户端设置中启用模拟验收")
	}
	if err := s.requireTaskSession(); err != nil {
		s.mu.Unlock()
		return err
	}
	if s.inventoryActiveLocked(id) {
		s.mu.Unlock()
		return errors.New("请先停止该任务的真实库存监控")
	}
	var task tasks.Task
	found := false
	for _, item := range s.tasks.Snapshot().Tasks {
		if item.ID == id {
			task, found = item, true
			break
		}
	}
	if !found {
		s.mu.Unlock()
		return errors.New("任务不存在")
	}
	data, err := s.tasks.Business(id)
	if err != nil || !data.Usable || len(task.PeopleSelected) == 0 {
		s.mu.Unlock()
		return errors.New("模拟验收需要已登录业务账号和已选择的观演人")
	}
	for _, r := range s.inventory.runs {
		if !r.simulated && s.inventoryActiveLocked(r.task.ID) {
			s.mu.Unlock()
			return errors.New("模拟验收要求其他任务均已停止")
		}
	}
	b := s.businessRuns[id]
	if b == nil {
		s.mu.Unlock()
		return errors.New("模拟验收需要已登录的业务账号")
	}
	catalog := tasks.Catalog{}
	catalog = s.tasks.Snapshot().Catalog
	match := s.session.CurrentMatch
	if match.SaleAt.IsZero() {
		s.mu.Unlock()
		return errors.New("请先配置比赛开售时间")
	}
	now, start := s.calibratedNowLocked(), time.Now()
	query, target, early := inventory.Schedule(now, match.SaleAt, s.inventory.settings)
	var defaults []inventory.Region
	if early {
		raw, readErr := readDefaultStock(s.root + "/config/bill-regions.json")
		if readErr == nil {
			defaults, readErr = inventory.Parse(raw)
		}
		if readErr != nil {
			s.mu.Unlock()
			return fmt.Errorf("模拟默认库存读取失败：%w", readErr)
		}
	}
	ctx, cancel := context.WithCancel(s.ctx)
	qctx, qcancel := context.WithCancel(ctx)
	r := &inventoryRun{simulated: true, ctx: ctx, cancel: cancel, queryCancel: qcancel, business: b, status: "waiting", message: "模拟验收：等待开售后第1秒查询", task: task, catalog: catalog, match: match, people: data.People, realtime: !early, early: early, queryAt: query, target: target, defaults: defaults}
	r.matcher = inventory.NewMatcher(catalog, task)
	if err := r.prepareOrderPeople(); err != nil {
		cancel()
		s.mu.Unlock()
		return err
	}
	if old := s.inventory.runs[id]; old != nil && old.cancel != nil {
		old.cancel()
	}
	s.inventory.runs[id] = r
	_ = s.tasks.AppendLog(id, "info", "模拟验收已启动：仅使用本地响应，不访问官方接口")
	s.inventory.wg.Add(1)
	queryDeadline, targetDeadline := start.Add(query.Sub(now)), start.Add(target.Sub(now))
	if early {
		s.inventory.wg.Add(1)
		go func() {
			defer s.inventory.wg.Done()
			if waitInventory(ctx, time.Until(targetDeadline)) {
				s.mu.Lock()
				defer s.mu.Unlock()
				s.inventoryDueLocked(id, r)
			}
		}()
	}
	s.mu.Unlock()
	go func() { defer s.inventory.wg.Done(); s.pollInventory(id, r, qctx, queryDeadline) }()
	return nil
}

func (s *Service) simulationStock(ctx context.Context, id int64) ([]byte, error) {
	s.mu.Lock()
	settings := s.simulation
	full := false
	var saleAt time.Time
	var elapsed time.Duration
	if r := s.inventory.runs[id]; r != nil && r.simulated {
		saleAt = r.match.SaleAt
		elapsed = s.calibratedNowLocked().Sub(saleAt)
		full = simulationFullStock(s.calibratedNowLocked(), saleAt, r.simulationQueries)
		r.simulationQueries++
	}
	s.mu.Unlock()
	roll := rand.Intn(100)
	var raw []byte
	var err error
	switch {
	case full:
		raw, err = readDefaultStock(s.root + "/config/bill-regions.json")
	case roll < settings.SuccessPercent:
		raw, err = readDefaultStock(s.root + "/config/bill-regions.json")
		if err == nil {
			raw, err = randomizeSimulationStock(raw, elapsed >= 2*time.Minute)
		}
	case roll < settings.SuccessPercent+settings.EmptyPercent:
		raw = []byte(`{"code":1,"btn_status":1,"btn_text":"已售罄","data":[]}`)
	case roll < settings.SuccessPercent+settings.EmptyPercent+settings.UnavailablePercent:
		raw = []byte(`{"code":1,"btn_status":0,"btn_text":"暂未开售","data":[]}`)
	case roll < settings.SuccessPercent+settings.EmptyPercent+settings.UnavailablePercent+settings.NetworkErrorPercent:
		err = context.DeadlineExceeded
	default:
		codes := []int{-1, 996, 997, 998, 999, 200, 1234}
		code := codes[rand.Intn(len(codes))]
		raw = []byte(fmt.Sprintf(`{"code":%d,"msg":"模拟业务异常"}`, code))
		err = &business.APIError{HTTP: 200, Code: code}
	}
	netdebug.Response(s.taskNetworkContext(ctx, id), "SIM-N1", "POST", "/fc/wx_api/v1/MatchCon/getBillRegion", 200, raw, time.Now(), err)
	return raw, err
}

func simulationFullStock(now, sale time.Time, queries int) bool {
	return queries == 0 || (!now.Before(sale) && now.Before(sale.Add(time.Minute)))
}

// Keep the captured region structure and prices, but generate a fresh stock
// combination for every successful simulated inventory response.
func randomizeSimulationStock(raw []byte, returning bool) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil || payload == nil {
		return nil, errors.New("模拟库存样例格式无效")
	}
	data, ok := payload["data"].([]any)
	if !ok {
		return nil, errors.New("模拟库存样例缺少票区数据")
	}
	type areaRef struct {
		group, area map[string]any
		maximum     int
	}
	areas := make([]areaRef, 0)
	for _, groupValue := range data {
		group, ok := groupValue.(map[string]any)
		if !ok {
			continue
		}
		groupAreas, ok := group["list"].([]any)
		if !ok {
			continue
		}
		group["usable_count"] = 0
		for _, areaValue := range groupAreas {
			area, ok := areaValue.(map[string]any)
			if !ok {
				continue
			}
			maximum := int(numberValue(area["usable_count"]))
			area["usable_count"] = 0
			areas = append(areas, areaRef{group: group, area: area, maximum: maximum})
		}
	}
	active := 0
	if returning {
		if len(areas) > 0 {
			active = 2 + rand.Intn(3)
			if active > len(areas) {
				active = len(areas)
			}
		}
	} else if len(areas) > 0 {
		active = (len(areas)*3 + 4) / 5
		if active < 1 {
			active = 1
		}
		if active > len(areas) {
			active = len(areas)
		}
	}
	perm := rand.Perm(len(areas))
	for _, index := range perm[:active] {
		entry := areas[index]
		if entry.maximum < 1 {
			continue
		}
		var count int
		if returning {
			if rand.Intn(100) < 80 {
				count = 1 + rand.Intn(2)
			} else {
				count = 3 + rand.Intn(3)
			}
		} else {
			count = 10 + rand.Intn(91)
		}
		if count > entry.maximum {
			count = entry.maximum
		}
		entry.area["usable_count"] = count
		entry.group["usable_count"] = numberValue(entry.group["usable_count"]) + float64(count)
	}
	payload["code"], payload["btn_status"], payload["btn_text"] = 1, 1, "立即购买"
	return json.Marshal(payload)
}

func numberValue(value any) float64 {
	if number, ok := value.(float64); ok {
		return number
	}
	return 0
}

func (s *Service) simulationOrder(ctx context.Context, id int64) (business.OrderResult, error) {
	settings := s.SimulationSettings()
	result := business.OrderResult{}
	var err error
	outcome := settings.orderOutcome(rand.Intn(100))
	if outcome == -1 {
		result = business.OrderResult{Code: 1, OrderID: fmt.Sprintf("SIM-%d", time.Now().UnixMicro())}
	} else {
		switch outcome {
		case 0:
			result = business.OrderResult{Code: 3, Message: "模拟库存不足"}
		case 1:
			result = business.OrderResult{Code: 5, Delay: time.Duration(5+rand.Intn(8)) * time.Second, Message: "模拟抢座失败"}
		case 2:
			result = business.OrderResult{Code: 1, Message: "模拟成功码缺少订单 ID"}
		case 3:
			result = business.OrderResult{Code: 0, Message: "您有未支付的订单"}
		case 4:
			result = business.OrderResult{Code: 0, Message: "此身份证已购买过本场比赛门票"}
		case 5:
			result = business.OrderResult{Code: 0, Message: "您已超过最高购票量"}
		case 6:
			err = context.DeadlineExceeded
		case 7:
			err = errors.New("模拟响应无法解析")
		case 8:
			result = business.OrderResult{Code: 3, Delay: 2 * time.Second, Message: "模拟库存不足（timeout=2）"}
		case 9:
			result = business.OrderResult{Code: 5, Delay: 2 * time.Second, Message: "模拟抢座失败（timeout=2）"}
		case 10:
			err = errors.New("模拟连接中断")
		default:
			result = business.OrderResult{Code: 996, Message: "模拟其他业务失败"}
		}
	}
	raw, _ := json.Marshal(map[string]any{"code": result.Code, "msg": result.Message, "data": result.OrderID, "timeout": result.Delay.Seconds()})
	netdebug.Response(s.taskNetworkContext(ctx, id), "SIM-N5", "POST", "/fc/wx_api/v1/MatchCon/createMatchOrder", 200, raw, time.Now(), err)
	return result, err
}

func simulatedOrderDetail(orderID string, viewers []OrderViewer, plan inventory.Plan, now time.Time) business.OrderDetail {
	serial, _ := strconv.ParseInt(strings.TrimPrefix(orderID, "SIM-"), 10, 64)
	if serial == 0 {
		serial = time.Now().UnixMicro()
	}
	detail := business.OrderDetail{ID: serial, Status: 5, CreateTime: now.Format("2006-01-02 15:04:05"), OrderEndTime: now.Add(15 * time.Minute).Format("2006-01-02 15:04:05"), PayType: "微信支付", RefundEnabled: true, Bill: []business.OrderTicket{}, Refunded: []business.OrderTicket{}}
	total := 0.0
	for i, part := range plan.Parts {
		price, _ := strconv.ParseFloat(part.Region.Price, 64)
		for n := 0; n < part.Quantity; n++ {
			viewer := OrderViewer{}
			if len(viewers) > 0 {
				viewer = viewers[(len(detail.Bill))%len(viewers)]
			}
			detail.Bill = append(detail.Bill, business.OrderTicket{ID: int64(i*100 + n + 1), No: part.Region.Name, Price: part.Region.Price, Status: 5, Region: part.Region.Group, Estate: part.Region.Estate, Row: n + 1, Seat: n + 1, RealName: viewer.Name, Phone: viewer.Phone, EstateName: part.Region.Name})
			total += price
		}
	}
	detail.CountBill = len(detail.Bill)
	detail.Payable = fmt.Sprintf("%.2f", total)
	detail.Payed, detail.Refund = "0.00", "0.00"
	return detail
}

// Simulated records stay local even after debug mode is disabled or the app restarts.
func (s *Service) simulationOperation(ctx context.Context, taskID int64, operation string) error {
	settings := s.SimulationSettings()
	probability := settings.QueryFailurePercent
	if operation == "cancel" {
		probability = settings.CancelFailurePercent
	}
	if operation == "refund" {
		probability = settings.RefundFailurePercent
	}
	var err error
	if rand.Intn(100) < probability {
		failures := []string{"模拟网络超时", "模拟断网", "模拟响应无法解析", "模拟订单 token 失效", "模拟服务端拒绝操作"}
		err = errors.New(failures[rand.Intn(len(failures))])
	}
	code, message := 1, "操作成功"
	if err != nil {
		code, message = -1, err.Error()
	}
	raw, _ := json.Marshal(map[string]any{"code": code, "msg": message})
	netdebug.Response(s.taskNetworkContext(ctx, taskID), "SIM-ORDER", "POST", "/simulation/order/"+operation, 200, raw, time.Now(), err)
	return err
}
