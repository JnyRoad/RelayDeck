# Implementation Plan: 模型调用追踪

**Branch**: `003-model-call-tracing` | **Date**: 2026-09-03 | **Spec**: [spec.md](spec.md)

## Summary

为模型网关新增一个默认关闭、独立存储的调用追踪能力：认证后的网关中间件记录客户端请求和客户端可见响应，WebSocket 每轮单独记录；管理端在既有用量页提供调用详情页签、按需正文查看和独立留存清理。成本用量和提示词审计保持原有职责。

## Technical Context

**Language/Version**: Go 1.27、TypeScript/Vue 3
**Primary Dependencies**: Gin、PostgreSQL、Redis、Vue/Vite；不新增第三方依赖
**Storage**: PostgreSQL；正文以现有 AES-256-GCM `SecretEncryptor` 加密的 TEXT 保存
**Testing**: `go test`、Vitest、Vue typecheck/build
**Target Platform**: RelayDeck 后端服务与管理 Web 面板
**Project Type**: Web API + 单页管理面 + 定时后台任务
**Performance Goals**: 列表不读取正文；流式响应不因采集等待完整结果；每个正文最多 1 MiB
**Constraints**: 模型网关可用性高于追踪持久化；认证、Cookie、API Key、二进制和 Base64 不得保存
**Scale/Scope**: 仅模型网关调用；同步、SSE、WebSocket；自动保留 1–90 天

## Constitution Check

- 产品命名：通过；所有新增标识使用 RelayDeck 既有命名空间。
- 接口保留：通过；网关请求与响应协议不改变，追踪默认关闭。
- 证据驱动：通过；每项行为先写可失败测试，再写最小实现。
- 受控发布：通过；不执行部署、推送或远程变更。

## Project Structure

```text
backend/
├── internal/modeltrace/                 # 追踪领域、脱敏、设置、存储、清理
├── internal/server/middleware/          # Gin 请求/响应采集中间件
├── internal/handler/admin/              # 管理端追踪 API
├── internal/server/routes/              # 网关和管理路由接入
├── cmd/server/                          # Wire 依赖和生命周期
└── migrations/                          # 新追踪表

frontend/src/
├── api/admin/usage.ts                   # 追踪 API 客户端与类型
├── components/admin/usage/              # 追踪表、详情抽屉、留存策略
└── views/admin/UsageView.vue            # 新调用详情页签
```

**Structure Decision**: 追踪领域集中在 `internal/modeltrace`，避免污染计费用量与提示词安全审计；网关采集只通过明确的中间件和 WebSocket 接入点跨模块调用。

## Execution Design

1. 新增 SQL 迁移和 `modeltrace.Repository`，以独立表持久化头、正文和清理运行；列表 query 只读头表。
2. 新增解析/脱敏/有上限捕获器。它恢复被读取的 HTTP 请求体，包装 Gin writer 捕获实际写出的同步和 SSE 响应，且完整实现必要的 flush/hijack 转发语义。
3. 网关中间件仅在 API Key 鉴权成功后运行。显式路由分类表排除模型列表、用量查询、下载和纯媒体内容；路由覆盖测试锁定其与 `gateway.go` 的一致性。
4. 新增 WebSocket 轮次追踪，避免把连接视为单次调用。
5. 通过现有 `SecretEncryptor` 加密脱敏后正文。正文采集故障只标记 capture 状态并记录无敏感日志，绝不改变网关结果。
6. 新增管理员 handler，列表/详情/正文/设置/清理预览接口分离。正文读取与删除经既有管理员和可用的 step-up 门控，写入现有操作审计。
7. 新增 `ModelTraceCleanupService`，采用 Redis 领导锁、每日固定调度、按 ID 分批删除和清理运行表；与 Ops 清理配置分离。
8. 在 `UsageView` 增加懒加载的调用详情页签、筛选、详情抽屉和留存面板；原用量页签不改变。

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| 新追踪领域模块 | 必须隔离高敏感正文、留存和查询职责 | 把正文加入用量或提示词审计会混合不同数据生命周期与安全语义 |
| HTTP writer 包装 | 必须捕获真实写给客户端的 SSE 内容而不破坏流式 | 仅在 handler 结束后读取响应不可用于流式，且会遗漏统一错误路径 |
