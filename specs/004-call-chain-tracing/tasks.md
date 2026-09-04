# Tasks: 完整模型调用链追踪

**Input**: [plan.md](./plan.md), [spec.md](./spec.md), [research.md](./research.md), [data-model.md](./data-model.md), [contracts/model-traces-admin-api.md](./contracts/model-traces-admin-api.md)

**Tests**: 必须按 TDD 执行：每个行为测试先运行并确认因功能缺失而失败，再写最小生产代码使其通过。

## Phase 1: Specification setup

- [X] T001 Create the implementation plan and research decisions in `specs/004-call-chain-tracing/plan.md` and `specs/004-call-chain-tracing/research.md`.
- [X] T002 Define trace entities, administrator contracts and acceptance steps in `specs/004-call-chain-tracing/data-model.md`, `specs/004-call-chain-tracing/contracts/model-traces-admin-api.md`, and `specs/004-call-chain-tracing/quickstart.md`.

## Phase 2: Foundational trace contracts (blocking)

**Purpose**: Establish additive storage, safe data contracts and audit boundaries before adding any UI behavior.

- [X] T003 Add a failing migration integration test for trace snapshots, session fields, attempts and cascade cleanup in `backend/internal/modeltrace/migration_integration_test.go`.
- [X] T004 Add `backend/migrations/232_model_call_trace_sessions_and_attempts.sql` and extend `backend/internal/modeltrace/{recorder.go,repository.go}` for persisted snapshots, session metadata, attempt records and upstream payload kinds.
- [X] T005 Add failing tests for explicit session/response-lineage extraction and unlinked-call isolation in `backend/internal/modeltrace/conversation_test.go`.
- [X] T006 Implement explicit session metadata extraction and conversation query/read models in `backend/internal/modeltrace/{conversation.go,query.go,query_repository.go}`.
- [X] T007 Add failing tests that sensitive trace reads, copy events and cleanup publish content-free audit data in `backend/internal/handler/admin/model_trace_handler_test.go` and `backend/internal/server/middleware/audit_log_test.go`.
- [X] T008 Add audited admin route classification and copy-event validation in `backend/internal/handler/admin/model_trace_handler.go`, `backend/internal/server/routes/admin.go`, and `backend/internal/server/middleware/audit_log.go`.

**Checkpoint**: Existing and new trace records can coexist; tracing configuration validates finite retention; all protected read paths are authenticated and auditable.

## Phase 3: User Story 1 — 按调用人或 Key 定位调用 (P1)

**Goal**: 管理员能按调用时的用户/Key 身份或现有关联键定位调用，列表显示完整归属快照但不返回正文。

**Independent Test**: 插入多个用户和 Key 的追踪记录后，按 user、api_key、请求 ID 或链路 ID 仅返回正确摘要；改名/删除后的记录仍显示历史快照。

- [X] T009 [US1] Add failing snapshot/filter query tests in `backend/internal/modeltrace/query_test.go` and handler parser tests in `backend/internal/handler/admin/model_trace_handler_test.go`.
- [X] T010 [US1] Persist non-sensitive user, Key, group and account snapshots from `backend/internal/server/middleware/model_call_trace.go` and expose searchable summaries through `backend/internal/modeltrace/{query.go,query_repository.go}` and `backend/internal/handler/admin/model_trace_handler.go`.
- [X] T011 [US1] Add failing user/Key filter and no-body-list assertions in `frontend/src/components/admin/usage/__tests__/ModelTracePanel.spec.ts`.
- [X] T012 [US1] Add user/Key filters and historical attribution columns while preserving summary-only list loading in `frontend/src/api/admin/modelTrace.ts`, `frontend/src/components/admin/usage/ModelTracePanel.vue`, and both `frontend/src/i18n/locales/*/admin/modelTrace.ts` files.

**Checkpoint**: User Story 1 is independently usable from the list without opening or decrypting any body.

## Phase 4: User Story 2 — 聊天式会话回放 (P1)

**Goal**: 从任意已关联轮次打开独立、可滚动的聊天详情；无可靠关联时严格只显示本轮。

**Independent Test**: 三轮同一显式会话从中间轮打开时按顺序显示三轮，当前轮被标识；相同用户但无会话标识的记录不被合并。

- [X] T013 [US2] Add failing conversation endpoint and response-lineage ordering tests in `backend/internal/modeltrace/query_test.go` and `backend/internal/handler/admin/model_trace_handler_test.go`.
- [X] T014 [US2] Add `GET /:traceID/conversation` in `backend/internal/handler/admin/model_trace_handler.go` and `backend/internal/server/routes/admin.go`, backed by exact session/lineage querying in `backend/internal/modeltrace/{conversation.go,query.go,query_repository.go}`.
- [X] T015 [US2] Add failing dialog behavior tests for full, current-turn-focused chat replay and unlinked status in `frontend/src/components/admin/usage/__tests__/ModelTraceDetailDialog.spec.ts`.
- [X] T016 [US2] Implement `frontend/src/components/admin/usage/ModelTraceDetailDialog.vue` and connect it from `frontend/src/components/admin/usage/ModelTracePanel.vue` and `frontend/src/api/admin/modelTrace.ts` with lazy body reads only after detail opens.

**Checkpoint**: User Story 2 provides a single scrollable chat replay with a stable metadata header and no heuristic grouping.

## Phase 5: User Story 3 — 完整原始链路与上游尝试 (P1)

**Goal**: 原始链路按实际顺序展示客户端、路由、每个上游尝试、错误或响应和最终客户端输出。

**Independent Test**: 一个先失败再成功的网关请求生成两个有独立顺序、模型、账户、请求和结果的尝试；流式中断显示实际交付内容及 partial 状态。

- [X] T017 [US3] Add failing transport-boundary tests for separate upstream request/response/error capture and no gateway-response change on observer failure in `backend/internal/repository/http_upstream_test.go` and `backend/internal/server/middleware/model_call_trace_test.go`.
- [X] T018 [US3] Implement a context-bound attempt observer in `backend/internal/modeltrace/upstream_attempt.go`; wire it through `backend/internal/server/middleware/model_call_trace.go` and instrument both `Do` and `DoWithTLS` in `backend/internal/repository/http_upstream.go`.
- [X] T019 [US3] Add failing selected-upstream-payload and copy-event tests in `backend/internal/handler/admin/model_trace_handler_test.go`.
- [X] T020 [US3] Expose ordered attempts and one selected upstream request/response/error payload via `backend/internal/modeltrace/{query.go,query_repository.go}` and `backend/internal/handler/admin/model_trace_handler.go`.
- [X] T021 [US3] Add failing raw-chain tab, chronological retry and on-demand payload tests in `frontend/src/components/admin/usage/__tests__/ModelTraceDetailDialog.spec.ts`.
- [X] T022 [US3] Implement the raw-chain tab, content metadata display and content-free copy audit event in `frontend/src/components/admin/usage/ModelTraceDetailDialog.vue`, `frontend/src/api/admin/modelTrace.ts`, and locale files.

**Checkpoint**: User Story 3 exposes every captured upstream attempt without placing raw content in a table or audit log.

## Phase 6: User Story 4 — 有限留存与清理 (P2)

**Goal**: 管理员可设置有限留存、预览/确认清理，且清理只移除到期追踪链及其步骤/正文。

**Independent Test**: 1、365 通过，0、负数、非整数和 366 被拒绝；到期根记录清理后其尝试/正文不可读，独立用量记录仍可读。

- [X] T023 [US4] Add failing 1–365 validation, preview totals and attempt-cascade tests in `backend/internal/modeltrace/{config_store_test.go,cleanup_test.go,migration_integration_test.go}`.
- [X] T024 [US4] Complete retention validation and cleanup aggregation in `backend/internal/modeltrace/{config_store.go,cleanup_repository.go}` without touching usage data.
- [X] T025 [US4] Add failing front-end retention range and preview/confirmation assertions in `frontend/src/components/admin/usage/__tests__/ModelTracePanel.spec.ts`.
- [X] T026 [US4] Update retention UI range, cleanup messaging and localized labels in `frontend/src/components/admin/usage/ModelTracePanel.vue` and `frontend/src/i18n/locales/{zh,en}/admin/modelTrace.ts`.

**Checkpoint**: User Story 4 bounds storage by policy and has an explicit, quantified deletion confirmation.

## Phase 7: Bounded full-content persistence and replay remediation

**Purpose**: Preserve the approved full trace while bounding each gateway capture, administrator raw-body read and conversation page.

- [X] T027 Add failing migration and repository tests for independently encrypted 256 KiB payload chunks, aggregate metadata and cascade cleanup in `backend/internal/modeltrace/migration_integration_test.go` and `backend/internal/modeltrace/repository_integration_test.go`.
- [X] T028 Add `backend/migrations/235_model_call_trace_payload_chunks.sql` and extend `backend/internal/modeltrace/{recorder.go,repository.go,service.go}` with bounded chunk stream creation, ordered append and fail-closed finalization.
- [X] T029 Add failing gateway/transport/WebSocket tests showing bodies larger than one chunk preserve every chunk without one unbounded capture buffer in `backend/internal/server/middleware/model_call_trace_test.go`, `backend/internal/modeltrace/upstream_attempt_test.go`, and `backend/internal/modeltrace/websocket_test.go`.
- [X] T030 Replace unbounded client, upstream and WebSocket body aggregation with the chunk-stream observer in `backend/internal/server/middleware/model_call_trace.go` and `backend/internal/modeltrace/{upstream_attempt.go,websocket.go}`.
- [X] T031 Add failing bounded conversation query tests for a 51-turn explicit session, current-turn anchoring, cursor direction and constant batch detail loading in `backend/internal/modeltrace/query_test.go` and `backend/internal/handler/admin/model_trace_handler_test.go`.
- [X] T032 Implement `limit`/cursor conversation pages and batch detail hydration in `backend/internal/modeltrace/{query.go,query_repository.go}` and `backend/internal/handler/admin/model_trace_handler.go`.
- [X] T033 Add failing bounded raw-body page tests for 1 MiB continuation and a complete multi-page result in `backend/internal/modeltrace/query_test.go` and `backend/internal/handler/admin/model_trace_handler_test.go`.
- [X] T034 Implement chunk-page decrypt/query contracts in `backend/internal/modeltrace/{query.go,query_repository.go}` and `backend/internal/handler/admin/model_trace_handler.go`.
- [X] T035 Add failing front-end tests for older/newer conversation continuation and sequential raw-body loading in `frontend/src/components/admin/usage/__tests__/ModelTraceDetailDialog.spec.ts`.
- [X] T036 Implement user-controlled conversation continuation and raw-body continuation in `frontend/src/api/admin/modelTrace.ts` and `frontend/src/components/admin/usage/ModelTraceDetailDialog.vue`.
- [X] T037 Run focused non-database backend, Vitest, type, lint and production-build checks; record isolated migration checks separately in this task list.

**Checkpoint**: New full bodies are stored and read in bounded increments; reliable long conversations are replayed from bounded pages without losing any retained turn.

## Phase 8: Verification and convergence

- [ ] T038 Run focused backend tests and targeted migration integration tests from `backend/`, recording the exact commands and results in the implementation handoff.
- [X] T039 Run focused Vitest tests, `frontend/node_modules/.bin/vue-tsc --noEmit`, scoped ESLint and `frontend/node_modules/.bin/vite build`.
- [ ] T040 Perform the manual browser acceptance sequence in `specs/004-call-chain-tracing/quickstart.md` against the new local frontend port and a real database only after explicit runtime authorization.
- [X] T041 Re-run a Spec Kit consistency review and update unfinished tasks in `specs/004-call-chain-tracing/tasks.md` before reporting completion.

### Verification notes

- T038 remains open: focused package tests passed, but migration integration assertions were skipped because no isolated `MODEL_TRACE_TEST_POSTGRES_DSN` was configured; they must never be pointed at the live Docker database.
- T040 remains open: the independent local frontend loaded at `127.0.0.1:15174`, but its browser session had no administrator login state. No real database migration, trace configuration change, or cleanup was attempted.
- T041 completed manually: this checkout has no `.specify/` prerequisite script, so the automated Spec Kit analyzer cannot run. A cross-read of `spec.md`, `plan.md`, `data-model.md`, API contract, and this task list aligned the implementation with explicit user-controlled paging; the two runtime-dependent items above remain explicitly unverified.

## Dependencies and execution order

- T003–T008 are foundational and block all stories.
- US1 (T009–T012) requires foundation.
- US2 (T013–T016) requires session foundation and can start after T006/T008; it does not depend on UI changes in US1 except shared types.
- US3 (T017–T022) requires foundation and can proceed after T004/T008; it shares dialog work with US2, so T022 follows T016.
- US4 (T023–T026) requires migration foundation but is otherwise independent of the first three stories.
- T037–T041 require all selected implementation tasks.

## Parallel opportunities

- After T004, T005 and T007 can be prepared in separate files; this execution remains sequential because the current task does not authorize subagents.
- After T008, backend US1 and the frontend US1 test can proceed independently; frontend dialogue work follows the stable conversation contract.

## Implementation strategy

Deliver foundation first, then make US1 searchable and independently testable. Add explicit chat replay (US2), then upstream forensic attempts (US3), followed by retention UX (US4). Do not claim a completed chain until the focused backend tests, frontend checks and manual browser validation all pass.
