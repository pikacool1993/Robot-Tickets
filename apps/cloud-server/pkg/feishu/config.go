package feishu

import (
	"errors"
	"net/url"
	"regexp"
	"strings"
)

type Config struct {
	Enabled      bool   `json:"enabled"`
	Webhook      string `json:"webhook"`
	OrderCreated bool   `json:"order_created"`
	AuthExpired  bool   `json:"auth_expired"`
	OrderBlocked bool   `json:"order_blocked"`
	Revision     uint64 `json:"revision"`
}

func Default() Config {
	return Config{Enabled: true, OrderCreated: true, AuthExpired: true, OrderBlocked: true}
}

var hookPath = regexp.MustCompile(`^/open-apis/bot/v2/hook/[A-Za-z0-9-]{8,128}$`)

func ValidateWebhook(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Host != "open.feishu.cn" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || !hookPath.MatchString(u.Path) || u.RawPath != "" {
		return errors.New("请输入有效的飞书机器人 Webhook（https://open.feishu.cn/open-apis/bot/v2/hook/…）")
	}
	return nil
}

func (c Config) Allows(kind string) bool {
	if !c.Enabled || c.Webhook == "" {
		return false
	}
	switch kind {
	case "order_created":
		return c.OrderCreated
	case "auth_expired":
		return c.AuthExpired
	case "order_blocked":
		return c.OrderBlocked
	case "test":
		return true
	}
	return false
}

func (c Config) MaskedWebhook() string {
	if c.Webhook == "" {
		return ""
	}
	tail := c.Webhook[strings.LastIndex(c.Webhook, "/")+1:]
	if len(tail) > 4 {
		tail = tail[len(tail)-4:]
	}
	return "https://open.feishu.cn/…/****" + tail
}
