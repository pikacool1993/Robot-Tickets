package protocols

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Account struct {
	Ref      string `json:"ref"`
	Nickname string `json:"nickname"`
}
type Scan struct {
	ID      string   `json:"-"`
	Image   string   `json:"image"`
	Status  string   `json:"status"`
	Account *Account `json:"-"`
}

// Protocol deliberately exposes only the capabilities required in the first release.
type Protocol interface {
	Begin(context.Context) (Scan, error)
	Poll(context.Context, string) (Scan, error)
	Maintain(context.Context, Account) (string, error)
	Logout(context.Context, Account) error
}
type adapter struct {
	kind, base, token, proxy string
	client                   *http.Client
}
type envelope struct {
	Code    int             `json:"code"`
	Success bool            `json:"success"`
	Data    json.RawMessage `json:"data"`
}
type callError struct{ status, code int }

func (e *callError) Error() string {
	return fmt.Sprintf("协议请求未成功（HTTP %d，业务码 %d）", e.status, e.code)
}
func (a *adapter) call(ctx context.Context, method, path string, body any, out any) error {
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			return err
		}
	}
	req, err := http.NewRequestWithContext(ctx, method, a.base+path, &buf)
	if err != nil {
		return errors.New("协议请求参数无效")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+a.token)
	res, err := a.client.Do(req)
	if err != nil {
		return errors.New("协议服务暂时无法连接")
	}
	defer res.Body.Close()
	var e envelope
	if json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&e) != nil {
		return errors.New("协议响应无法解析")
	}
	success := e.Code == 0
	if a.kind == "wechat" {
		success = e.Success
		if strings.Contains(path, "LoginGetQRCar") {
			success = e.Success && e.Code == 1
		}
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 || !success {
		return &callError{res.StatusCode, e.Code}
	}
	if out != nil {
		if err = json.Unmarshal(e.Data, out); err != nil {
			return errors.New("协议响应字段无效")
		}
	}
	return nil
}
func localClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: &http.Transport{Proxy: nil}, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("协议服务不允许重定向") }}
}

func adapt(a *adapter) Protocol {
	if a.kind == "yyb" {
		return &yybAdapter{a}
	}
	return &wechatAdapter{a}
}
