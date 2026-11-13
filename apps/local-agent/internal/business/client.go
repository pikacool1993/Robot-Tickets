package business

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const AppID = "wxffa42ecd6c0e693d"
const N1 = "https://fccdn1.k4n.cc"

type Template struct {
	UserAgent string         `json:"user_agent"`
	Referer   string         `json:"referer"`
	E         map[string]any `json:"e"`
	S         map[string]any `json:"s"`
	V         string         `json:"v"`
}
type Config struct {
	Ready            bool      `json:"ready"`
	ErrorCode        string    `json:"error_code"`
	Message          string    `json:"message"`
	AppID            string    `json:"app_id"`
	DeviceType       string    `json:"device_type"`
	ConfigRevision   uint64    `json:"config_revision"`
	TemplateRevision uint64    `json:"template_revision"`
	Template         *Template `json:"template"`
}

func (c Config) Validate() error {
	if !c.Ready {
		return errors.New("云端业务设备配置未就绪，请联系管理员后重新登录客户端")
	}
	t := c.Template
	if c.AppID != AppID || (c.DeviceType != "windows" && c.DeviceType != "android" && c.DeviceType != "ios") || t == nil {
		return errors.New("云端业务设备配置无效")
	}
	if t.E == nil || t.S == nil || t.V == "" || t.S["platform"] != c.DeviceType || t.UserAgent == "" || strings.ContainsAny(t.UserAgent, "\r\n\x00") || !regexp.MustCompile(`^https://servicewechat\.com/`+AppID+`/[0-9]+/page-frame\.html$`).MatchString(t.Referer) {
		return errors.New("云端业务模板无效，请联系管理员")
	}
	return nil
}
func NewDeviceID() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(10000000))
	if err != nil {
		return "", err
	}
	return formatDeviceID(time.Now().UnixMilli(), n.Int64()), nil
}
func formatDeviceID(ms, n int64) string { return strconv.FormatInt(ms, 10) + strconv.FormatInt(n, 10) }

type Person struct {
	ID        string `json:"id"`
	Name      string `json:"realname"`
	Card      string `json:"real_card_id"`
	Phone     string `json:"phone"`
	Self      bool   `json:"is_self"`
	FullCard  string `json:"real_card_id2"`
	FullPhone string `json:"phone2"`
	Timestamp int64  `json:"timestamp"`
	Signature string `json:"signature"`
}

func (p *Person) UnmarshalJSON(raw []byte) error {
	type alias Person
	var v struct {
		ID json.RawMessage `json:"id"`
		*alias
	}
	v.alias = (*alias)(p)
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	id, err := scalarID(v.ID)
	p.ID = id
	return err
}
func scalarID(raw []byte) (string, error) {
	var s string
	if json.Unmarshal(raw, &s) == nil && s != "" && s != "0" {
		return s, nil
	}
	var n json.Number
	if json.Unmarshal(raw, &n) == nil {
		if _, err := strconv.ParseUint(n.String(), 10, 64); err == nil && n.String() != "0" {
			return n.String(), nil
		}
	}
	return "", errors.New("业务响应缺少有效 ID")
}

type Session struct{ Token, UserID string }
type APIError struct{ HTTP, Code int }

func (e *APIError) Error() string {
	return fmt.Sprintf("业务请求失败（HTTP %d，业务码 %d）", e.HTTP, e.Code)
}
func IsTokenExpired(err error) bool { var e *APIError; return errors.As(err, &e) && e.HTTP == 401 }

// Unknown business codes are ordinary failures until real failure samples establish their meaning.
func IsCodeExpired(err error) bool {
	// N1's code-expiry business code has not been supplied. Do not guess from WeChat codes.
	return false
}

type Client struct {
	http  *http.Client
	base  string
	delay time.Duration
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, base: N1, delay: 5 * time.Second}
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func Retry(ctx context.Context, delay time.Duration, action func() error) error {
	for attempt := 0; ; attempt++ {
		err := action()
		if err == nil {
			return nil
		}
		var netErr interface{ Timeout() bool }
		if (!errors.As(err, &netErr) && !errors.Is(err, io.ErrUnexpectedEOF)) || attempt == 3 {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
func (c *Client) call(ctx context.Context, cfg Config, path, token string, body any, out any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return errors.New("业务请求参数无效")
	}
	action := func() error {
		req, err := http.NewRequestWithContext(ctx, "POST", c.base+path, bytes.NewReader(data))
		if err != nil {
			return errors.New("业务请求参数无效")
		}
		req.Header.Set("Content-Type", "application/json;charset=utf-8")
		req.Header.Set("User-Agent", cfg.Template.UserAgent)
		req.Header.Set("Referer", cfg.Template.Referer)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := c.http.Do(req)
		if err != nil {
			return err
		}
		defer res.Body.Close()
		raw, err := io.ReadAll(io.LimitReader(res.Body, 2<<20+1))
		if err != nil {
			return err
		}
		if len(raw) > 2<<20 {
			return errors.New("业务响应过大")
		}
		if res.StatusCode < 200 || res.StatusCode >= 300 {
			return &APIError{HTTP: res.StatusCode}
		}
		var result struct {
			Code int `json:"code"`
		}
		if json.Unmarshal(raw, &result) != nil {
			return errors.New("业务响应格式错误")
		}
		expected := 1
		if path == "/fc/wx_api/v1/Login/index" {
			expected = 200
		}
		if result.Code != expected && !(strings.HasPrefix(path, "/fc/wx_api/v1/MatchCon/getBillRegion?") && result.Code == 200) {
			return &APIError{HTTP: res.StatusCode, Code: result.Code}
		}
		if json.Unmarshal(raw, out) != nil {
			return errors.New("业务响应字段无效")
		}
		return nil
	}
	if strings.HasPrefix(path, "/fc/wx_api/v1/MatchCon/getBillRegion?") {
		return action()
	}
	return Retry(ctx, c.delay, action)
}
func (c *Client) Login(ctx context.Context, cfg Config, deviceID, code string) (Session, error) {
	if err := cfg.Validate(); err != nil {
		return Session{}, err
	}
	var t Template
	raw, _ := json.Marshal(cfg.Template)
	_ = json.Unmarshal(raw, &t)
	t.S["deviceId"] = deviceID
	if cfg.DeviceType == "ios" {
		delete(t.E, "sessionId")
	}
	var out struct {
		Token string `json:"token"`
		Info  struct {
			ID json.RawMessage `json:"id"`
		} `json:"info"`
	}
	err := c.call(ctx, cfg, "/fc/wx_api/v1/Login/index", "", map[string]any{"code": code, "e": t.E, "s": t.S, "v": t.V}, &out)
	if err != nil {
		return Session{}, err
	}
	id, err := scalarID(out.Info.ID)
	if err != nil || out.Token == "" {
		return Session{}, errors.New("业务登录响应缺少 Token 或用户 ID")
	}
	return Session{out.Token, id}, nil
}
func (c *Client) People(ctx context.Context, cfg Config, s Session) ([]Person, error) {
	var out struct {
		Data []Person `json:"data"`
	}
	err := c.call(ctx, cfg, "/fc/wx_api/v1/Member/getCrewsList?lid2="+url.QueryEscape(s.UserID), s.Token, struct{}{}, &out)
	if err != nil {
		return nil, err
	}
	if out.Data == nil {
		return nil, errors.New("观演人响应缺少列表")
	}
	seen := map[string]bool{}
	for _, p := range out.Data {
		if p.ID == "" || seen[p.ID] {
			return nil, errors.New("观演人 ID 无效或重复")
		}
		seen[p.ID] = true
	}
	return out.Data, nil
}
