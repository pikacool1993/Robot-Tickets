# cloud-server

## 本地运行（Windows / PowerShell）

## 使用 Docker Compose 本地运行

如果本机已安装 Docker Desktop，推荐使用 Compose 同时启动 MySQL 和 `cloud-server`。`cloud-server` 镜像会在一次构建中编译 `admin-web` 前端和 Go 服务端，由同一个容器提供管理页面与 API。

### 1. 创建环境文件

在 `E:\Robot\apps\cloud-server` 目录执行：

```powershell
Copy-Item .env.example .env
```

编辑 `.env`，至少修改 `MYSQL_ROOT_PASSWORD`、`MYSQL_APP_PASSWORD` 和 `ADMIN_BOOTSTRAP_PASSWORD`。AES 密钥可以用 PowerShell 生成：

```powershell
function New-AesKey { $buf = [byte[]]::new(32); [Security.Cryptography.RandomNumberGenerator]::Fill($buf); [Convert]::ToBase64String($buf) }
$adminKey = New-AesKey
$clientKey = New-AesKey
$credentialKey = New-AesKey
```

把生成的两个值分别填入 `.env` 的 `TRANSPORT_KEYS_JSON`（仅 `client-v1`）以及 `AGENT_CREDENTIAL_ENCRYPTION_KEY`。管理员后台接口使用普通 JSON，不再需要管理员传输密钥。不要提交 `.env`。

### 2. 启动

```powershell
docker compose --env-file .env -f docker-compose.dev.yml up --build -d
```

MySQL 会先自动创建数据库和业务账号；MySQL 健康检查通过后，`cloud-server` 自动执行表迁移并启动。浏览器访问 `http://localhost:8080` 使用管理后台，API 位于同一源的 `/api/` 下。另开窗口检查：

```powershell
Invoke-WebRequest http://localhost:8080/healthz -Method Get
```

### 3. 停止和清理

```powershell
docker compose --env-file .env -f docker-compose.dev.yml down
```

保留 MySQL 数据。如果需要清空本地数据库并重新初始化：

```powershell
docker compose --env-file .env -f docker-compose.dev.yml down -v
```

Compose 初始化账号和数据库只在 MySQL 数据卷为空时执行。修改 `.env` 中的数据库密码后，如需让新密码生效，应先执行 `down -v` 再重新启动。

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
$env:TRANSPORT_KEYS_JSON = "{`"client-v1`":`"$agentTransportKey`"}"
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

成功时返回 HTTP `204 No Content`。`/healthz` 是不含业务数据的明文基础设施端点；代理商 API 使用 `client-v1` 的 AES-GCM JSON 信封，管理员 API 使用普通 JSON。管理页面由 Go 服务端托管，不需要额外的 Web 容器或 Nginx。

### 5. 测试与构建

```powershell
go test ./...
go build ./cmd/server
```
