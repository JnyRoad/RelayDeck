# 管理端调用追踪接口契约

所有接口位于既有管理员鉴权下的 `/admin/model-traces`。列表和详情不返回正文；正文接口一次只返回一类正文。

## 列表

`GET /admin/model-traces`

查询参数：`page`、`page_size`（1–200）、`start_time`、`end_time`、`request_id`、`trace_id`、`user_id`、`api_key_id`、`group_id`、`account_id`、`requested_model`、`route`、`protocol`、`outcome`、`capture_status`。

响应：`items` 和分页统计。每项包含调用头字段与请求/响应采集状态；不得包含 `ciphertext` 或已解密正文。

## 详情与正文

`GET /admin/model-traces/:traceID`

返回调用头、上游尝试摘要和可用正文种类，不含正文内容。

`GET /admin/model-traces/:traceID/payloads/:kind?attempt_no=0`

`kind` 仅允许 `client_request`、`client_response`、`error_response`。返回解密后的、已脱敏的正文及完整性元数据。若部署启用 step-up，本接口必须强制既有 step-up 检查。读取成功和失败均应写入操作审计，且审计内容不包含正文。

## 配置

`GET /admin/model-traces/config`

返回生效配置和最近一次清理运行摘要。

`PUT /admin/model-traces/config`

接收完整配置。必须验证保留天数、开关组合和加密前置条件；修改写入操作审计。

## 清理

`GET /admin/model-traces/cleanup-preview`

返回将删除的调用数、正文数、字节数和截止时间，不执行删除。

`POST /admin/model-traces/cleanup`

由管理界面在展示预览后要求管理员明确确认；创建清理运行并返回其运行摘要。若部署启用 step-up，必须强制既有 step-up 检查。

## Error Contract

错误使用现有响应格式，并至少区分：`model_call_trace_invalid_filter`、`model_call_trace_not_found`、`model_call_trace_payload_unavailable`、`model_call_trace_invalid_config`、`model_call_trace_encryption_key_required`、`model_call_trace_cleanup_confirmation_invalid`。
