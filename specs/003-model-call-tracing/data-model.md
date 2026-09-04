# Data Model: 模型调用追踪

## `model_call_traces`

一次模型网关逻辑调用的一行轻量索引。正文不放在此表。

| 字段 | 类型/约束 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | 内部关联键 |
| `trace_id` | VARCHAR(64) UNIQUE | 对外显示的随机追踪标识 |
| `request_id` | VARCHAR(128), nullable | 既有客户端请求标识，非唯一 |
| `parent_trace_id` | VARCHAR(64), nullable | WebSocket 会话或关联调用 |
| `turn_number` | INTEGER, nullable | WebSocket 模型调用轮次 |
| `user_id`, `api_key_id`, `group_id`, `account_id` | BIGINT, nullable | 调用身份与最终路由快照 |
| `route`, `protocol` | VARCHAR(160), VARCHAR(24) | 入口路由和 `sync` / `sse` / `websocket` |
| `requested_model`, `upstream_model`, `response_model` | VARCHAR(200), nullable | 请求、路由和响应模型 |
| `outcome` | VARCHAR(32) | `succeeded` / `failed` / `blocked` / `client_cancelled` / `partial` |
| `status_code`, `upstream_status_code` | INTEGER, nullable | 客户端和上游状态码 |
| `error_stage`, `error_code` | VARCHAR(64), nullable | 不含正文的诊断分类 |
| `stream` | BOOLEAN | 是否流式 |
| `duration_ms`, `first_byte_ms` | INTEGER, nullable | 时序数据 |
| `input_tokens`, `output_tokens` | INTEGER, nullable | 可用时写入 |
| `request_capture_status`, `response_capture_status` | VARCHAR(24) | 主体采集状态摘要 |
| `request_bytes`, `response_bytes` | BIGINT | 原始字节数 |
| `expires_at`, `created_at`, `completed_at` | TIMESTAMPTZ | 留存与时序 |

索引：`created_at DESC, id DESC`、`request_id`、`trace_id`、`user_id + created_at`、`api_key_id + created_at`、`outcome + created_at`、`requested_model + created_at`、`expires_at`。

## `model_call_payloads`

正文一行对应一种内容或一次上游尝试。

| 字段 | 类型/约束 | 说明 |
|---|---|---|
| `id` | BIGSERIAL PK | 内部键 |
| `model_call_trace_id` | BIGINT FK CASCADE | 归属调用 |
| `kind` | VARCHAR(32) | `client_request`、`client_response`、`upstream_attempt`、`error_response` |
| `attempt_no` | INTEGER | 上游尝试序号；客户端正文为 0 |
| `capture_status` | VARCHAR(24) | `complete`、`truncated`、`redacted`、`not_applicable`、`failed` |
| `mime_type`, `content_encoding` | VARCHAR(128), nullable | 原始内容类型和传输语义 |
| `original_bytes`, `stored_bytes` | BIGINT | 原始与存储长度 |
| `sha256` | CHAR(64), nullable | 脱敏前字节哈希；不用于内容检索 |
| `redaction_version` | SMALLINT | 脱敏规则版本 |
| `ciphertext` | TEXT, nullable | 加密后的安全正文 |
| `created_at` | TIMESTAMPTZ | 创建时间 |

唯一约束：`(model_call_trace_id, kind, attempt_no)`。正文查询只按单 trace 与 kind 读取。

## `model_call_trace_cleanup_runs`

| 字段 | 说明 |
|---|---|
| `id`, `mode` | 自动或手动运行标识 |
| `requested_by` | 手动运行操作者；自动运行为空 |
| `cutoff_at` | 本次删除阈值 |
| `status` | `running`、`succeeded`、`failed`、`cancelled` |
| `deleted_traces`, `deleted_payloads`, `deleted_bytes` | 不含正文的结果统计 |
| `error_code` | 无敏感数据的失败分类 |
| `started_at`, `finished_at` | 生命周期 |

## Setting: `model_call_trace_config`

```json
{
  "enabled": false,
  "payload_capture_enabled": false,
  "auto_cleanup_enabled": false,
  "retention_days": 7
}
```

验证规则：只有 `retention_days` 在 1–90 时可保存；自动清理默认关闭，管理员显式开启后每日执行；开启正文采集时必须存在固定加密密钥。配置更新不删除历史数据。

## State Transitions

```text
started → succeeded
started → failed
started → blocked
started → client_cancelled
started → partial
```

终态不可回退。正文采集失败不改变调用终态，只改变相应 `capture_status`。
