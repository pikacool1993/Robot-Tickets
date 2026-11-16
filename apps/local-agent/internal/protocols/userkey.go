package protocols

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"robot/apps/local-agent/internal/userkey"
	"time"
)

func (m *Manager) keyBindingValid(id int64, expected Binding) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	b, ok := m.doc.Bindings[id]
	return ok && b.Protocol == expected.Protocol && b.Account.Ref == expected.Account.Ref && b.Generation == expected.Generation && b.Status == "alive" && !b.Paused && m.active == expected.Protocol && m.state[expected.Protocol] == "running"
}

func (m *Manager) UserEncryptionKey(ctx context.Context, id int64, expected Binding, appID string) (userkey.Material, error) {
	if !m.keyBindingValid(id, expected) {
		return userkey.Material{}, errors.New("协议账号状态已变化")
	}
	lock := m.accountLock(expected.Protocol, expected.Account.Ref)
	lock.Lock()
	defer lock.Unlock()
	if ctx.Err() != nil {
		return userkey.Material{}, ctx.Err()
	}
	if !m.keyBindingValid(id, expected) {
		return userkey.Material{}, errors.New("协议账号状态已变化")
	}
	a := m.client(expected.Protocol)
	defer a.client.CloseIdleConnections()
	v, err := a.encryptionKey(ctx, expected.Account.Ref, appID)
	if ctx.Err() != nil {
		return userkey.Material{}, ctx.Err()
	}
	if !m.keyBindingValid(id, expected) {
		return userkey.Material{}, errors.New("协议账号状态已变化")
	}
	return v, err
}

func (a *adapter) encryptionKey(ctx context.Context, ref, appID string) (userkey.Material, error) {
	payload := map[string]any{"api_name": "webapi_getuserencryptkey", "data": map[string]any{}, "env": 1, "operate_directly": false, "tid": time.Now().UnixMilli()}
	var result struct {
		ErrNo *int   `json:"err_no"`
		Data  string `json:"data"`
	}
	if a.kind == "yyb" {
		var out struct {
			Result json.RawMessage `json:"result"`
		}
		if err := a.call(ctx, "POST", "/wxapp/operateWxData", map[string]any{"ref": ref, "app_id": appID, "payload": payload}, &out); err != nil {
			return userkey.Material{}, err
		}
		if json.Unmarshal(out.Result, &result) != nil {
			return userkey.Material{}, errors.New("用户加密参数响应无法解析")
		}
	} else {
		data, _ := json.Marshal(payload)
		var out struct {
			Data         string `json:"data"`
			BaseResponse *struct {
				Ret *int `json:"ret"`
			} `json:"BaseResponse"`
		}
		if err := a.call(ctx, "POST", "/api/Wxapp/CloudCallFunction", map[string]any{"Wxid": ref, "Appid": appID, "Data": string(data)}, &out); err != nil {
			return userkey.Material{}, err
		}
		if out.BaseResponse != nil && (out.BaseResponse.Ret == nil || *out.BaseResponse.Ret != 0) {
			return userkey.Material{}, errors.New("用户加密参数请求失败")
		}
		raw, err := base64.StdEncoding.DecodeString(out.Data)
		if err != nil || json.Unmarshal(raw, &result) != nil {
			return userkey.Material{}, errors.New("用户加密参数响应无法解析")
		}
	}
	if result.ErrNo == nil || *result.ErrNo != 0 {
		return userkey.Material{}, errors.New("用户加密参数业务状态无效")
	}
	return userkey.Parse([]byte(result.Data), time.Now())
}
