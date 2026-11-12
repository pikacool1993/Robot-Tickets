package business

import (
	"context"
	"errors"
)

// Refresh authenticates only when necessary, and replays the read request once on HTTP 401.
func (c *Client) Refresh(ctx context.Context, cfg Config, deviceID string, session Session, getCode func(context.Context) (string, error), log func(string, string)) (Session, []Person, error) {
	if err := cfg.Validate(); err != nil {
		return Session{}, nil, err
	}
	login := func() error {
		for attempt := 0; attempt < 2; attempt++ {
			log("info", "开始获取小程序授权码")
			var code string
			err := Retry(ctx, c.delay, func() error { var e error; code, e = getCode(ctx); return e })
			if err != nil {
				return errors.New("小程序取码失败，请检查协议账号后重试")
			}
			log("success", "小程序取码成功，开始登录业务系统")
			session, err = c.Login(ctx, cfg, deviceID, code)
			code = ""
			if err == nil {
				log("success", "业务登录成功")
				return nil
			}
			if !IsCodeExpired(err) || attempt == 1 {
				return err
			}
		}
		return errors.New("业务登录失败")
	}
	if session.Token == "" {
		if err := login(); err != nil {
			return session, nil, err
		}
	}
	log("info", "开始获取观演人")
	people, err := c.People(ctx, cfg, session)
	if IsTokenExpired(err) {
		session = Session{}
		if err = login(); err == nil {
			people, err = c.People(ctx, cfg, session)
		}
	}
	if IsTokenExpired(err) {
		session = Session{}
	}
	return session, people, err
}
