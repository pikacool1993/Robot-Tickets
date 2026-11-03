# 客户端 .cfg 配置导出协议（第一版）

本协议用于 cloud-server 后台向 Windows local-agent 交付通信配置。内部内容为 UTF-8 JSON；交付文件为 `.cfg` 二进制，采用 AES-256-GCM 加密及 Ed25519 签名。文件包装密钥与接口通信密钥、数据库凭据加密密钥独立。

## 业务入口

默认下载文件名为 `bootstrap.cfg`，打包交付时放入软件的 `config/bootstrap.cfg`。客户端自动查找及导入逻辑将在 local-agent 中实现；首次启动无本地配置时读取此文件，文件缺失或校验失败时允许手动选择配置。导出 ID 保留在文件内容与审计记录中用于追溯。

管理员登录后，在代理商列表点击“导出客户端配置”，确认后下载。API：`POST /api/admin/v1/client-config/export`，Bearer 管理员令牌，普通 JSON 请求体 `{}`。成功返回 `application/octet-stream` 和 `Content-Disposition: attachment`；错误沿用后台普通 JSON 错误响应。未认证返回 401，未启用导出返回 503。

由服务端生成配置、加密、签名，浏览器只下载二进制，不接收文件包装密钥或签名私钥。服务端成功记录 `client_config_exported` 审计后才发送文件；审计含管理员、IP、导出 ID、密钥 ID，不含密钥或配置正文。审计表示已生成并允许返回，无法证明浏览器下载完成。

本版配置为部署级通信配置，使用现有 `AGENT_TRANSPORT_KEY_ID` 指向的共享通信密钥，不包含代理商密码、登录令牌或设备编号，不绑定具体代理商，不增加代理商独立密钥体系。导入配置不替代代理商登录授权。每次导出生成新的导出 ID、时间和随机 nonce；重复下载不会轮换任何密钥。

## 服务端配置

在 `apps/cloud-server` 运行 `go run ./cmd/config-keys`，生成独立随机密钥，并输出以下配置及客户端验签公钥。输出含秘密，安全保存，不提交到 Git、不贴入日志或聊天。

| 环境变量 | 含义 |
| --- | --- |
| CLIENT_CONFIG_SERVER_URL | 客户电脑能访问的 HTTP(S) 服务根地址，如 `http://server.example:8080`，不含 API 路径、用户信息、查询或片段 |
| CLIENT_CONFIG_ENCRYPTION_KEY_ID | 包装加密密钥 ID，例如 `cfg-aes-v1` |
| CLIENT_CONFIG_ENCRYPTION_KEY | Base64 编码的 32 字节 AES 密钥 |
| CLIENT_CONFIG_SIGNING_KEY_ID | 签名密钥 ID，例如 `cfg-sign-v1` |
| CLIENT_CONFIG_SIGNING_SEED | Base64 编码的 32 字节 Ed25519 私钥种子，仅服务端保存 |

五项全部为空时禁用导出，原部署仍可运行；填写部分、格式不正确或复用敏感密钥时启动报错。Compose 已透传这些变量。更改后重建/重启服务端。对外地址由显式配置确定，不根据请求 Host 生成。

客户端构建时需要包装密钥 ID/密钥以及签名密钥 ID/公钥（生成命令输出注释中的 `CLIENT_CONFIG_SIGNING_PUBLIC_KEY`）。**不得将签名种子放入客户端、配置文件或网页。** 包装密钥固定使用但允许通过 ID 轮换；旧文件是否可导入取决于客户端内置信任密钥表。先发布支持新密钥的客户端，再切换后台导出密钥；需要兼容旧文件时保留旧密钥表。

## 二进制格式

所有长度按字节计算，整数采用大端序。整个文件最大 65536 字节。

| 顺序 | 长度 | 内容 |
| --- | --- | --- |
| 1 | 8 | ASCII magic：`RBTCFG\r\n` |
| 2 | 4 | 头部 JSON 长度，无符号 uint32，范围 2–4096 |
| 3 | 可变 | UTF-8 头部 JSON |
| 4 | 可变 | AES-GCM 密文，尾部包含 16 字节认证标签 |
| 5 | 64 | Ed25519 签名 |

头部 JSON 字段：`version`（整数 1）、`encryption_key_id`、`signing_key_id`、`nonce`（12 随机字节的标准 Base64）。密钥 ID 为 1–64 位 ASCII 字母、数字、连字符、下划线或点。AES-GCM AAD 为第 1–3 部分的原始字节；签名覆盖第 1–4 部分的原始字节。验签时禁止将头部 JSON 重新序列化，也不从文件接收验签公钥。

解密后的配置 JSON 示例（密钥是说明用占位符）：

```json
{
  "version": 1,
  "server_url": "http://server.example:8080",
  "transport_key_id": "client-v1",
  "transport_key": "<Base64 编码的 32 字节接口通信密钥>",
  "export_id": "<UUID>",
  "issued_at": "2026-11-03T00:00:00Z"
}
```

文件格式版本和配置内容版本独立，第一版均为 1。算法由文件版本固定，禁止由文件指定任意算法或降级。`issued_at` 用于追溯，不是会话有效期，配置文件不携带 7 天登录会话。

## 客户端导入约定

1. 限制文件大小，验证 magic、长度和版本，拒绝截断及额外数据。
2. 根据签名密钥 ID 在本地可信公钥表中查找公钥，先验证完整签名。
3. 验签通过后，按包装密钥 ID 解密，并验证 GCM 标签和 JSON 字段。
4. 完整校验后才替换实例配置；失败保留原配置，允许重新选择文件。
5. 敏感信息使用 Windows DPAPI CurrentUser 保护后保存在当前实例目录，禁止明文 JSON 落盘和日志输出。后续启动自动读取；登录页允许重新导入。

服务端 `internal/clientconfig.Open` 提供参考解码实现，local-agent 尚未实现。文件加密提高直接读取门槛，固定包装密钥仍可能被客户端逆向提取；数字签名不阻止复制真实文件，也不替代账号授权。当前管理页面仍使用现有 HTTP 普通 JSON 协议；签名不能改变后台登录的传输边界。
