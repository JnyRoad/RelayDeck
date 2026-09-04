# Implementation Plan: 完整模型调用链追踪

**Branch**: `feat/call-chain-tracing` | **Date**: 2026-09-04 | **Spec**: [spec.md](./spec.md)

## Summary

在既有模型调用追踪的基础上，实现一次网关调用从客户端输入、路由、每一次上游尝试到客户端最终输出的完整、可检索链路。管理端列表只加载索引；独立详情默认以聊天方式回放由可靠标识连接的会话，并可切换到按时间排序的原始链路。正文保存完整文本和元数据、但始终排除认证材料；留存由管理员配置的有限天数控制。

## Technical Context

**Language/Version**: Go 1.27 后端；TypeScript、Vue 3 前端

**Primary Dependencies**: Gin、`database/sql`、PostgreSQL 15+、Vue I18n、Vitest

**Storage**: PostgreSQL 的 `model_call_traces`、`model_call_trace_attempts` 和 `model_call_payloads`；正文先安全归一化并加密后入库

**Testing**: Go `testing`（单元、路由和迁移集成测试）；Vitest + Vue Test Utils；前端类型检查和生产构建

**Target Platform**: RelayDeck 的 HTTP/SSE/WebSocket 模型网关和管理员 Web 界面

**Project Type**: Go API 服务 + Vue 单页管理端

**Performance Goals**: 列表查询不连接正文表且不读取/解密正文；追踪写入、上游观察与审计失败均不得改变网关客户端响应。

**Constraints**:

- 仅使用客户端显式的稳定会话 ID 或协议明确的 `previous_response_id` 连接会话；绝不按用户、Key、模型、IP 或时间猜测。
- 正常文本 prompt、模型输出、SSE 实际交付事件和每次上游请求/响应需完整保存；非文本媒体只保存元数据。
- API Key 明文、Authorization、Cookie、OAuth Token、密码和会话秘密永不进入正文、摘要、审计或前端状态。
- 留存天数仅允许 1–365；没有永久保留选项；清理只能删除追踪及其步骤/正文，不能影响用量、用户、Key 或账户。
- 管理端正文读取、会话读取、复制事件和清理必须留有不含正文的操作审计。

**Scale/Scope**: 一次调用可以包含多次上游尝试；会话详情按需读取已关联轮次的必要正文，列表和通常分析路径不读取正文。

## Constitution Check

| Principle | Status | Evidence / handling |
|---|---|---|
| Canonical Product Identity | Pass | 新文件、路由与界面使用 RelayDeck 现有命名。 |
| Preserve Functional Interfaces Unless Named | Pass | 不改变 `/v1/*` 客户端请求或响应契约；只增加管理员读取、审计和存储能力。 |
| Evidence-Driven Delivery | Pass | 每个行为任务先增加会失败的 Go/Vitest 测试，随后最小实现并运行相关检查。 |
| Controlled Publication | Pass | 本计划不包含部署、生产配置变更或发布操作。 |

复核结论：无需要例外说明的宪法冲突。

## Architecture and Data Flow

1. 网关中间件创建根调用记录，透明包装客户端请求/响应流，并在处理结束时从安全的服务端上下文写入归属、路由、模型、会话链接和终态。
2. 中间件把仅本次调用有效的追踪句柄放入 `context.Context`。`HTTPUpstream.Do` 与 `DoWithTLS` 仅在这个句柄存在时包装上游请求和响应；每次实际执行分配递增的尝试序号。请求错误立即形成失败步骤，响应在消费/关闭时形成响应步骤。所有观察写入均为 best-effort。
3. 迁移增加调用时身份快照、可靠会话字段和上游尝试表；每次尝试以独立元数据行关联其请求、响应或错误正文。旧记录保持可读，缺少可靠关联时仅显示单条记录。
4. 查询服务将索引、单条详情、会话详情和单正文解密分开。会话查询只依据同一显式会话 ID 或 `response_id`/`previous_response_id` 的明确链路递归查找。
5. 管理端打开表格行时显示全屏、可滚动详情弹窗：默认聊天回放，原始链路为第二页签。复制正文触发不含正文的审计事件；列表首屏绝不预取正文。

## Data Model

详见 [data-model.md](./data-model.md)。数据库迁移仅追加，不修改已部署的 `183_model_call_traces.sql`。

## API Contract

详见 [contracts/model-traces-admin-api.md](./contracts/model-traces-admin-api.md)。所有新端点位于现有管理员认证组，并由审计中间件记录敏感读取和复制。

## Project Structure

```text
backend/
├── migrations/232_model_call_trace_sessions_and_attempts.sql
├── internal/modeltrace/
│   ├── recorder.go                 # 根调用、正文和上游尝试的领域契约
│   ├── upstream_attempt.go         # context 观察器和尝试生命周期
│   ├── conversation.go             # 显式会话链接提取与查询服务
│   ├── repository.go               # 写入根记录、尝试和正文
│   ├── query.go                    # 索引、详情和会话的只读契约
│   └── query_repository.go         # 参数化 SQL 和递归会话查询
├── internal/repository/http_upstream.go
├── internal/server/middleware/model_call_trace.go
├── internal/handler/admin/model_trace_handler.go
└── internal/server/routes/admin.go

frontend/src/
├── api/admin/modelTrace.ts
├── components/admin/usage/ModelTracePanel.vue
├── components/admin/usage/ModelTraceDetailDialog.vue
├── components/admin/usage/__tests__/ModelTracePanel.spec.ts
├── components/admin/usage/__tests__/ModelTraceDetailDialog.spec.ts
└── i18n/locales/{zh,en}/admin/modelTrace.ts
```

## Delivery Phases

1. **Foundation**: migration, data contracts, identity snapshots, 1–365 retention validation, safe explicit session-link extraction, and audit-route classification.
2. **US1**: index filters and historical user/Key snapshots in the API and table.
3. **US2**: explicit conversation query and full-screen chat replay.
4. **US3**: upstream-attempt context capture, raw-chain API/view, copied-body audit, and stream/partial behavior.
5. **US4**: cleanup boundary, configuration range, documentation, type/build/test validation and manual browser acceptance.

## Complexity Tracking

| Added component | Why it is needed | Simpler alternative rejected because |
|---|---|---|
| `model_call_trace_attempts` | One call can contain retries, account changes and separate request/response/error results. | A single `upstream_attempt` blob cannot reliably expose each raw request, response, status and time independently. |
| Context-bound upstream observer | It records only actual gateway transport attempts without modifying every provider handler. | Instrumenting only final handler state loses retries/fallbacks; globally logging HTTP clients risks unrelated traffic. |
| Full-screen detail component | Chat replay needs stable metadata and a scrollable viewport without pushing the table down. | Rendering an expanding table row cannot provide readable multi-turn context or raw-chain navigation. |
