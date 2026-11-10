# 微信与 YYB 协议服务 API 参考

> 范围：本仓库 `protocol-services/wechat`、`protocol-services/yyb` 的协议服务 API，以及外部 RongCheng C# HTTP client 暴露的对应调用。  
> 更新日期：2026-11-08。以源码/API 文档为准；升级协议服务后应复核端点、字段和业务码。

## 服务和端口

| 服务 | 本仓库默认端口 | 外部 C# client 默认值 | 说明 |
|---|---:|---:|---|
| YYB Go | `8088` | `18080` | YYB 可通过 `-port` 指定；local-agent 固定使用 8088 |
| WeChat | `8062` | 用户配置 `wechat_port` | local-agent 管理模式可通过环境变量指定端口 |

YYB C# client 的 `BaseUrl` 与本仓库 YYB 可执行程序默认端口不同。使用该 C# client 调用本仓库构建的 YYB 时，需将客户端地址改为实际端口。

## 响应和错误

YYB 的 JSON API 外层为 `{code,msg,data}`，成功时 HTTP 2xx 且 `code == 0`。`GET /health` 的 `{ok:true}` 位于 `data` 中；`/accounts/avatar` 返回图片或 HTTP 重定向，不是 JSON 信封。

WeChat 普通业务 API 外层由 Go struct 序列化，字段通常为 `Code`、`Success`、`Message`、`Data` 等。多数成功分支为 `Code == 0 && Success == true`；车机二维码创建成功为 `Code == 1 && Success == true`。协议响应中的状态码、二维码状态和微信 protobuf `BaseResponse.Ret` 是不同层级。

| 协议层错误码 | 含义 |
|---|---|
| YYB `400` | 参数或 JSON 无效 |
| YYB `404` | 路由、二维码会话或账号不存在 |
| YYB `405` | HTTP 方法不支持 |
| YYB `409` | 扫码授权缓冲尚未就绪 |
| YYB `500` | 服务内部错误 |
| YYB `502` | 上游微信协议调用失败 |
| WeChat `-8` | 账号/会话、传输、解包、序列化等通用错误；需结合 `Message` 判断 |
| WeChat `-9` | 获取二维码过于频繁，UUID 为空 |
| WeChat `-3` | 需要额外扫码验证 |
| WeChat `-2` | 扫码通知解包异常 |
| WeChat `errtype` | 底层协议请求错误透传 |

## YYB API

### 端点

| 方法 | 路径 | 用途 |
|---|---|---|
| `GET` | `/health` | 服务健康状态 |
| `POST` | `/qr?as_base64=true` | 创建二维码会话 |
| `GET` | `/qr/{session_id}/image` | 获取二维码图片 |
| `GET` | `/qr/{session_id}/poll` | 查询扫码状态 |
| `POST` | `/qr/{session_id}/confirm` | 取得登录缓冲并保存账号 |
| `GET` | `/accounts` | 列出账号 |
| `DELETE` | `/accounts?ref={ref}` | 删除账号 |
| `POST` | `/accounts/refresh` | 检查存活并刷新登录凭据 |
| `POST` | `/accounts/resync` | 重新同步账号资料 |
| `GET` | `/accounts/avatar?ref={ref}` | 获取账号头像 |
| `POST` | `/wxapp/getCode` | 获取小程序授权码 |
| `POST` | `/wxapp/getPhoneNumber` | 获取授权手机号 |
| `POST` | `/wxapp/operateWxData` | 调用小程序云函数 |

扫码请求顺序为：

```text
POST /qr?as_base64=true
GET  /qr/{session_id}/poll
POST /qr/{session_id}/confirm  # poll 为 authorized 后调用
```

创建二维码响应示例：

```json
{
  "code": 0,
  "msg": "success",
  "data": {
    "session_id": "SESSION_ID",
    "status": "pending",
    "image_url": "/qr/SESSION_ID/image",
    "image_base64": "data:image/jpeg;base64,..."
  }
}
```

`as_base64=true` 时内嵌二维码 data URI；否则可用 `image_url` 请求图片。轮询 `data.status` 常见值为 `pending`、`scanned`、`authorized`、`confirmed`、`expired`、`cancelled`、`unknown`。`data.errcode` 是上游扫码状态：408 等待、404 已扫码、403 取消、402 过期、405 且带授权码表示授权完成。它与外层 HTTP/业务 `code` 不同。

确认接口返回账号公开对象，常用字段为 `id`、`openid`、`uin`、`nickname`、`avatar`、`status`。`openid` 是协议账号标识；`id` 是 YYB 本地数据库 ID。账号维护接口的 `ref` 可解析为 ID、UIN 或 openid，客户端绑定宜保存 `openid`。

确认成功后的公开账号结果在协议进程内保留五分钟，从首次确认成功开始计时。期间重复 poll 返回 `confirmed`，重复 confirm 返回同一账号结果，不重复执行上游登录和账号保存；重试不会延长保留时间。超过期限返回 404，创建新二维码时清理过期结果。缓存不包含登录凭据，不跨协议进程重启保留；二维码图片在首次确认成功后删除。

`POST /accounts/refresh` 和 `/accounts/resync` 接收可选请求体：

```json
{"ref":"openid 或其他可解析账号引用"}
```

不传 `ref` 表示批量操作全部账号。`refresh` 返回 `id`、`openid`、`uin`、`nickname`、`status`；`resync` 返回更新后的账号对象。`refresh` 兼具存活检查和凭据刷新，不是 WeChat `/HeartBeat` 的同类协议包。

头像端点接受账号 `ref`，返回图片，或在需要访问远端头像时重定向。

三个小程序接口的请求字段：

```json
{"ref":"openid","app_id":"wx..."}
```

`operateWxData` 还要求 `payload`：

```json
{"ref":"openid","app_id":"wx...","payload":{}}
```

响应通常为 `{code,msg,data:{openid,result}}`。操作目录码 `1001`、`1002`、`1003` 分别对应 `getCode`、`getPhoneNumber`、`operateWxData`；它们不是外层 HTTP 响应码。

## WeChat API

### 端点

| 方法 | 路径 | 用途 |
|---|---|---|
| `POST` | `/api/Login/LoginGetQRCar` | 创建车机登录二维码 |
| `POST` | `/api/Login/LoginCheckQR?uuid={uuid}` | 轮询二维码 |
| `POST` | `/api/Login/HeartBeat?wxid={wxid}` | 发送一次微信心跳 |
| `POST` | `/api/Login/HeartBeatLong?wxid={wxid}` | 长连接心跳；源码提供，C# client 未暴露 |
| `POST` | `/api/Login/AutoHeartBeat?wxid={wxid}` | 开启服务端自动心跳/自动二次登录 |
| `POST` | `/api/Login/CloseAutoHeartBeat?wxid={wxid}` | 关闭服务端自动心跳 |
| `POST` | `/api/Login/LogOut?wxid={wxid}` | 微信账号登出 |
| `POST` | `/api/User/GetContractProfile?wxid={wxid}` | 读取微信个人资料 |
| `POST` | `/api/Wxapp/JSLogin` | 获取小程序授权码 |
| `POST` | `/api/Wxapp/CloudCallFunction` | 调用小程序云函数 |

车机二维码请求体：

```json
{"DeviceID":"","DeviceName":"","LoginType":"","Proxy":{"ProxyIp":"","ProxyUser":"","ProxyPassword":""}}
```

成功外层 `Code=1`。二维码数据通常包含 `Uuid`、`QrBase64`、`QrUrl`、`ExpiredTime`。轮询请求无请求体；成功响应的数据字段由微信 protobuf 消息决定。账号确认成功时会带有 `acctSectResp.userName`，其值为 `wxid`。`-3` 表示需要额外验证，不能当作一般网络失败。

`HeartBeat` 和 `LogOut` 不带请求体，使用 `wxid` 查询参数。心跳响应的微信 `BaseResponse.Ret == 0` 表示该次请求成功；外层 `Code`、`Success` 也需一并检查。自动心跳接口会在协议服务进程内启动/停止后台心跳；local-agent 托管模式由客户端定时维护，不应同时启用两种心跳机制。

`GetContractProfile` 无请求体，响应 `Data` 为微信 `GetProfileResponse`，包括微信侧个人资料，不代表目标小程序业务授权已完成。

小程序授权码请求：

```json
{"Appid":"wx...","Wxid":"wxid_xxx"}
```

授权码位于外层 `Data.code`；外层 `Code` 是协议接口业务码。云函数请求：

```json
{"Appid":"wx...","Data":"云函数调用数据字符串","Wxid":"wxid_xxx"}
```

云函数结果位于 `Data.data`。其中内部载荷由目标小程序定义。

## local-agent 管理扩展

以下端点是本仓库为托管协议进程添加的控制接口，不属于上面的外部 C# 协议客户端接口：

| 协议 | 方法和路径 | 用途 |
|---|---|---|
| YYB / WeChat | `GET /robot/health` | 校验协议实例身份和进程 PID |
| WeChat | `GET /robot/account?wxid={wxid}` | 查询微信账号是否仍在 Redis 账号存储中 |

local-agent 管理模式通过 `Authorization: Bearer {ROBOT_PROTOCOL_TOKEN}` 保护 `/robot/` 端点。YYB 原生 `/health`、控制台和其他原生 API 沿用协议服务原有访问方式。WeChat 没有通用 `/health` 路由。

## 源码索引

| 内容 | 文件 |
|---|---|
| YYB 路由和 OpenAPI | `protocol-services/yyb/internal/httpapi/app.go`、`openapi.go` |
| YYB 扫码状态解析 | `protocol-services/yyb/internal/qr/qr.go` |
| WeChat 路由 | `protocol-services/wechat/routers/router.go` |
| WeChat 控制器 | `protocol-services/wechat/controllers/Login.go`、`User.go`、`Wxapp.go` |
| 本地 WeChat 管理路由 | `protocol-services/wechat/main.go` |
| RongCheng C# 客户端参考 | `E:\蓉城\Projects\RongCheng-WeChat\Network\HttpClient\WeChatHttpClient.cs`、`E:\蓉城\Projects\RongCheng-YYB\Network\HttpClient\YYBHttpClient.cs` |
