# Terminal Task And Order Sync API

完整中文对接文档（含登录同款鉴权和加密规范、参数、响应、错误码及重试约定）见 [客户端任务状态与出票同步接口文档](client-sync-api.md)。

All `/api/agent/v1` requests use the existing encrypted envelope and Bearer session. The server derives agent ID, client number, and client instance ID from the authenticated session. Client supplied ownership fields are not accepted.

## Replace terminal task snapshot

`PUT /api/agent/v1/sync/tasks`

```json
{
  "revision": 12,
  "tasks": [
    {"task_id": 101, "authorization_status": "online"},
    {"task_id": 102, "authorization_status": "expired"}
  ]
}
```

Each snapshot replaces the authenticated terminal's current task set. `revision` must increase monotonically for that terminal; stale retries return `applied: false` and do not overwrite newer state. An empty task array is a valid synced-empty snapshot. Status values are `online`, `expired`, `pending_authorization`, `protocol_offline`, `maintenance_paused`, and `unknown`.

## Upload one order

`POST /api/agent/v1/orders`

```json
{
  "order_id": "987654321",
  "task_id": 101,
  "match_id": "match-42",
  "match_name": "Home vs Away",
  "ticket_count": 2,
  "tiers": ["VIP"],
  "seats": [{"tier": "VIP", "row": 3, "seat": 11}, {"tier": "VIP", "row": 3, "seat": 12}],
  "created_at": "2026-11-21T13:00:00+08:00",
  "payment_deadline": "2026-11-21T13:15:00+08:00",
  "detail_status": "complete",
  "viewers": [{"name": "Example", "phone": "13800000000"}, {"name": "Example Two", "phone": "13900000000"}]
}
```

`detail_status` is `complete` or `unavailable`. Complete records require payment deadline, one row/seat/tier per ticket, and one viewer per ticket. Unavailable records may contain empty seat details and a null deadline. Viewer JSON is encrypted at rest with the configured credential encryption key. The unique key is terminal instance ID plus order ID; retries never create a second record and never overwrite the first accepted payload.

## Read and inspect orders

- Authenticated agent `GET /api/agent/v1/orders` returns its own complete viewer names and phone numbers.
- Admin `GET /api/admin/v1/agents/{agent_id}/orders` returns records for that agent with viewer fields masked.
- Admin `POST /api/admin/v1/agents/{agent_id}/orders/{order_record_id}/reveal-viewers` with `{"confirm":true}` returns full viewer fields and writes an audit event containing the administrator, order, terminal, and source IP.

## Admin terminal task drilldown

- `GET /api/admin/v1/agents/{agent_id}` includes `task_summary` for terminals that have uploaded at least one snapshot; absent `task_summary` means not yet reported.
- `GET /api/admin/v1/agents/{agent_id}/terminals/{client_instance_id}/tasks` returns the specific task IDs and statuses. A never-reported terminal returns `synced:false`; an uploaded empty snapshot returns `synced:true` and zero tasks.
