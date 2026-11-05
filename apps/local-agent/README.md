# Robot local-agent

Windows 10/11 x64 桌面客户端。采用 Wails v2.17.1、Go 1.26、Vue 3、TypeScript、Vite、SQLite（纯 Go 驱动）、Windows DPAPI 和 slog。已实现登录及雾霭青任务工作台，支持任务创建、删除、座位配置和本地自动保存。微信、YYB 授权、观演人获取及购票执行尚未接入，相应弹窗明确显示未接入状态，不模拟授权成功。

## 构建与运行

需要 Windows x64、Go 1.26+、Node.js（建议与服务端前端一致使用 Node 24）、pnpm 11 和 Microsoft Edge WebView2 Runtime。普通 Go 构建即可生成生产桌面程序，不需要 C 编译器；Wails CLI 为开发模式可选工具。

### 首次打包 EXE

先关闭正在运行的客户端，避免输出的 EXE 被占用。在 PowerShell 中执行：

```powershell
cd E:\Robot\apps\local-agent

# 交付构建：从已配置的服务端环境文件提取文件解密密钥、派生验签公钥。
.\build.ps1 -ServerEnv ..\cloud-server\.env
```

脚本自动安装前端依赖、执行 TypeScript 类型检查、构建 Vue 页面，再编译 Windows x64 桌面 EXE。输出位置为 `E:\Robot\apps\local-agent\build\bin\Robot.exe`。这一步只生成程序，不会自动导出或复制 `bootstrap.cfg`，也不会自动生成交付压缩包。

### 修改代码后重新打包

首次准备过可信密钥且密钥未变时，关闭客户端，然后执行：

```powershell
cd E:\Robot\apps\local-agent
.\build.ps1
```

如果服务端的 `.cfg` 文件加密密钥或签名密钥变更，重新执行首次打包命令，更新客户端内置的解密密钥与验签公钥。

仅预览界面、尚未准备可信密钥时可以执行 `./build.ps1 -Preview`，但生成的程序不能导入正式 `.cfg`。`-Preview` 只跳过可信密钥文件存在检查；若之前已准备过密钥，仍会嵌入现有密钥。

也可以先执行 `go run ./cmd/prepare-trust -env ../cloud-server/.env`，再执行 `./build.ps1`。该命令不会改变服务端配置，不输出密钥，不将签名私钥写入客户端；生成的 `internal/trust/assets/local.json` 已被 Git 忽略。密钥轮换时保留此文件中的旧 key ID，重新构建客户端。

### 测试与交付

程序位于 `build/bin/Robot.exe`。从 Web 后台导出 `bootstrap.cfg`，放到 `build/bin/config/bootstrap.cfg`，然后双击 EXE。

测试时确保 cloud-server 已运行，且配置中的地址能够访问。客户端导入配置后，填写代理商用户名、密码和客户端编号登录，进入任务工作台。客户电脑仅需 Windows 10/11 x64 和 WebView2 Runtime，无需安装 Go、Node.js 或 pnpm。

```text
Robot/
  Robot.exe
  config/
    bootstrap.cfg
    seat-catalog.json
```

交付时仅复制 EXE 与 config 文件夹，不交付已运行产生的 `data` 文件夹。为避免误交付，构建脚本不会自动把测试数据打包成压缩包。复制未运行的原始软件包到不同文件夹即可多开。同一目录重复双击激活已有窗口。不要放到没有写权限的 Program Files 目录。移动同一软件目录可以保留实例 ID；复制使用过的目录不属于当前支持的多开方式。

若没有配置服务端的五个 `CLIENT_CONFIG_*` 变量，需先按 cloud-server README 配置；客户端不会生成或替换生产密钥。

## 登录模块行为

- 首次自动读取随包 `config/bootstrap.cfg`，先验签、再解密；无文件或失败时显示手动选择入口。
- 通信配置保存后不依赖原始 cfg 文件；登录页可重新导入。重新导入会清除旧的记住登录信息，已登录时需先退出。
- 本地数据位于 EXE 同目录 `data`，SQLite 中的配置及记住的表单整体使用 DPAPI CurrentUser 加密。更换 Windows 用户或电脑后可能需要重新导入并填写密码。
- 首次默认不记住登录信息。成功登录后才保存记住偏好；取消勾选并成功登录会删除已保存表单。关闭、下次启动始终返回登录页，不自动登录。
- 令牌仅在 Go 内存中保存，不发送到 Vue 或写入数据库。网络请求由 Go 发起，接口使用现有 AES-GCM 信封。
- 同一实例重新登录复用设备名额。编号每次可修改、全局唯一，新编号登录成功才释放旧编号。
- 会话有效期以服务端响应为准。每秒检查到期，每 30 秒在线检查授权；网络检查失败显示连接中断，401 返回登录页。暂时断网不会直接注销，但到期仍强制返回登录页。请求最长 15 秒，到期时验证请求使用到期截止时间。
- 点击 × 必须确认，取消继续运行，确认退出后尽力在两秒内撤销会话并关闭程序，不保留后台。若撤销失败，原云端会话按服务器规则过期，同一实例下次登录替换旧会话。
- 日志位于 `data/logs`，只记录必要事件，不记录密码、密钥、配置内容或令牌。当前按启动日期选择日志文件，尚无日志保留清理策略。

## 模块结构

任务按客户端实例共享，切换代理商后仍显示原任务。任务编号单调递增，删除不复用。任务数据（包括编号序列和最新操作日志）整体通过 DPAPI 保护，存入同一 SQLite settings 记录，以单次原子提交同时保存序列与任务。

启动时读取 `config/seat-catalog.json`（开发时相对于 `ROBOT_AGENT_ROOT`）。构建时仅为缺少目录文件的新软件包复制源配置；已有 `build/bin/config/seat-catalog.json` 不会被覆盖，更新需显式替换。目录包含票类、价格、看台及可售状态；无效文件显示错误，不覆盖有效任务数据。目录规范化后计算 SHA-256，忽略格式、字段顺序及选项排列变化；有效内容变化后重启，全部已有座位选择失效，需重新选择。无变化重启保留配置。新建默认选 A/B/C/D/E；模式切换按票类归属双向转换；不可售区域不参与选择。

任务行固定高度，座位摘要单行省略，悬停查看全文，点击打开弹窗。看台选项固定两行，编号及特殊说明位于第一行，票类和价格在第二行，长说明省略并提供悬停详情。保存失败会保留弹窗修改并提供重试或放弃，最新保存日志落盘后重启继续显示。窗口支持缩小到 360px 宽。

| 模块 | 职责 |
| --- | --- |
| main.go | Wails 窗口、原生文件对话框、目录单实例、退出确认、Vue 绑定 |
| internal/agent | 启动导入、登录流程、偏好保存、会话监控 |
| internal/cloud | HTTP 客户端、超时、AES-GCM 信封、业务错误映射 |
| internal/store | SQLite 数据及实例身份 |
| internal/tasks | 座位目录校验与指纹、永久任务编号、任务持久化和座位规则 |
| internal/secure | DPAPI CurrentUser 保护 |
| internal/trust | 编译时内置的文件解密密钥和可信验签公钥表 |
| frontend | 登录、配置导入与最小主界面 |

复用 `cloud-server/pkg/clientprotocol` 的公开协议 API，通过本地 Go module replace 引用，服务端与客户端仍独立构建，不需要将云端可执行程序打包到客户端。

## 开发与验证

```powershell
# 先构建前端，供 go:embed 使用
cd frontend
pnpm install --frozen-lockfile
pnpm build
cd ..
go test ./...
go vet ./...

# 可选热更新开发（首次安装 CLI）
go install github.com/wailsapp/wails/v2/cmd/wails@v2.17.1
$env:ROBOT_AGENT_ROOT = (Get-Location).Path
wails dev
```

`ROBOT_AGENT_ROOT` 仅用于开发时显式指定实例数据根目录，正常交付不设置。前端开发服务器本身不具备桌面绑定，需通过 Wails 开发窗口打开。

必须同步重启/部署更新后的 cloud-server，以创建编号绑定表及全局事务锁表。原有活跃会话迁移到编号绑定表；无法关联实例的历史编号保留其原代理商归属。新登录改号会删除旧编号关联的历史会话以满足原表外键，然后释放旧编号；审计历史保留。新版本仍使用已有 7 天会话。

测试使用独立临时 SQLite、当前 Windows 用户的真实 DPAPI，以及本地加密测试 HTTP 服务，不访问生产账号。真实部署登录、Windows 10 目标机器兼容性和多实例规模性能需在对应环境验收。任务读写均在 Go 层检查登录及会话到期；注销/到期后退出工作台，本地任务保留。尚未接入协议进程及购票执行，后续需将其接入同一注销/退出生命周期。
