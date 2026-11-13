package clientprotocol

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

// MatchConfig is a login-time snapshot shared by all agents.
type MatchConfig struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	MatchAt  time.Time `json:"match_at"`
	SaleAt   time.Time `json:"sale_at"`
	Enabled  bool      `json:"enabled"`
	Revision uint64    `json:"revision"`
	Message  string    `json:"message,omitempty"`
}

func (m MatchConfig) Validate() error {
	n, err := strconv.ParseUint(m.ID, 10, 64)
	if err != nil || n == 0 || strings.TrimSpace(m.Name) == "" || len(m.Name) > 200 || m.MatchAt.IsZero() || m.SaleAt.IsZero() || !m.SaleAt.Before(m.MatchAt) {
		return errors.New("请填写有效场次ID、名称和时间，开售时间须早于比赛时间")
	}
	return nil
}

func (m MatchConfig) Ready() error {
	if err := m.Validate(); err != nil {
		return err
	}
	if !m.Enabled {
		return errors.New("当前比赛未启用，请联系管理员后重新登录")
	}
	return nil
}
