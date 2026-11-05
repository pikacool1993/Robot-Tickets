# cloud-server

## 代理商详情与已登录终端

详情中的事件通过代理商名称旁的“查看事件”按钮打开弹窗，点击后才加载记录；支持加载提示、空状态、失败重试和关闭操作。

管理控制台支持手机、平板和桌面屏幕：窄屏采用顶部导航，720px 及以下将代理商、终端和事件表格转换为带字段标签的卡片。登录、创建/编辑、凭据查看及确认弹窗均适配小屏，弹窗内容可独立滚动，打开时禁止背景页面滚动。

代理商列表显示有效登录设备数 / 设备上限，详情显示创建时间及已登录终端列表（客户端编号、实例 ID、登录时间、登录 IP、会话到期时间）。统计与终端列表统一按账号已启用、会话未撤销且未过期计算，不代表终端实时联网状态。正常退出、禁用账号、密码重置或会话到期后，相关会话不再计入。

Web 列表及详情每 60 秒自动刷新，并显示倒计时；两处均提供手动刷新入口，手动刷新后重新计时；页面隐藏时暂停轮询。刷新失败保留上次数据并提示，避免把请求失败显示为零设备。详情 API `GET /api/admin/v1/agents/{id}` 返回 `created_at`、`active_devices` 和 `devices`；设备记录不返回令牌或令牌哈希。异常关机或断网且未成功登出时，会话可能继续占用名额直到撤销或到期。

## Go 密钥生成工具

在 `apps/cloud-server` 目录执行：

```powershell
go run ./cmd/generate-keys
```

生成接口通信 AES 密钥与独立的数据库凭据加密密钥，输出可填入 `.env` 的 `TRANSPORT_KEYS_JSON`、`AGENT_TRANSPORT_KEY_ID`、`AGENT_CREDENTIAL_ENCRYPTION_KEY`、`AGENT_CREDENTIAL_KEY_VERSION`。此命令替代原 `scripts/generate_keys.py`，不再需要 Python。

`.cfg` 文件包装加密密钥与签名密钥仍使用 `go run ./cmd/config-keys` 生成。两个命令只生成密钥配置，不修改 `.env`，不直接生成 `.cfg` 文件；文件由 Web 后台导出。输出包含秘密，应安全保存。初始化后不要随意替换已有密钥，尤其数据库凭据密钥，否则历史密码将无法解密。

## 客户端通信配置导出（.cfg）

后台代理商列表提供“导出客户端配置”。服务端以 AES-256-GCM 加密 JSON 配置，并使用 Ed25519 签名，管理员确认后下载 `.cfg` 二进制文件。需要先配置 `.env.example` 中的五个 `CLIENT_CONFIG_*` 变量；全部为空时该功能不可用，其余服务正常运行。

在本目录执行 `go run ./cmd/config-keys` 生成独立密钥和客户端验签公钥，并安全保存输出。将 `CLIENT_CONFIG_SERVER_URL` 改为客户实际可访问的服务根地址；随后重启服务或重建 Compose。签名私钥种子仅保存在服务端，客户端需要文件加密密钥和验签公钥。完整格式、密钥管理及导入规则见 [客户端配置文件协议](../../docs/客户端cfg配置文件协议.md)。

本版导出部署级共享通信配置，不包含代理商凭据，不替代账号登录。

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
