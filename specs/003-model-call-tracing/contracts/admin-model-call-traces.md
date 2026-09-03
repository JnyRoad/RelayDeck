# 管理端调用追踪接口契约

所有接口位于既有管理员鉴权下的 `/admin/usage`。列表不返回正文；正文接口只返回一类正文。

## 列表

`GET /admin/usage/traces`

查询参数：`cursor`、`page_size`（1–100）、`start_time`、`end_time`、`request_id`、`trace_id`、`user_id`、`api_key_id`、`group_id`、`account_id`、`model`、`route`、`protocol`、`outcome`、`capture_status`。

响应：`items`、`next_cursor`。每项包含调用头字段与请求/响应采集状态；不得包含 `ciphertext` 或已解密正文。

## 详情与正文

`GET /admin/usage/traces/:trace_id`

返回调用头、上游尝试摘要和可用正文种类，不含正文内容。

`GET /admin/usage/traces/:trace_id/payloads/:kind`

`kind` 仅允许 `client_request`、`client_response`、`error_response`。返回解密后的、已脱敏的正文及完整性元数据。若部署启用 step-up，本接口必须强制既有 step-up 检查。读取成功和失败均应写入操作审计，且审计内容不包含正文。

## 配置

`GET /admin/usage/trace-settings`

返回生效配置和最近一次清理运行摘要。

`PUT /admin/usage/trace-settings`

接收完整配置。必须验证保留天数、开关组合和加密前置条件；修改写入操作审计。

## 清理

`POST /admin/usage/traces/cleanup-preview`

接收可选截止时间；返回将删除的调用数、正文数、字节数和确认令牌，不执行删除。

`POST /admin/usage/traces/cleanup`

接收预览返回的确认令牌；创建清理运行并返回其运行摘要。若部署启用 step-up，必须强制既有 step-up 检查。

`GET /admin/usage/traces/cleanup-runs`

返回最近清理运行摘要；不得包含正文或请求内容。

## Error Contract

错误使用现有响应格式，并至少区分：`model_call_trace_invalid_filter`、`model_call_trace_not_found`、`model_call_trace_payload_unavailable`、`model_call_trace_invalid_config`、`model_call_trace_encryption_key_required`、`model_call_trace_cleanup_confirmation_invalid`。
