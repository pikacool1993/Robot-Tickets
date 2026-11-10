# 协议与业务接口文档索引

协议 API、local-agent 适配流程和 Football 蓉城业务接口已拆分维护：

| 文档 | 内容 |
|---|---|
| [微信与YYB协议服务API参考](微信与YYB协议服务API参考.md) | YYB / WeChat 端点、请求响应字段、错误语义及 local-agent 管理扩展 |
| [local-agent协议适配与流程](local-agent协议适配与流程.md) | 统一数据结构、协议差异映射、登录和维护流程、进程生命周期及接入范围 |
| [Football蓉城业务接口参考](Football蓉城业务接口参考.md) | CDN3 / CDN5 登录、账号、订单和加密签名接口 |

## 维护约定

- 协议服务 API 以本仓库 `protocol-services/wechat`、`protocol-services/yyb` 源码及各自 API 文档为准。
- local-agent 行为以 `apps/local-agent/internal/protocols` 和 `docs/local-agent协议接入第一版.md` 为准。
- Football CDN3/CDN5 客户端源码位于原 Football 工程，不在当前 Robot 仓库中；相关文档需对照原客户端和服务端版本维护。
- 协议版本更新时，同时复核 API 路由、字段大小写、响应信封、业务码和端口配置。
