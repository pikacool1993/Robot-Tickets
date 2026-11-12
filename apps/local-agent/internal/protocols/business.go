package protocols

import (
	"context"
	"errors"
	"net/url"
)

// MiniProgramCode returns only the short-lived code, never the surrounding keys.
func (m *Manager) MiniProgramCode(ctx context.Context, id int64, expected Binding, appID string) (string, error) {
	m.mu.Lock()
	current, ok := m.doc.Bindings[id]
	active := m.active
	running := m.state[expected.Protocol] == "running"
	m.mu.Unlock()
	if !ok || current.Protocol != expected.Protocol || current.Account.Ref != expected.Account.Ref || current.Generation != expected.Generation || current.Status != "alive" || current.Paused || active != expected.Protocol || !running {
		return "", errors.New("协议账号状态已变化，请检查账号后重试")
	}
	a := m.client(expected.Protocol)
	lock := m.accountLock(expected.Protocol, expected.Account.Ref)
	lock.Lock()
	defer lock.Unlock()
	if expected.Protocol == "yyb" {
		var out struct {
			Result struct {
				Code   string `json:"code"`
				ErrMsg string `json:"errMsg"`
			} `json:"result"`
		}
		if err := a.call(ctx, "POST", "/wxapp/getCode", map[string]string{"ref": expected.Account.Ref, "app_id": appID}, &out); err != nil {
			return "", err
		}
		if out.Result.Code == "" || out.Result.ErrMsg != "login:ok" {
			return "", errors.New("协议未返回有效小程序授权码")
		}
		return out.Result.Code, nil
	}
	var out struct {
		JSAPI *struct {
			ErrCode *int `json:"errcode"`
		} `json:"jsapiBaseresponse"`
		Code         string `json:"code"`
		BaseResponse *struct {
			Ret *int `json:"ret"`
		} `json:"BaseResponse"`
	}
	if err := a.call(ctx, "POST", "/api/Wxapp/JSLogin?wxid="+url.QueryEscape(expected.Account.Ref), map[string]string{"Wxid": expected.Account.Ref, "Appid": appID}, &out); err != nil {
		return "", err
	}
	if out.Code == "" || (out.JSAPI != nil && out.JSAPI.ErrCode != nil && *out.JSAPI.ErrCode != 0) || out.BaseResponse == nil || out.BaseResponse.Ret == nil || *out.BaseResponse.Ret != 0 {
		return "", errors.New("协议未返回有效小程序授权码")
	}
	return out.Code, nil
}

// ResumeTask only clears a persisted pause after the active protocol has confirmed
// the same binding online. Stopped, unknown and expired accounts stay paused.
func (m *Manager) ResumeTask(id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.doc.Bindings[id]
	if !ok {
		return errors.New("任务尚未绑定协议账号")
	}
	if m.active != b.Protocol || m.state[b.Protocol] != "running" || m.manual[b.Protocol] || m.workCtx == nil || m.workCtx.Err() != nil {
		return errors.New("请先启动当前协议并等待账号检查完成")
	}
	if b.Status != "alive" {
		return errors.New("协议账号尚未确认在线，请等待检查；失效账号需重新扫码")
	}
	if !b.Paused {
		return nil
	}
	previous := b
	b.Paused = false
	m.doc.Bindings[id] = b
	if err := m.saveLocked(); err != nil {
		m.doc.Bindings[id] = previous
		return err
	}
	m.taskLog(id, "success", "协议账号在线，任务已恢复，开始刷新业务登录和观演人")
	return nil
}
