# 客户端任务状态与出票同步接口文档

更新日期：2026-11-21。以当前 cloud-server 源码为准。本文中的业务 JSON 示例均为加密前或解密后的内容。接口根地址与登录接口相同。

## 一、公共规范（沿用代理商登录接口）

客户端基础路径：`/api/agent/v1`。先调用 `POST /api/agent/v1/auth/login`，再携带其返回的访问令牌：

```http
Authorization: Bearer <access_token>
Content-Type: application/json
```

客户端接口有请求体时必须使用加密信封，成功和错误响应均使用加密信封。GET 无请求体，响应仍需解密。

```json
{
  "version": 1,
  "key_id": "<AGENT_TRANSPORT_KEY_ID 对应的密钥标识>",
  "nonce": "<随机 12 字节 nonce 的标准 Base64>",
  "ciphertext": "<密文与 GCM tag 的标准 Base64>"
}
```

算法为 AES-256-GCM；密钥 32 字节、nonce 12 字节、认证标签 16 字节，明文为 UTF-8 JSON。AAD 是 UTF-8 字符串 `robot-http-json:1:<key_id>`。每次请求（包括重试）都生成新 nonce，认证标签附在密文末尾。

当前实现使用服务器配置的 `AGENT_TRANSPORT_KEY_ID`，与现有登录传输一致；不要自行切换到登录响应中的 `agent_key_id`。local-agent 可复用 `cloud.Client.Call(ctx, method, path, token, input, &output)`；此处 path 不含 `/api/agent/v1` 前缀。

响应解密后统一为：

```json
{"code":"ok","message":"","data":{}}
```

代理商、终端编号、实例 ID 由有效会话确定，请求体无需填写归属字段；传入同名字段不会改变归属。时间采用 RFC 3339 带时区字符串，云端保存订单时间时转换为 UTC。客户端加密信封请求体最大 1 MiB。以下长度限制按当前 Go 校验的 UTF-8 字节数计。

管理员基础路径为 `/api/admin/v1`，使用管理员 Bearer 会话，**普通 JSON，不套客户端加密信封**。

## 二、接口清单

| 调用方 | 方法 | 完整路径 | 用途 |
| --- | --- | --- | --- |
| 客户端 | PUT | `/api/agent/v1/sync/tasks` | 上传本终端完整任务快照 |
| 客户端 | POST | `/api/agent/v1/orders` | 上传一条出票记录 |
| 客户端 | GET | `/api/agent/v1/orders` | 查询所属代理商完整订单 |
| 管理员 | GET | `/api/admin/v1/agents/{id}` | 现有接口增加终端任务汇总 |
| 管理员 | GET | `/api/admin/v1/agents/{id}/terminals/{instance}/tasks` | 查看具体任务 |
| 管理员 | GET | `/api/admin/v1/orders` | 集中查询全部代理商脱敏订单 |
| 管理员 | POST | `/api/admin/v1/orders/{order}/reveal-viewers` | 确认查看集中订单明文并审计 |

## 三、上传终端任务状态

**PUT `/api/agent/v1/sync/tasks`**

### 请求参数

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| revision | int64 | 是 | 大于 0，本代理商与终端实例范围内单调递增，须持久化，重启不归零 |
| tasks | array | 是 | 当前全部任务，最多 5000 条，允许 []，不能省略或为 null |
| tasks[].task_id | int64 | 是 | 正整数，同一快照内不得重复 |
| tasks[].authorization_status | string | 是 | 见状态枚举 |

| 状态值 | 展示含义 |
| --- | --- |
| online | 在线 |
| expired | 授权失效 |
| pending_authorization | 待授权 |
| protocol_offline | 协议离线 |
| maintenance_paused | 维护暂停 |
| unknown | 状态未知 |

### 请求示例

```json
{"revision":12,"tasks":[{"task_id":101,"authorization_status":"online"},{"task_id":102,"authorization_status":"expired"}]}
```

### 成功响应：HTTP 200

```json
{"code":"ok","message":"","data":{"revision":12,"applied":true,"total":2,"changed":2}}
```

`total` 是本次快照任务总数；`changed` 是新增、状态变化、删除数量之和。全量替换当前任务集合，省略的旧任务从统计中删除，不影响订单历史。上传 `tasks:[]` 表示终端当前没有任务。

若版本小于或等于云端版本，返回 HTTP 200，但不写入：

```json
{"code":"ok","message":"","data":{"revision":13,"applied":false}}
```

此处 revision 是云端已有版本，不返回 total、changed。重试保留原 revision，新状态生成更高版本。任务创建、删除、授权状态变化时触发，可合并为最新待发快照；不要求固定周期上报。

## 四、上传出票记录

**POST `/api/agent/v1/orders`**

订单创建后等待现有订单详情查询流程结束：成功则上传 complete，最终失败则上传 unavailable 和已有信息。每单生成一次上报内容，网络失败允许重试。

### 请求参数

| 字段 | 类型 | 必填 | 说明 |
| --- | --- | --- | --- |
| order_id | string | 是 | 官方订单号，去首尾空白后非空，最长 128 字节 |
| task_id | int64 | 是 | 出票任务编号，正整数 |
| match_id | string | 是 | 场次 ID，非空，最长 128 字节 |
| match_name | string | 是 | 场次名称，非空，最长 255 字节 |
| ticket_count | integer | 是 | 1～100 张 |
| tiers | string[] | 否 | 票档，最多 100 项，省略/null 转为 [] |
| seats | object[] | 条件 | 实际座位，最多 100 项；complete 时项数须等于票数 |
| created_at | string | 是 | 非零 RFC 3339 订单创建/出票时间 |
| payment_deadline | string/null | 条件 | 支付截止时间，complete 时必填且非零 |
| detail_status | string | 是 | complete 或 unavailable |
| viewers | object[] | 条件 | 观演人，最多 100 项；complete 时项数须等于票数 |
| simulated | boolean | 否 | 是否为客户端模拟验收订单；省略时按 false 处理 |
| order_ip | string | 否 | N5 请求发送时选中的代理 IP（IPv4/IPv6），本机直连为 `direct`，历史未知或模拟订单为空；不含端口或认证信息 |

| 子字段 | 类型 | 校验规则 |
| --- | --- | --- |
| seats[].tier | string | 最长 128 字节，complete 时非空 |
| seats[].row | integer | 非负排号，complete 时大于 0 |
| seats[].seat | integer | 非负座位号，complete 时大于 0 |
| viewers[].name | string | 已提供条目必须非空，最长 200 字节 |
| viewers[].phone | string | 最长 64 字节，未取得时允许空字符串 |

### 完整详情请求示例

```json
{
  "order_id":"987654321",
  "task_id":101,
  "match_id":"match-42",
  "match_name":"主队 VS 客队",
  "ticket_count":2,
  "tiers":["VIP"],
  "seats":[{"tier":"VIP","row":3,"seat":11},{"tier":"VIP","row":3,"seat":12}],
  "created_at":"2026-11-21T13:00:00+08:00",
  "payment_deadline":"2026-11-21T13:15:00+08:00",
  "detail_status":"complete",
  "viewers":[{"name":"示例甲","phone":"13800000000"},{"name":"示例乙","phone":"13900000000"}]
}
```

### 详情失败请求示例

```json
{
  "order_id":"987654322",
  "task_id":102,
  "match_id":"match-42",
  "match_name":"主队 VS 客队",
  "ticket_count":1,
  "tiers":["A"],
  "seats":[],
  "created_at":"2026-11-21T13:02:00+08:00",
  "payment_deadline":null,
  "detail_status":"unavailable",
  "viewers":[{"name":"示例甲","phone":"13800000000"}]
}
```

未取得字段留空，不能以计划座位要求代替实际座位。接口不接收票务 token、code 或证件号。`order_ip` 取自本次下单请求实际选中的出口快照，后续代理切换不改写；代理 IP 表示录入的代理入口地址，不保证等同代理商最终公网出口，`direct` 也不代表已探测到本机公网 IP。模拟验收订单会通过同一接口上报，必须携带 `simulated:true`；客户端模拟流程使用本地生成的详情，不请求真实订单详情接口。

### 成功响应：HTTP 200

```json
{"code":"ok","message":"","data":{"order_id":"987654321"}}
```

全局唯一键为终端实例 ID + 官方订单号。**首次成功入库后，重复上传不新增、不覆盖原记录**，返回相同成功格式，不区分首次与重试。已上传的详情失败记录也不在后续普通查单时补写。代理商名和终端编号取自首次上报时的会话及云端资料，姓名和电话加密存储。

## 五、客户端查询所属代理商订单

**GET `/api/agent/v1/orders`**，无请求体。只查询当前认证代理商，不接受指定其他代理商的参数。当前按出票时间和云端记录 ID 倒序返回最多 500 条，没有分页参数。

### 成功响应：HTTP 200

```json
{
  "code":"ok","message":"",
  "data":[{
    "id":51,"agent_name":"示例代理商",
    "client_number":"A01","client_instance_id":"11111111-1111-4111-8111-111111111111",
    "task_id":101,"order_id":"987654321",
    "match_id":"match-42","match_name":"主队 VS 客队",
    "ticket_count":1,"tiers":["VIP"],"seats":[{"tier":"VIP","row":3,"seat":11}],
    "created_at":"2026-11-21T05:00:00Z","payment_deadline":"2026-11-21T05:15:00Z",
    "detail_status":"complete","simulated":false,"order_ip":"192.0.2.10","viewers":[{"name":"示例甲","phone":"13800000000"}],
    "received_at":"2026-11-21T05:00:02Z"
  }]
}
```

`id` 是云端记录 ID，`order_id` 是官方订单号，`received_at` 是云端接收时间。未知支付截止时间返回 null，无记录返回 `data:[]`。所属代理商取得完整姓名、电话。

## 六、管理员终端任务接口

### 6.1 现有代理商详情扩展

**GET `/api/admin/v1/agents/{id}`** 的 `data.devices[]` 新增 `task_summary`，原字段保留：

```json
{"task_summary":{"total":3,"online":2,"expired":1,"pending_authorization":0,"protocol_offline":0,"maintenance_paused":0,"unknown":0,"tasks_synced_at":"2026-11-21T05:00:00Z"}}
```

尚未上报为 `task_summary:null`；已上报空集合则各项为 0 且有同步时间。终端云端会话状态与任务微信授权状态分开，不因终端断连推断授权失效。

### 6.2 终端具体任务

**GET `/api/admin/v1/agents/{id}/terminals/{instance}/tasks`**。id 是代理商 ID，instance 是终端实例 ID，路径值须 URL 编码。

```json
{
  "code":"ok","message":"",
  "data":{
    "synced":true,"tasks_synced_at":"2026-11-21T05:00:00Z","total":1,
    "counts":{"online":1,"expired":0,"pending_authorization":0,"protocol_offline":0,"maintenance_paused":0,"unknown":0},
    "tasks":[{"task_id":101,"authorization_status":"online","updated_at":"2026-11-21T05:00:00Z"}]
  }
}
```

任务按编号升序。updated_at 是服务器接收新增/状态变化的时间，不是客户端事件时间。没有该代理商/终端快照时仍为 HTTP 200：

```json
{"code":"ok","message":"","data":{"synced":false,"tasks":[]}}
```

## 七、管理员出票接口

### 7.1 脱敏订单列表

**GET `/api/admin/v1/orders`**。admin-web 的“订单查询”侧边栏入口使用此接口，将所有客户端成功上报的订单集中展示。姓名完整返回，电话默认脱敏，例如 `138****0000`。

支持筛选参数：

| 参数 | 说明 |
| --- | --- |
| agent_id | 代理商 ID |
| client_number | 终端编号 |
| task_id | 任务编号 |
| order_id | 官方订单号 |
| match_name | 场次名称模糊匹配 |
| detail_status | `complete` 或 `unavailable` |
| created_from / created_to | 出票日期范围，格式 `YYYY-MM-DD`，结束日期包含当天 |
| page / limit | 页码和每页数量；默认 1 / 50，limit 最大 200 |

成功响应：`data` 包含 `items`、`page`、`limit`、`total`；每个 item 结构与第五节订单记录一致，并包含 `simulated` 布尔字段及 `order_ip` 字符串字段。管理页面对模拟订单显示“模拟订单”标识。无记录时 `items:[]`。

### 7.2 查看完整观演人信息

**POST `/api/admin/v1/orders/{order}/reveal-viewers`**。

order 为订单列表 item 的云端记录 `id`，**不是官方订单号**。请求普通 JSON：

```json
{"confirm":true}
```

成功响应 HTTP 200：

```json
{"code":"ok","message":"","data":{"order_id":"987654321","viewers":[{"name":"示例甲","phone":"13800000000"}]}}
```

接口校验管理员会话。管理员点击联系方式中的脱敏号码时，页面调用此接口读取完整号码，该操作不写入审计。

## 八、错误码和重试约定

业务错误格式：

```json
{"code":"invalid_input","message":"Invalid task snapshot","data":null}
```

| HTTP | code | 含义 | 建议处理 |
| --- | --- | --- | --- |
| 400 | invalid_envelope | 客户端信封、密钥 ID 或加密内容无效 | 检查传输配置 |
| 400 | invalid_input | 任务或订单字段不合法 | 修正数据后再发 |
| 401 | unauthorized | 未登录、令牌过期/撤销、代理商不可用 | 暂存数据，重新登录后重试 |
| 500 | internal_error | 数据库、加密或审计错误 | 保留待发数据，退避重试 |
| 400 | invalid_json | 管理员请求 JSON 无法解析 | 修正请求 |
| 400 | confirmation_required | 管理员未明确确认查看明文 | 传 confirm:true |
| 404 | not_found | 明文查看订单不存在或不属于指定代理商 | 核对路径 ID |

客户端错误响应也要解密，管理员错误直接解析。不要依赖 message 的具体文字分支。网络超时可能发生在云端已提交之后：任务重试保留原版本，订单重试保留原订单号及原业务内容，传输 nonce 每次重新生成。任务 `applied:false` 不应循环发送旧版本；HTTP 200 且 code=ok 表示请求已处理。

## 九、交付边界

cloud-server 接口与本次 local-agent 接入均已实现。模拟订单以 `simulated:true` 上报并在管理页面标识。当前没有客户端删除云端订单 API，删除本地记录不删除云端记录。

## 十、飞书通知配置下发

`GET /api/agent/v1/feishu-config` 使用上述 Bearer 会话与 AES-GCM 响应信封，返回的配置只属于当前会话的代理商，不接受 `agent_id` 等账号选择参数。登录后获取，并在健康检查成功后约每 30 秒同步。

解密后的成功响应：

```json
{"code":"ok","message":"","data":{"enabled":true,"webhook":"https://open.feishu.cn/open-apis/bot/v2/hook/<机器人标识>","order_created":true,"auth_expired":true,"order_blocked":true,"revision":1}}
```

未配置时 `webhook` 为空、`revision` 为 0，四个开关默认开启。客户端应保留最近有效配置以应对临时同步失败，缓存必须加密并按服务端和代理商隔离。完整 Webhook 只交给 Go 发送层；界面与日志需脱敏。

通知由 local-agent 直接发送飞书，服务器不接收事件、不转发消息、不保存逐条发送结果。客户端失败后依次等待 5／15／30 秒，最多重试 3 次；订单通知等到详情齐全再发送，已知支付截止时间到期后停止；关闭或重启不恢复待发事件。完整业务规则和管理员配置接口见[工程文档的飞书章节](../../../README.md#feishu-notifications)。
