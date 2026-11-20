package business

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net/http"
	"net/url"
	"robot/apps/local-agent/internal/netdebug"
	"strconv"
	"strings"
	"time"
)

const N5 = "https://fccdn5.k4n.cc"

type OrderResult struct {
	Code    int
	Message string
	OrderID string
	Delay   time.Duration
}

type OrderTicket struct {
	ID         int64  `json:"id"`
	No         string `json:"no"`
	Price      string `json:"price"`
	Status     int    `json:"status"`
	Region     int    `json:"region"`
	Estate     int    `json:"estate"`
	Row        int    `json:"row"`
	Seat       int    `json:"seat"`
	RealName   string `json:"realname"`
	Phone      string `json:"phone"`
	EstateName string `json:"estateName"`
}
type OrderDetail struct {
	ID            int64         `json:"id"`
	Status        int           `json:"status"`
	CreateTime    string        `json:"create_time"`
	OrderEndTime  string        `json:"order_end_time"`
	Payable       string        `json:"payable"`
	Payed         string        `json:"payed"`
	Refund        string        `json:"refund"`
	CountBill     int           `json:"count_bill"`
	RefundBill    int           `json:"refund_bill"`
	PayType       string        `json:"pay_type"`
	RefundEnabled bool          `json:"refundEnabled"`
	Bill          []OrderTicket `json:"bill"`
	Refunded      []OrderTicket `json:"tuiBill"`
}

func (c *Client) MatchOrderInfo(ctx context.Context, cfg Config, session Session, id string) (OrderDetail, error) {
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			OrderDetail
			Match struct {
				Refund bool `json:"refund"`
			} `json:"match"`
		} `json:"data"`
	}
	if err := c.callOrderAPI(ctx, cfg, session, "/fc/wx_api/v1/MatchOrder/getMatchOrderInfo", map[string]string{"id": id}, &out); err != nil {
		return OrderDetail{}, err
	}
	if out.Code != 1 {
		return OrderDetail{}, errors.New(out.Msg)
	}
	if strconv.FormatInt(out.Data.ID, 10) != id {
		return OrderDetail{}, errors.New("订单详情 ID 与请求不一致")
	}
	detail := out.Data.OrderDetail
	detail.RefundEnabled = out.Data.Match.Refund
	return detail, nil
}

func (c *Client) CancelMatchOrder(ctx context.Context, cfg Config, session Session, id int64) error {
	return c.matchOrderAction(ctx, cfg, session, "/fc/wx_api/v1/MatchOrder/cancelOrder", id)
}
func (c *Client) RefundMatchOrder(ctx context.Context, cfg Config, session Session, id int64) error {
	return c.matchOrderAction(ctx, cfg, session, "/fc/wx_api/v1/MatchOrder/refundOrder", id)
}
func (c *Client) matchOrderAction(ctx context.Context, cfg Config, session Session, path string, id int64) error {
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := c.callOrderAPI(ctx, cfg, session, path, map[string]int64{"order_id": id}, &out); err != nil {
		return err
	}
	if out.Code != 1 {
		return errors.New(out.Msg)
	}
	return nil
}

func (c *Client) callOrderAPI(ctx context.Context, cfg Config, session Session, path string, body, out any) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if session.Token == "" || session.UserID == "" {
		return errors.New("订单凭据不完整")
	}
	if path != "/fc/wx_api/v1/MatchOrder/getMatchOrderInfo" && path != "/fc/wx_api/v1/MatchOrder/cancelOrder" && path != "/fc/wx_api/v1/MatchOrder/refundOrder" {
		return errors.New("订单接口路径无效")
	}
	u := N1 + path + "?lid2=" + url.QueryEscape(session.UserID)
	raw, err := json.Marshal(body)
	if err != nil {
		return errors.New("订单请求参数无效")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	req.Header.Set("User-Agent", cfg.Template.UserAgent)
	req.Header.Set("Referer", cfg.Template.Referer)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	started := time.Now()
	res, err := client.Do(req)
	if err != nil {
		netdebug.Response(ctx, "N1", "POST", path, 0, nil, started, err)
		return err
	}
	defer res.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	netdebug.Response(ctx, "N1", "POST", path, res.StatusCode, raw, started, err)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return &APIError{HTTP: res.StatusCode}
	}
	if len(raw) > 2<<20 {
		return errors.New("订单响应过大")
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return errors.New("订单响应格式异常")
	}
	return nil
}

func (r OrderResult) BlocksPurchase() bool {
	for _, hint := range []string{"您有未支付的订单", "此身份证已购买过本场比赛门票", "您已超过最高购票量"} {
		if strings.Contains(r.Message, hint) {
			return true
		}
	}
	return false
}

// OrderJSONBody provides already-encoded, read-only JSON for one submission.
type OrderJSONBody interface{ OrderJSON() ([]byte, error) }

// CreateOrder performs exactly one request. Transport failures must never replay a POST.
// Response URLs are ignored and redirects are disabled, including for custom clients.
func (c *Client) CreateOrder(ctx context.Context, cfg Config, session Session, path string, body any) (OrderResult, error) {
	if err := cfg.Validate(); err != nil {
		return OrderResult{}, err
	}
	u, err := url.Parse(path)
	if err != nil || u.IsAbs() || u.Host != "" || u.Path != "/fc/wx_api/v1/MatchCon/createMatchOrder" || u.Query().Get("lid2") != session.UserID || session.Token == "" || session.UserID == "" {
		return OrderResult{}, errors.New("下单会话或请求路径无效")
	}
	var raw []byte
	if encoded, ok := body.(OrderJSONBody); ok {
		raw, err = encoded.OrderJSON()
	} else {
		raw, err = json.Marshal(body)
	}
	if err != nil {
		return OrderResult{}, errors.New("下单参数无法序列化")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.orderBase+path, bytes.NewReader(raw))
	if err != nil {
		return OrderResult{}, errors.New("下单请求无效")
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	req.Header.Set("User-Agent", cfg.Template.UserAgent)
	req.Header.Set("Referer", cfg.Template.Referer)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	client := *c.http
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	started := time.Now()
	res, err := client.Do(req)
	if err != nil {
		netdebug.Response(ctx, "N5", "POST", path, 0, nil, started, err)
		return OrderResult{}, err
	}
	defer res.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	netdebug.Response(ctx, "N5", "POST", path, res.StatusCode, raw, started, err)
	if err != nil {
		return OrderResult{}, err
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return OrderResult{}, &APIError{HTTP: res.StatusCode}
	}
	if len(raw) > 2<<20 {
		return OrderResult{}, errors.New("下单响应过大")
	}
	var result struct {
		Code    *int            `json:"code"`
		Message string          `json:"msg"`
		Data    json.RawMessage `json:"data"`
		Timeout json.RawMessage `json:"timeout"`
	}
	if json.Unmarshal(raw, &result) != nil || result.Code == nil {
		return OrderResult{}, errors.New("下单响应格式异常")
	}
	out := OrderResult{Code: *result.Code, Message: result.Message}
	if out.Code == 1 {
		id, err := scalarID(result.Data)
		n, parseErr := strconv.ParseUint(id, 10, 64)
		if err != nil || parseErr != nil || n == 0 || id[0] == '0' {
			return out, errors.New("下单成功响应缺少有效订单 ID，需核实")
		}
		out.OrderID = id
	}
	if out.Code == 5 {
		out.Delay = orderRetryDelay(result.Timeout, 3*time.Second)
	} else if out.Code == 3 {
		out.Delay = orderRetryDelay(result.Timeout, 5600*time.Millisecond)
	} else if out.Code != 1 {
		out.Delay = orderRetryDelay(result.Timeout, time.Second)
	}
	return out, nil
}

func orderRetryDelay(raw json.RawMessage, fallback time.Duration) time.Duration {
	var n float64
	if json.Unmarshal(raw, &n) != nil {
		var s string
		if json.Unmarshal(raw, &s) != nil {
			return fallback
		}
		n, _ = strconv.ParseFloat(s, 64)
	}
	if n <= 0 || math.IsNaN(n) || math.IsInf(n, 0) || n >= float64(math.MaxInt64)/float64(time.Second) {
		return fallback
	}
	d := time.Duration(n * float64(time.Second))
	if d <= 0 {
		return fallback
	}
	return d
}
