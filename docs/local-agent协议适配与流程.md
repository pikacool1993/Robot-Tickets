# local-agent 协议适配与流程

> 范围：说明 local-agent 如何将微信和 YYB 两套协议适配到统一任务模型。具体请求、字段和错误码见 [微信与YYB协议服务API参考](微信与YYB协议服务API参考.md)。  
> 更新日期：2026-11-08。

## 接入结构

微信和 YYB 的 HTTP API 不同：微信以 `wxid` 标识账号，使用 `Success` 和微信协议业务码；YYB 以 `openid` 标识账号，使用 `{code,msg,data}`，并在扫码授权后额外执行 `/confirm`。local-agent 把这些差异留在各自 adapter 中，授权工作流和任务视图只使用统一结构。

```text
Task / Authorization workflow
             |
       Protocol interface
         /          \
 WeChat adapter    YYB adapter
      |                 |
 local WeChat       YYB Go service
```

核心结构定义在 `apps/local-agent/internal/protocols/adapter.go` 和 `runtime.go`：

```go
type Account struct {
    Ref      string // wxid 或 openid
    Nickname string
    Avatar   string // 微信扫码响应中的头像 URL
}

type Scan struct {
    ID      string // uuid 或 session_id
    Image   string // data URI
    Status  string // 统一扫码状态
    Account *Account
}

type Binding struct {
    Protocol string // wechat 或 yyb
    Account  Account
    Status   string
    Paused   bool
}
```

核心能力接口为 `Protocol`：

```go
type Protocol interface {
    Begin(context.Context) (Scan, error)
    Poll(context.Context, string) (Scan, error)
    Maintain(context.Context, Account) (string, error)
    Logout(context.Context, Account) error
}
```

协议专属 adapter 还实现签约资料、账号同步、健康检查、微信服务端自动心跳控制和 YYB 头像 URL 等可选能力。它们不要求另一协议伪装实现；除头像 URL 被任务视图使用外，其余能力目前尚未接成 agent/Wails 操作或 UI 入口。

## 字段和状态映射

| local-agent 字段 | WeChat 来源 | YYB 来源 |
|---|---|---|
| `Scan.ID` | `Data.Uuid` | `data.session_id` |
| `Scan.Image` | `Data.QrBase64` | `data.image_base64` |
| `Account.Ref` | `acctSectResp.userName`，即 wxid | confirm 响应的 `openid` |
| `Account.Nickname` | `acctSectResp.nickName` | confirm 响应的 `nickname` |
| 任务头像 | 保存本次扫码响应 `headImgUrl`，确认响应缺失时沿用本次扫码已取得的头像 | 由 `/accounts/avatar?ref=openid` 提供 |
| 扫码已确认 | 微信轮询成功数据包含账号信息 | poll 为 `authorized` 后还要 confirm 成功 |
| `Maintain` | 检查 `/robot/account` 后请求 `/api/Login/HeartBeat` | `/accounts/refresh` |
| `Logout` | `/api/Login/LogOut?wxid=...` | `DELETE /accounts?ref=openid` |

WeChat 的 -8 等通用错误可能表示网络或解包失败；local-agent 将这类结果标记为 `unknown`，不据此清除账号绑定。YYB 的账号不存在映射为账号过期。删除任务时两种协议仍调用各自退出语义：微信上游登出，YYB 删除协议服务保存的账号。

## 登录和扫码流程

登录时用户选择协议并配置该协议代理。local-agent 启动或连接所选服务、检查管理身份，再继续客户端云端登录；另一套已运行的协议服务不会被主动关闭。当前协议选择保存在客户端实例级，实例中的所有任务共用，任务不能单独指定协议。

扫码授权过程：

1. `BeginAuthorization` 为任务创建独立操作 ID 和五分钟截止时间。
2. adapter 申请二维码并映射为统一的 `Scan`。
3. 工作流每两秒调用一次 `Poll`。WeChat 从登录响应识别 wxid；YYB 在授权状态后调用 `/confirm` 并读取 openid。
4. 成功绑定后将 `{Protocol, Account, Status}` 持久化。新授权保存成功后才替换旧绑定；取消、失败、过期、超时或保存失败都保留旧绑定。

关闭扫码弹窗不会取消后端等待。重新发起扫码会取消旧操作并重新取码计时。

## 账号维护和进程生命周期

登录期间每 15 秒检查协议进程身份；账号维护按分钟触发，按协议和账号引用去重并串行执行。WeChat 使用客户端触发的心跳；YYB 使用 `/accounts/refresh` 检查状态并刷新凭据。WeChat 协议自身的 `AutoHeartBeat` 能力已映射，但当前托管流程不启用，避免与客户端定时器重复维护。

协议进程由 `runtime.go` 独立启动并持久化 PID、创建时间、端口和控制令牌。YYB 固定 `127.0.0.1:8088`；WeChat 使用已保存端口或 8062，并保留端口回退策略。客户端退出时取消扫码和账号维护，保留协议进程及协议侧凭据；重新登录后重新检查账号。手动关闭/重启协议会根据用户确认处理受影响任务。

YYB 服务的原生控制台仍可在 `http://127.0.0.1:8088/` 访问。`/robot/health` 是令牌保护的 local-agent 管理端点；YYB 原生 `/health` 是协议 API。WeChat 同样由 local-agent 增加 `/robot/health`、`/robot/account`，没有通用 `/health`。

## 当前范围

- 两套协议都完成扫码、账号绑定、存活检查、退出及各自非小程序可选接口的 adapter 映射。
- 业务小程序授权码、cloud-server 业务登录和小程序云函数调用尚未接入 local-agent；扫码成功只表示“协议账号已登录”。
- 后续业务授权应独立于协议账号维护：微信授权码取自 `Data.code`，YYB 取自 `data.result.code`，之后由业务层使用短期 code 登录目标业务系统。
- 若未来改成每任务单独选择协议，需要把协议选择从登录级 `AuthProtocol` 移至任务绑定，并让扫码、维护、显示和删除按任务绑定的协议分发。

## 代码索引

| 内容 | 文件 |
|---|---|
| 统一模型、接口和可选能力 | `apps/local-agent/internal/protocols/adapter.go` |
| 微信字段与端点映射 | `apps/local-agent/internal/protocols/wechat.go` |
| YYB 字段与端点映射 | `apps/local-agent/internal/protocols/yyb.go` |
| 扫码等待、绑定和账号维护 | `apps/local-agent/internal/protocols/authorization.go` |
| 进程生命周期和健康身份检查 | `apps/local-agent/internal/protocols/runtime.go` |
| 客户端登录级协议选择 | `apps/local-agent/internal/agent/service.go` |
| 产品约束与验收 | `docs/local-agent协议接入第一版.md` |
