# Football 蓉城业务接口参考

> 本文记录 Football 客户端调用的 CDN3/CDN5 业务接口。协议服务 API 见 [微信与YYB协议服务API参考](微信与YYB协议服务API参考.md)，local-agent 的协议适配和状态流程见 [local-agent协议适配与流程](local-agent协议适配与流程.md)。  
> 更新时间：2026-11-08。目标服务端业务码和字段可能变化，以当前服务端响应为准。

## 请求约定

- CDN3：`https://fccdn1.k4n.cc/`；CDN5：`https://fccdn5.k4n.cc/`。
- 登录后的 CDN3 接口携带 `Authorization: Bearer {token}`。
- 请求使用工程配置的设备 User-Agent、微信小程序 Referer 和代理池。
- HTTP 非 2xx 或 JSON 解析失败时，客户端将 `Success=false`。

## CDN3

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

客户端成功业务码为 `1` 或 `200`。登录请求核心字段为 `code`、`s.deviceId`，成功响应读取 `token`、`info.id`。票区响应读取 `btnStatus`、`btnText`、`maxNum`、`typeCode` 和 `data`。

资料更新请求：

```json
{"encryptedData":"小写十六进制密文","version":1,"expireTime":1700000000000}
```

工程使用 AES-CBC/PKCS7 加密资料 JSON，密钥为 Base64，IV 为 UTF-8 字符串。

## CDN5 下单

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

响应读取 `code`、`msg`、`timeout`、`data`；最终下单结果需结合业务码和消息判断。

## 源码索引

这些 Football C# 源码目录不包含在当前 Robot 仓库中；接口细节来自原客户端工程参考。

| 内容 | 文件 |
|---|---|
| CDN3 客户端 | `src/Football.Infrastructure/RongCheng/RongChengCdn1Client.cs` |
| CDN5 客户端 | `src/Football.Infrastructure/RongCheng/RongChengCdn5Client.cs` |
| CDN 请求头 | `src/Football.Infrastructure/RongCheng/RongChengRequestHeaders.cs` |
| CDN5 加密签名 | `src/Football.Infrastructure/RongCheng/RongChengOrderCrypto.cs` |
