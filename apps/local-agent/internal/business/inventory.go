package business

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
)

// Stock returns one complete response; callers own polling and cancellation.
func (c *Client) Stock(ctx context.Context, cfg Config, s Session, matchID string) ([]byte, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if s.Token == "" || s.UserID == "" {
		return nil, errors.New("业务账号未登录")
	}
	var out json.RawMessage
	err := c.call(ctx, cfg, "/fc/wx_api/v1/MatchCon/getBillRegion?lid2="+url.QueryEscape(s.UserID), s.Token, map[string]string{"id": matchID}, &out)
	return out, err
}
