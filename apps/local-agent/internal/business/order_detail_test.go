package business

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type orderAPIRoundTripper func(*http.Request) (*http.Response, error)

func (f orderAPIRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMatchOrderInfoParsesOrderAndTicketDetails(t *testing.T) {
	c := NewClient()
	defer c.Close()
	calls := 0
	c.http.Transport = orderAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodPost || r.URL.Host != "fccdn1.k4n.cc" || r.URL.Path != "/fc/wx_api/v1/MatchOrder/getMatchOrderInfo" || r.URL.Query().Get("lid2") != "135" {
			t.Errorf("unexpected order detail request: %s %s", r.Method, r.URL.String())
		}
		if r.Header.Get("Authorization") != "Bearer saved-token" || r.Header.Get("Content-Type") != "application/json;charset=utf-8" {
			t.Errorf("missing order credentials/headers")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || body["id"] != "71" || body["order_id"] != nil {
			t.Errorf("wrong detail request body: %#v", body)
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":1,"data":{"id":71,"status":6,"create_time":"2026-11-17 12:00:00","order_end_time":"2026-11-17 12:30:00","payable":"1288.00","payed":"1288.00","refund":"0","count_bill":1,"refund_bill":0,"pay_type":"微信支付","bill":[{"id":8,"no":"VIP1","price":"1288.00","status":6,"region":3,"estate":1,"row":12,"seat":4,"realname":"测试","phone":"13800000000","estateName":"VIP1"}],"tuiBill":[],"match":{"refund":true}}}`))}, nil
	})
	detail, err := c.MatchOrderInfo(context.Background(), config(), Session{Token: "saved-token", UserID: "135"}, "71")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || detail.ID != 71 || detail.Status != 6 || detail.OrderEndTime == "" || detail.Payable != "1288.00" || !detail.RefundEnabled || len(detail.Bill) != 1 {
		t.Fatalf("order details were not parsed: calls=%d detail=%+v", calls, detail)
	}
	ticket := detail.Bill[0]
	if ticket.Region != 3 || ticket.Estate != 1 || ticket.Row != 12 || ticket.Seat != 4 || ticket.RealName != "测试" || ticket.Phone != "13800000000" {
		t.Fatalf("ticket seat/person details were not parsed: %+v", ticket)
	}
}

func TestOrderAfterSaleRequestsUseOfficialOrderIDField(t *testing.T) {
	for _, tc := range []struct {
		path string
		call func(*Client) error
	}{
		{"/fc/wx_api/v1/MatchOrder/cancelOrder", func(c *Client) error {
			return c.CancelMatchOrder(context.Background(), config(), Session{Token: "t", UserID: "135"}, 71)
		}},
		{"/fc/wx_api/v1/MatchOrder/refundOrder", func(c *Client) error {
			return c.RefundMatchOrder(context.Background(), config(), Session{Token: "t", UserID: "135"}, 71)
		}},
	} {
		t.Run(tc.path, func(t *testing.T) {
			c := NewClient()
			defer c.Close()
			calls := 0
			c.http.Transport = orderAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.URL.Path != tc.path || r.URL.Query().Get("lid2") != "135" || r.Header.Get("Authorization") != "Bearer t" {
					t.Errorf("unexpected operation request: %s", r.URL.String())
				}
				var body map[string]int64
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["order_id"] != 71 || len(body) != 1 {
					t.Errorf("wrong operation body: %#v", body)
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"code":1}`))}, nil
			})
			if err := tc.call(c); err != nil || calls != 1 {
				t.Fatalf("operation failed or replayed: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestMatchOrderInfoRejectsMismatchedIDAndBusinessError(t *testing.T) {
	for _, raw := range []string{`{"code":1,"data":{"id":72}}`, `{"code":0,"msg":"denied","data":{"id":71}}`} {
		c := NewClient()
		c.http.Transport = orderAPIRoundTripper(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(raw))}, nil
		})
		_, err := c.MatchOrderInfo(context.Background(), config(), Session{Token: "t", UserID: "135"}, "71")
		c.Close()
		if err == nil {
			t.Fatalf("accepted invalid order detail response: %s", raw)
		}
	}
}
