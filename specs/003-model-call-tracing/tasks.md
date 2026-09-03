# Tasks: 模型调用追踪

**Input**: [spec.md](spec.md)、[plan.md](plan.md)、[research.md](research.md)、[data-model.md](data-model.md)、[contracts](contracts/)

**Tests**: 所有行为任务必须先写并运行失败测试，再写最小实现；每完成一个任务立即勾选。

## Phase 1: 规格与基础

- [x] T001 固化规格、设计、数据模型、接口契约和验证指南于 `specs/003-model-call-tracing/`
- [x] T002 编写 `backend/internal/modeltrace` 的纯单元测试：正文脱敏、1 MiB 截断、状态与路由分类，于 `backend/internal/modeltrace/*_test.go`
- [x] T003 实现最小的追踪类型、状态、路由分类和脱敏/有界捕获器，于 `backend/internal/modeltrace/`
- [ ] T004 编写迁移结构与级联删除集成测试，于 `backend/internal/modeltrace/repository_integration_test.go`
- [x] T005 新增 `backend/migrations/183_model_call_traces.sql`，创建追踪头、正文、清理运行表、约束和索引

## Phase 2: 网关采集基础

- [ ] T006 编写 Gin 中间件行为测试：关闭时零写入、恢复请求体、同步成功、策略拦截、流式响应和正文故障不改变响应，于 `backend/internal/server/middleware/model_call_trace_test.go`
- [ ] T007 实现追踪 repository、配置存储和加密正文读取，于 `backend/internal/modeltrace/repository.go`、`config.go`、`service.go`
- [ ] T008 实现 API Key 鉴权后的请求/响应追踪中间件及完整 writer 代理，于 `backend/internal/server/middleware/model_call_trace.go`
- [ ] T009 编写并实现 `backend/internal/server/routes/gateway.go` 的模型路由覆盖测试与中间件接入；排除非调用路由
- [ ] T010 编写 WebSocket 每轮独立追踪的失败测试，于 `backend/internal/handler/openai_gateway_handler_trace_test.go`
- [ ] T011 在 `backend/internal/handler/openai_gateway_handler.go` 接入 `ResponsesWebSocket` 的轮次追踪

## Phase 3: 管理查询与留存（User Story 1、3）

- [ ] T012 [US1] 编写调用摘要列表、详情和正文按需读取的 handler/service 测试，于 `backend/internal/handler/admin/model_call_trace_handler_test.go`
- [ ] T013 [US1] 实现追踪筛选、游标分页、详情和正文读取服务，于 `backend/internal/modeltrace/query.go`
- [ ] T014 [US1] 实现管理员调用追踪 handler，并在 `backend/internal/handler/handlers.go`、`backend/internal/server/routes/admin.go`、`backend/cmd/server/wire.go`、`backend/cmd/server/wire_gen.go` 接入
- [ ] T015 [US3] 编写设置验证、清理预览、分批删除、自动清理 leader lock 和不影响用量记录的测试，于 `backend/internal/modeltrace/cleanup_test.go`
- [ ] T016 [US3] 实现设置更新、确认令牌、清理运行、每日调度、生命周期 stop 和操作审计，于 `backend/internal/modeltrace/config.go`、`cleanup.go`、`backend/cmd/server/*`

## Phase 4: 管理界面（User Story 1、2、3）

- [ ] T017 [US1] 编写管理 API 客户端与调用列表状态测试，于 `frontend/src/api/admin/__tests__/usage-traces.spec.ts` 和 `frontend/src/components/admin/usage/__tests__/ModelTraceTable.spec.ts`
- [ ] T018 [US1] 扩展 `frontend/src/api/admin/usage.ts` 的追踪类型和 API 客户端
- [ ] T019 [US1] 实现 `frontend/src/components/admin/usage/ModelTraceTable.vue` 和 `ModelTraceFilters.vue`，列表只显示摘要
- [ ] T020 [US2] 编写正文懒加载、截断提示和未授权错误展示测试，于 `frontend/src/components/admin/usage/__tests__/ModelTraceDetailDrawer.spec.ts`
- [ ] T021 [US2] 实现 `frontend/src/components/admin/usage/ModelTraceDetailDrawer.vue`，支持概览、客户端请求、模型响应和错误页签
- [ ] T022 [US3] 编写并实现 `frontend/src/components/admin/usage/ModelTraceRetentionPanel.vue`，覆盖配置、预览、确认和最近清理状态
- [ ] T023 [US1] 在 `frontend/src/views/admin/UsageView.vue` 增加懒加载“调用详情”页签，并从用量请求 ID 跳转到追踪筛选

## Phase 5: 验证与收敛

- [ ] T024 编写凭据/Base64 canary 不泄露、列表不查正文和 UI 不误称截断内容完整的跨层回归测试
- [ ] T025 运行后端相关单测、迁移集成测试、前端 Vitest、typecheck、build，并按 `quickstart.md` 完成人工隔离环境验证
- [ ] T026 运行规格一致性分析，处理所有 CRITICAL/HIGH 问题并更新本 `tasks.md`
- [ ] T027 完成代码自审：安全、流式不变量、权限、迁移、无关改动与注释规范

## Dependencies & Execution Order

- T002–T005 完成前不写网关采集代码。
- T006 必须先于 T008，T010 必须先于 T011，T012 与 T015 必须先于对应实现。
- T014 依赖 T007、T013；T016 依赖 T007；前端任务依赖 T014 的接口稳定。
- T024–T027 依赖所有前序实现任务。
