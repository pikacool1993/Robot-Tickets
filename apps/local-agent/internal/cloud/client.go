package cloud

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	protocol "robot/apps/cloud-server/pkg/clientprotocol"
	"robot/apps/local-agent/internal/business"
	"time"
)

type Client struct {
	config protocol.Config
	keys   protocol.Keyring
	http   *http.Client
}
type APIError struct {
	Status int
	Code   string
}

func (e *APIError) Error() string {
	switch e.Code {
	case "invalid_credentials":
		return "用户名或密码错误，或账号当前不可用"
	case "client_number_conflict":
		return "客户端编号已被占用，请更换编号"
	case "device_limit_reached":
		return "已达到设备数量上限"
	case "invalid_input":
		return "客户端编号或实例信息无效"
	case "unauthorized":
		return "登录已失效，请重新登录"
	}
	return "服务端暂时无法处理请求，请稍后重试"
}

type LoginInput struct {
	Username     string `json:"username"`
	Password     string `json:"password"`
	ClientNumber string `json:"client_number"`
	InstanceID   string `json:"client_instance_id"`
}
type Session struct {
	BusinessConfig business.Config `json:"business_config"`
	Token          string          `json:"access_token"`
	ExpiresAt      time.Time       `json:"expires_at"`
	Agent          struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"agent"`
	ClientNumber string `json:"client_number"`
	InstanceID   string `json:"client_instance_id"`
}

func New(c protocol.Config) (*Client, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	key, _ := base64.StdEncoding.DecodeString(c.TransportKey)
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &Client{c, protocol.Keyring{c.TransportKeyID: key}, &http.Client{Timeout: 15 * time.Second, Transport: transport, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) Close() { c.http.CloseIdleConnections() }
func (c *Client) Call(ctx context.Context, method, path, token string, in, out any) error {
	var body io.Reader
	if in != nil {
		plain, err := json.Marshal(in)
		if err != nil {
			return err
		}
		env, err := c.keys.Seal(c.config.TransportKeyID, plain)
		if err != nil {
			return err
		}
		data, err := json.Marshal(env)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.config.ServerURL+"/api/agent/v1"+path, body)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("无法连接服务端或请求超时，请检查网络；登录超时后可使用同一实例重试")
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, (1<<20)+1))
	if err != nil || len(data) > 1<<20 {
		return errors.New("服务端响应读取失败")
	}
	var env protocol.Envelope
	if err := json.Unmarshal(data, &env); err != nil || env.KeyID != c.config.TransportKeyID {
		return errors.New("服务端响应格式错误，请检查通信配置")
	}
	plain, err := c.keys.Open(env)
	if err != nil {
		return errors.New("服务端响应解密失败，请重新导入正确的通信配置")
	}
	var payload struct {
		Code string          `json:"code"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(plain, &payload); err != nil {
		return errors.New("服务端响应内容无效")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || payload.Code != "ok" {
		return &APIError{resp.StatusCode, payload.Code}
	}
	if out != nil {
		if err := json.Unmarshal(payload.Data, out); err != nil {
			return errors.New("服务端响应内容不完整")
		}
	}
	return nil
}
func (c *Client) Login(ctx context.Context, in LoginInput) (Session, error) {
	var s Session
	err := c.Call(ctx, "POST", "/auth/login", "", in, &s)
	if err == nil && (s.Token == "" || !s.ExpiresAt.After(time.Now()) || s.InstanceID != in.InstanceID || s.ClientNumber != in.ClientNumber) {
		err = errors.New("登录响应会话信息无效")
	}
	return s, err
}
