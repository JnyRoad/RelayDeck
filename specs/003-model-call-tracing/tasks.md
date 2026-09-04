# Tasks: 模型调用追踪

**Input**: [spec.md](spec.md)、[plan.md](plan.md)、[research.md](research.md)、[data-model.md](data-model.md)、[contracts](contracts/)

**Tests**: 所有行为任务必须先写并运行失败测试，再写最小实现；每完成一个任务立即勾选。

## Phase 1: 规格与基础

- [x] T001 固化规格、设计、数据模型、接口契约和验证指南于 `specs/003-model-call-tracing/`
- [x] T002 编写 `backend/internal/modeltrace` 的纯单元测试：正文脱敏、1 MiB 截断、状态与路由分类，于 `backend/internal/modeltrace/*_test.go`
- [x] T003 实现最小的追踪类型、状态、路由分类和脱敏/有界捕获器，于 `backend/internal/modeltrace/`
- [x] T004 编写迁移结构与级联删除集成测试，于 `backend/internal/modeltrace/migration_integration_test.go` 与 `repository_integration_test.go`
- [x] T005 新增 `backend/migrations/183_model_call_traces.sql`，创建追踪头、正文、清理运行表、约束和索引

## Phase 2: 网关采集基础

- [x] T006 编写 Gin 中间件行为测试：请求体恢复、同步/流式写入、取消、部分写入和正文故障不改变响应，于 `backend/internal/server/middleware/model_call_trace_test.go`
- [x] T007 实现追踪 repository、配置存储和加密正文读取，于 `backend/internal/modeltrace/repository.go`、`config_store.go`、`service.go`
- [x] T008 实现 API Key 鉴权后的请求/响应追踪中间件及完整 writer 代理，于 `backend/internal/server/middleware/model_call_trace.go`
- [x] T009 实现模型路由白名单和全局中间件接入，并以路由分类测试排除非调用路由
- [x] T010 编写 WebSocket 每轮独立追踪、超限和释放状态测试，于 `backend/internal/modeltrace/websocket_test.go`
- [x] T011 在 `backend/internal/handler/openai_gateway_handler.go` 接入 `ResponsesWebSocket` 的轮次追踪

## Phase 3: 管理查询与留存（User Story 1、3）

- [x] T012 [US1] 编写调用摘要、详情元数据和单正文按需读取的 handler/service 测试，于 `backend/internal/handler/admin/model_trace_handler_test.go`
- [x] T013 [US1] 实现追踪筛选、分页、详情元数据和单正文读取服务，于 `backend/internal/modeltrace/query.go`
- [x] T014 [US1] 实现管理员调用追踪 handler，并在 `backend/internal/server/routes/admin.go`、`backend/cmd/server/wire.go`、`backend/cmd/server/wire_gen.go` 接入
- [x] T015 [US3] 编写设置验证、清理预览、分批删除、取消后运行收尾和自动清理测试，于 `backend/internal/modeltrace/cleanup_test.go`
- [x] T016 [US3] 实现设置更新、清理运行、每日调度和生命周期 stop，于 `backend/internal/modeltrace/config_store.go`、`cleanup.go`、`backend/cmd/server/*`

## Phase 4: 管理界面（User Story 1、2、3）

- [x] T017 [US1] 编写管理 API 客户端与调用列表状态测试，于 `frontend/src/components/admin/usage/__tests__/ModelTracePanel.spec.ts`
- [x] T018 [US1] 新增 `frontend/src/api/admin/modelTrace.ts` 的追踪类型和 API 客户端
- [x] T019 [US1] 实现 `frontend/src/components/admin/usage/ModelTracePanel.vue`，列表只显示摘要
- [x] T020 [US2] 编写正文按需加载与详情竞态防护测试，于 `frontend/src/components/admin/usage/__tests__/ModelTracePanel.spec.ts`
- [x] T021 [US2] 在 `ModelTracePanel.vue` 中实现概览、客户端请求、模型响应和错误正文页签
- [x] T022 [US3] 在 `ModelTracePanel.vue` 中实现配置、预览和确认清理
- [x] T023 [US1] 在 `frontend/src/views/admin/UsageView.vue` 增加“调用详情”页签

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
