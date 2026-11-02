# cloud-server

## 本地运行（Windows / PowerShell）

### 1. 创建 MySQL 数据库和账号

先用有权限的 MySQL 管理账号执行一次（替换密码）：

```sql
CREATE DATABASE robot CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci;
CREATE USER 'robot_app'@'127.0.0.1' IDENTIFIED BY '替换为强密码';
GRANT ALL PRIVILEGES ON robot.* TO 'robot_app'@'127.0.0.1';
FLUSH PRIVILEGES;
```

### 2. 生成本地密钥

在 PowerShell 中生成 32 字节 AES 密钥的 Base64 值：

```powershell
$newAesKey = { $buf = [byte[]]::new(32); [Security.Cryptography.RandomNumberGenerator]::Fill($buf); [Convert]::ToBase64String($buf) }
$adminTransportKey = & $newAesKey
$agentTransportKey = & $newAesKey
$credentialEncryptionKey = & $newAesKey
```

这些值只用于本地开发；不要提交到 Git、聊天记录或日志。

### 3. 配置并启动

在 `E:\Robot\apps\cloud-server` 目录执行：

```powershell
$env:MYSQL_DSN = 'robot_app:替换为强密码@tcp(127.0.0.1:3306)/robot?parseTime=true&charset=utf8mb4'
$env:ADMIN_BOOTSTRAP_USERNAME = 'admin'
$env:ADMIN_BOOTSTRAP_PASSWORD = '替换为强管理员密码'
$env:TRANSPORT_KEYS_JSON = "{`"admin-v1`":`"$adminTransportKey`",`"client-v1`":`"$agentTransportKey`"}"
$env:ADMIN_TRANSPORT_KEY_ID = 'admin-v1'
$env:AGENT_TRANSPORT_KEY_ID = 'client-v1'
$env:AGENT_CREDENTIAL_ENCRYPTION_KEY = $credentialEncryptionKey
$env:AGENT_CREDENTIAL_KEY_VERSION = 'v1'
$env:LISTEN_ADDR = ':8080'

go run ./cmd/server
```

首次启动会自动创建数据表，并在管理员表为空时创建环境变量指定的管理员。后续启动不会覆盖管理员密码。迁移文件路径相对当前工作目录解析，因此请从 `apps/cloud-server` 目录启动。

### 4. 检查服务

另开一个 PowerShell 窗口执行：

```powershell
Invoke-WebRequest http://localhost:8080/healthz -Method Get
```

成功时返回 HTTP `204 No Content`。`/healthz` 是不含业务数据的明文基础设施端点；业务 API 按项目协议使用 AES-GCM JSON 信封。客户端登录使用 `client-v1`；管理员 API 使用 `admin-v1`。

### 5. 测试与构建

```powershell
go test ./...
go build ./cmd/server
```
