package protocols

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
)

type wechatAdapter struct{ *adapter }

var _ Protocol = (*wechatAdapter)(nil)
var _ AccountProfileReader = (*wechatAdapter)(nil)
var _ AutoHeartbeatController = (*wechatAdapter)(nil)

func (a *wechatAdapter) GetAccountProfile(ctx context.Context, acc Account) (json.RawMessage, error) {
	var data json.RawMessage
	err := a.call(ctx, "POST", "/api/User/GetContractProfile?wxid="+url.QueryEscape(acc.Ref), nil, &data)
	return data, err
}

func (a *wechatAdapter) SetAutoHeartbeat(ctx context.Context, acc Account, enabled bool) error {
	path := "/api/Login/CloseAutoHeartBeat?wxid="
	if enabled {
		path = "/api/Login/AutoHeartBeat?wxid="
	}
	return a.call(ctx, "POST", path+url.QueryEscape(acc.Ref), nil, nil)
}

func (a *wechatAdapter) Begin(ctx context.Context) (Scan, error) {
	p := map[string]string{"ProxyIp": "", "ProxyUser": "", "ProxyPassword": ""}
	if a.proxy != "" {
		u, _ := url.Parse(a.proxy)
		p["ProxyIp"] = u.Scheme + "://" + u.Host
		if u.User != nil {
			p["ProxyUser"] = u.User.Username()
			p["ProxyPassword"], _ = u.User.Password()
		}
	}
	var d struct {
		UUID     string
		QrBase64 string
	}
	err := a.call(ctx, "POST", "/api/Login/LoginGetQRCar", map[string]any{"DeviceID": "", "DeviceName": "Robot", "LoginType": "", "Proxy": p}, &d)
	return Scan{ID: d.UUID, Image: d.QrBase64, Status: "pending"}, err
}
func (a *wechatAdapter) Poll(ctx context.Context, id string) (Scan, error) {
	s := Scan{ID: id, Status: "pending"}
	var d struct {
		Status       int
		NickName     string
		HeadImgUrl   string
		AcctSectResp struct{ UserName, NickName string }
	}
	err := a.call(ctx, "POST", "/api/Login/LoginCheckQR?uuid="+url.QueryEscape(id), nil, &d)
	var ce *callError
	if errors.As(err, &ce) && ce.code == -3 {
		s.Status = "verification"
		return s, nil
	}
	if err != nil {
		return s, err
	}
	s.Avatar = d.HeadImgUrl
	if d.AcctSectResp.UserName != "" {
		s.Status = "confirmed"
		s.Account = &Account{Ref: d.AcctSectResp.UserName, Nickname: d.AcctSectResp.NickName, Avatar: d.HeadImgUrl}
	} else {
		switch d.Status {
		case 1:
			s.Status = "scanned"
		case 3:
			s.Status = "expired"
		case 4:
			s.Status = "cancelled"
		}
	}
	return s, nil
}
func (a *wechatAdapter) Maintain(ctx context.Context, acc Account) (string, error) {
	var local struct{ Status string }
	if err := a.call(ctx, "GET", "/robot/account?wxid="+url.QueryEscape(acc.Ref), nil, &local); err != nil {
		return "unknown", err
	}
	if local.Status == "expired" {
		return "expired", nil
	}
	if local.Status != "alive" {
		return "unknown", errors.New("账号存储响应无效")
	}
	var d struct{ BaseResponse *struct{ Ret *int } }
	err := a.call(ctx, "POST", "/api/Login/HeartBeat?wxid="+url.QueryEscape(acc.Ref), nil, &d)
	// -8 includes transport and deserialization failures: never erase credentials for it.
	if err != nil {
		return "unknown", err
	}
	if d.BaseResponse == nil || d.BaseResponse.Ret == nil {
		return "unknown", errors.New("心跳响应缺少状态字段")
	}
	if *d.BaseResponse.Ret != 0 {
		return "expired", nil
	}
	return "alive", nil
}
func (a *wechatAdapter) Logout(ctx context.Context, acc Account) error {
	var local struct{ Status string }
	if err := a.call(ctx, "GET", "/robot/account?wxid="+url.QueryEscape(acc.Ref), nil, &local); err != nil {
		return err
	}
	if local.Status == "expired" {
		return nil
	}
	return a.call(ctx, "POST", "/api/Login/LogOut?wxid="+url.QueryEscape(acc.Ref), nil, nil)
}
