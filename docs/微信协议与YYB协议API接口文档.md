# Football 协议接口参考

> **范围**：Football 当前实际调用的 YYB、WeChat、CDN3、CDN5 接口。  
> **依据**：Football 客户端实现与工程内协议源码。  
> **更新时间**：2026-10-31

## 目录

- [1. 服务总览](#1-服务总览)
- [2. 统一约定](#2-统一约定)
- [3. YYB 协议](#3-yyb-协议)
- [4. WeChat 协议](#4-wechat-协议)
- [5. CDN3 / CDN5 蓉城接口](#5-cdn3--cdn5-蓉城接口)
- [6. 典型流程](#6-典型流程)
- [7. 安全注意事项](#7-安全注意事项)
- [8. 源码索引](#8-源码索引)

本文是工程内部参考，不是微信官方 API 文档。字段和业务码以当前构建版本源码为准。

## 1. 服务总览

| 服务 | 默认地址 | 职责 | 客户端 |
|---|---|---|---|
| YYB Go | `http://127.0.0.1:8063` | 扫码登录、账号、小程序 | `YybClient` |
| WeChat `wechat_874` | `http://127.0.0.1:8062` | 微信登录、心跳、小程序 | `WeChatClient` |
| CDN3 | `https://fccdn1.k4n.cc/` | 蓉城登录、票区、账号和订单 | `RongChengCdn1Client` |
| CDN5 | `https://fccdn5.k4n.cc/` | 蓉城下单 | `RongChengCdn5Client` |

YYB/WeChat 由 Host 管理启动；CDN 请求通过代理客户端发送。

| 服务 | 健康检查 | 成功结果 |
|---|---|---|
| YYB | `GET /health` | HTTP 2xx，`ok=true` |
| WeChat | `GET /health` | HTTP 2xx，`ok=true`、`service=wechat_874` |

## 2. 统一约定

### 响应格式

| 协议 | 外层结构 | 成功判断 |
|---|---|---|
| YYB | `{code,msg,data}` | HTTP 2xx 且 `code == 0` |
| WeChat | `{code,success,message,data}` | 通常 `success == true` |
| CDN3/CDN5 | 业务对象含 `code`、`msg` | HTTP 2xx 且 `code` 为 `1` 或 `200` |

### 源码业务码速查

本表记录协议源码显式定义或透传的码值，不采用 Football 客户端的成功判定规则。不同层级的码不可互换。

| 协议 | 业务码 | 含义 |
|---|---:|---|
| YYB HTTP 封装 | `0` | 成功响应外层 `code` |
| YYB HTTP 封装 | `400` | 请求参数或 JSON 错误 |
| YYB HTTP 封装 | `404` | 路由、会话或账号不存在 |
| YYB HTTP 封装 | `405` | HTTP 方法不支持 |
| YYB HTTP 封装 | `409` | 扫码授权缓冲尚未就绪 |
| YYB HTTP 封装 | `500` | 服务内部错误 |
| YYB HTTP 封装 | `502` | 上游微信协议调用失败 |
| YYB wxapp 操作目录 | `1001` | `getCode` 操作码 |
| YYB wxapp 操作目录 | `1002` | `getPhoneNumber` 操作码 |
| YYB wxapp 操作目录 | `1003` | `operateWxData` 操作码 |
| WeChat API | `1` | 车机二维码接口成功创建二维码 |
| WeChat API | `0` | 扫码确认、心跳、退出、小程序 JSLogin/CloudCallFunction 等成功分支 |
| WeChat API | `-8` | 会话/账号缺失、协议传输、解包或反序列化等通用错误；控制器 JSON 解析失败也使用此码 |
| WeChat API | `-9` | 取二维码过于频繁（上游 ret 成功但 UUID 为空） |
| WeChat API | `-3` | 扫码登录要求提交验证码并生成验证链接 |
| WeChat API | `-2` | 扫码通知数据解包异常 |
| WeChat API | `errtype` | `comm.SendRequest` 传输失败时透传的错误类型 |
| CDN3/CDN5 | `1`、`200` | 客户端定义的成功码 |
| CDN3/CDN5 | 其他 | 服务端动态业务码，结合 `msg` 判断 |

## 3. YYB 协议

### 接口清单

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/health` | 健康检查 |
| `POST` | `/qr`、`/qr?as_base64=true` | 创建扫码会话 |
| `GET` | `/qr/{session_id}/poll` | 轮询扫码状态 |
| `POST` | `/qr/{session_id}/confirm` | 保存登录账号 |
| `GET` | `/accounts` | 账号列表 |
| `POST` | `/accounts/refresh` | 刷新账号登录态 |
| `DELETE` | `/accounts?ref={ref}` | 删除账号 |
| `POST` | `/wxapp/getCode` | 小程序 code |
| `POST` | `/wxapp/operateWxData` | 小程序业务调用 |

### 创建与确认扫码

**`POST /qr`**。使用 `as_base64=true` 时返回二维码 data URI。

```json
{"code":0,"msg":"success","data":{"session_id":"SESSION_ID","status":"pending","image_url":"/qr/SESSION_ID/image","image_base64":null}}
```

Football 使用 `session_id`、`status`、`image_url`、`image_base64`。随后调用 `GET /qr/{session_id}/poll`，再调用 `POST /qr/{session_id}/confirm`。

轮询 `data.errcode` 映射：`408=pending`、`404=scanned`、`403=cancelled`、`402=expired`、`405+授权码=authorized`；登录缓冲已存在时为 `confirmed`。这些是 YYB 二维码轮询解析出的上游状态码，与 YYB HTTP 外层 `code` 无关。

### 账号

`GET /accounts` 返回账号数组，使用字段：`id`、`openid`、`uin`、`nickname`、`avatar`、`status`。

`POST /accounts/refresh` 请求体：

```json
{"ref":"账号ID、UIN或openid"}
```

读取 `data.id`、`data.openid`、`data.status`。账号不存在的 `404` 映射为 `expired`。`DELETE /accounts?ref={ref}` 删除账号。

**YYB 是否有类似 WeChat HeartBeat 的接口？** YYB 没有独立的 `HeartBeat` 路由。功能上最接近的是 `POST /accounts/refresh`：它使用已保存凭据刷新 login buffer，成功后更新凭据并标记账号 `alive`，失败则标记 `expired`。因此它兼具登录态检查和凭据续期；WeChat `/HeartBeat` 发送微信心跳协议包验证现有会话。两者目的相近，实现并不相同。`/accounts/refresh` 不传 `ref` 时会批量处理全部账号。

### 小程序

`POST /wxapp/getCode`：

```json
{"ref":"账号ID、UIN或openid","app_id":"wx..."}
```

读取 `data.openid`、`data.result.code`，错误提示兼容 `errMsg`、`err_msg`、`errmsg`。

`POST /wxapp/operateWxData`：

```json
{"ref":"账号ID、UIN或openid","app_id":"wx...","payload":{}}
```

读取 `data.openid` 和 `data.result`。

## 4. WeChat 协议

### 接口清单

| 方法 | 路径 | 说明 |
|---|---|---|
| `GET` | `/health` | 健康检查 |
| `POST` | `/api/Login/LoginGetQRCar` | 创建二维码 |
| `POST` | `/api/Login/LoginCheckQR?uuid={uuid}` | 轮询二维码 |
| `POST` | `/api/Login/HeartBeat?wxid={wxid}` | 微信心跳 |
| `POST` | `/api/Login/LogOut?wxid={wxid}` | 退出登录 |
| `POST` | `/api/Wxapp/JSLogin` | 小程序 code |
| `POST` | `/api/Wxapp/CloudCallFunction` | 云函数 |

### 登录与心跳

**`POST /api/Login/LoginGetQRCar`**

```json
{"DeviceID":"","DeviceName":"","LoginType":"","Proxy":{"ProxyIp":"","ProxyUser":"","ProxyPassword":""}}
```

源码业务码：`1` 表示取码成功；`-8` 表示 MMTLS 初始化、请求、解包或反序列化异常；`-9` 表示上游 ret 成功但 UUID 为空，即取码过于频繁；`0`（源码写作 `-0`）表示未知错误。读取 `data.uuid`、`qrBase64`、`qrUrl`、`expiredTime`。

**`POST /api/Login/LoginCheckQR?uuid={uuid}`** 无请求体。读取 `data.status`、`nickName`、`headImgUrl`；`data.acctSectResp.userName` 非空时映射为 `confirmed` 并作为 wxid。`data.status` 是二维码状态，不是外层业务码。

**`POST /api/Login/HeartBeat?wxid={wxid}`** 无请求体。源码成功分支返回 `code=0`；账号不存在或内部处理异常返回 `-8`；`comm.SendRequest` 失败时可能返回底层 `errtype`。响应 `data` 为微信 `HeartBeatResponse`。该调用确认现有登录会话和通信状态仍可用；失败时调用方应视为离线，并按业务策略重新登录。

**`POST /api/Login/LogOut?wxid={wxid}`** 无请求体。源码成功分支为 `0`；账号缺失、请求序列化或响应反序列化失败为 `-8`；协议传输失败透传 `errtype`。成功后服务端执行微信登出，不应继续对该 wxid 使用原会话。

### 小程序

`POST /api/Wxapp/JSLogin`：

```json
{"Appid":"wx...","Wxid":"wxid_xxx"}
```

外层源码成功码为 `0`；账号/会话缺失、序列化或反序列化错误为 `-8`；协议传输错误透传 `errtype`。读取 `data.code` 和可选的 `data.jsapiBaseresponse.errmsg`。`data.code` 是小程序登录凭据，不是外层业务码；业务方用它继续建立目标小程序会话。

`POST /api/Wxapp/CloudCallFunction`：

```json
{"Appid":"wx...","Data":"云函数调用数据字符串","Wxid":"wxid_xxx"}
```

外层源码成功码为 `0`；账号/会话缺失、序列化或反序列化错误为 `-8`；协议传输错误透传 `errtype`。读取 `data.data` 字符串作为云函数结果；该结果可承载 Football 后续使用的加密参数、库存等业务数据，内部结构由目标云函数决定。

## 5. CDN3 / CDN5 蓉城接口

### 请求约定

- CDN3：`https://fccdn1.k4n.cc/`；CDN5：`https://fccdn5.k4n.cc/`。
- 登录后的 CDN3 接口携带 `Authorization: Bearer {token}`。
- 使用工程配置的设备 User-Agent、微信小程序 Referer 和代理池。
- HTTP 非 2xx 或 JSON 解析失败时，客户端将 `Success=false`。

### CDN3 接口

| 方法 | 路径 | 请求数据 | 用途 |
|---|---|---|---|
| `POST` | `/fc/wx_api/v1/Login/index` | 登录 JSON | 获取 token、uid |
| `POST` | `/fc/wx_api/v1/Member/getCrewsList?lid2={uid}` | 空或 `{}` | 获取观赛人 |
| `POST` | `/fc/wx_api/v1/Member/getUserInfo?lid2={uid}` | `{}` | 获取用户资料 |
| `POST` | `/fc/wx_api/v1/Member/updateUserInfo?lid2={uid}` | 加密资料 | 更新资料 |
| `POST` | `/fc/wx_api/v1/MatchCon/getBillRegion?lid2={uid}` | `{"id":"{matchId}"}` | 获取票区库存 |
| `POST` | `/fc/wx_api/v1/MatchOrder/getMatchOrderInfo?lid2={uid}` | `{"id":"{orderId}"}` | 查询订单 |
| `POST` | `/fc/wx_api/v1/MatchOrder/cancelOrder?lid2={uid}` | `{"order_id":{orderId}}` | 取消订单 |
| `POST` | `/fc/wx_api/v1/MatchOrder/refundOrder?lid2={uid}` | `{"order_id":{orderId}}` | 退款 |

成功业务码为 `1` 或 `200`。登录请求核心字段为 `code`、`s.deviceId`，成功读取 `token`、`info.id`。票区响应读取 `btnStatus`、`btnText`、`maxNum`、`typeCode` 和 `data`。

资料更新请求：

```json
{"encryptedData":"小写十六进制密文","version":1,"expireTime":1700000000000}
```

工程使用 AES-CBC/PKCS7 加密资料 JSON，密钥为 Base64，IV 为 UTF-8 字符串。

### CDN5 下单

**`POST /fc/wx_api/v1/MatchCon/createMatchOrder`**

```text
/fc/wx_api/v1/MatchCon/createMatchOrder?lid2={uid}&s_time={unix秒}&s_sign={md5签名}
```

请求体：

```json
{"encryptedData":"小写十六进制密文","version":1,"expireTime":1700000000000,"agree":true,"id":"比赛或场次ID","regions":[],"users":[]}
```

签名：

```text
s_sign = lowercase_md5("Y6xkC7b5" + s_time + authCode + uid)
```

响应读取 `code`、`msg`、`timeout`、`data`；最终下单结果结合业务码和消息判断。

## 6. 典型流程

```text
YYB：/qr -> /qr/{session_id}/poll -> /qr/{session_id}/confirm -> wxapp
WeChat：LoginGetQRCar -> LoginCheckQR -> HeartBeat
蓉城：CDN3 Login -> 观赛人/票区 -> CDN5 createMatchOrder -> CDN3 查询订单
```

## 7. 安全注意事项

- token、wxid、openid、设备数据、验证码、AES 密钥、IV、授权码和订单密文均为敏感数据。
- YYB/WeChat 默认绑定回环地址；跨机器访问时必须增加访问控制。
- 禁止在日志或文档中记录真实二维码 Base64、身份证、手机号、token 和支付数据。
- 协议源码升级后，应重新核对业务码和字段。

## 8. 源码索引

| 内容 | 文件 |
|---|---|
| YYB 客户端 | `src/Football.Infrastructure/Yyb/YybClient.cs` |
| YYB 服务与二维码码表 | `YYB/internal/httpapi/app.go`、`YYB/internal/qr/qr.go` |
| WeChat 客户端 | `src/Football.Infrastructure/WeChat/WeChatClient.cs` |
| WeChat 路由与控制器 | `874ws--08/wechatReal08/routers/router.go`、`controllers/Login.go`、`controllers/Wxapp.go` |
| CDN3 客户端 | `src/Football.Infrastructure/RongCheng/RongChengCdn1Client.cs` |
| CDN5 客户端 | `src/Football.Infrastructure/RongCheng/RongChengCdn5Client.cs` |
| CDN 请求头 | `src/Football.Infrastructure/RongCheng/RongChengRequestHeaders.cs` |
| CDN5 加密签名 | `src/Football.Infrastructure/RongCheng/RongChengOrderCrypto.cs` |

