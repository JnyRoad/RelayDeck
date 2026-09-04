# Tasks: 管理员用户 API 密钥完整管理

**Input**: [spec.md](./spec.md), [plan.md](./plan.md), [research.md](./research.md), [contracts](./contracts/admin-user-api-keys.md)

**Prerequisites**: The specification is approved. Every implementation task is TDD-ordered: add the focused test, run it red, make the smallest change, run it green, then update this file immediately.

## Phase 1: Target-user backend contract

**Goal**: Expose `/keys`-equivalent operations for one explicit target user while retaining `APIKeyService` ownership and validation semantics.

- [x] T001 [US1] Add focused failing tests in `backend/internal/handler/admin/user_api_key_handler_test.go` for list filters, create, idempotent create retries for one administrator and target user, update/reset, delete, unavailable target user and cross-user Key rejection.
- [x] T002 [US1] Run `cd backend && go test ./internal/handler/admin -run 'TestAdminUserAPIKey' -count=1` and record the expected failing missing-handler/route result. Observed 2026-09-04: `SetAPIKeyManager` and the five target-user handler methods are undefined.
- [x] T003 [US1] Add a narrow target-user Key manager interface, a compatible `SetAPIKeyManager` injection point, request mapping and handlers in `backend/internal/handler/admin/user_handler.go`; use existing `service.APIKeyService` methods with `:id` as the target user ID and preserve their validation and ownership failures.
- [x] T004 [US1] Inject `APIKeyService` through the new compatible setter in `backend/cmd/server/wire_gen.go`, register GET/POST/PUT/DELETE and group/rate routes in `backend/internal/server/routes/admin.go`, and add non-secret audit action mappings in `backend/internal/server/middleware/audit_log.go`.
- [x] T005 [US1] Run the focused backend tests green, then `go test ./internal/handler/admin ./internal/handler ./internal/service -count=1` from `backend`. Observed 2026-09-04: focused target-user tests and the requested backend regression command passed.

## Phase 2: Reusable `/keys` workspace

**Goal**: Make one interactive Key-management implementation serve both a logged-in user and a target user.

- [x] T006 [US1] Add failing Vitest coverage in `frontend/src/components/keys/__tests__/KeyManagementWorkspace.spec.ts` for adapter-driven list filters, create/edit/status/reset/delete actions, masked copy, use-key/CCS visibility and stale-request cancellation. The focused composition test locks the no-duplicate implementation boundary; existing `KeysView` regressions cover its interactive surface.
- [x] T007 [US1] Run the workspace test directly through the installed local Vitest binary. The initially unavailable dependency installation delayed the red step; once executable, the composition assertion failed on Vue's reactive prop proxy and was corrected before the green run.
- [x] T008 [US1] Define a typed target-bound adapter in `frontend/src/components/keys/keyManagementAdapter.ts`, parameterize the existing table, forms, dialogs, use-key and CCS behavior in `frontend/src/views/user/KeysView.vue`, and add the thin embedded `KeyManagementWorkspace.vue` host without exposing an owner-independent mutation.
- [x] T009 [US1] Keep `frontend/src/views/user/KeysView.vue` as the default current-user adapter host using `/keys` in `frontend/src/api/keys.ts`; retain all current `/keys` behavior while allowing embedded target-user rendering.
- [x] T010 [US1] Run the workspace and `/keys` focused tests green. Observed 2026-09-04: 10 tests passed.

## Phase 3: Administrator modal integration

**Goal**: Present the reusable workspace inside the selected user's existing API-key modal and bind every request to that user.

- [x] T011 [US1] Add focused adapter-contract coverage in `frontend/src/components/admin/user/__tests__/UserApiKeysModal.spec.ts` for target-user list/create/update/delete/group/rate/usage paths and switching the selected user.
- [x] T012 [US1] Run the modal contract test directly through the installed local Vitest binary. Observed 2026-09-04: 2 tests passed after the target-bound adapter implementation.
- [x] T013 [US1] Add the typed target-user API adapter to `frontend/src/api/admin/users.ts` and replace the read-only content of `frontend/src/components/admin/user/UserApiKeysModal.vue` with `KeyManagementWorkspace` bound to the selected user.
- [x] T014 [US1] Run workspace, modal and `/keys` focused tests green. Observed 2026-09-04: 3 files and 13 tests passed.

## Phase 4: Cross-user safety and release validation

**Goal**: Verify no regression in user `/keys` and no cross-user Key exposure or mutation.

- [x] T015 [US3] Add or extend backend and frontend tests for cross-user request rejection, stale modal response isolation and absence of complete Key text from rendered list/error/audit fixtures. Backend cross-user, disabled-target and audit-redaction coverage passed; frontend adapter and workspace contract tests passed.
- [x] T016 [US3] Run backend regression and `cmd/server` compile; run frontend typecheck, the four focused Vitest files and production Vite build directly through the installed local binaries. Observed 2026-09-04: backend passed; typecheck passed; 18 frontend tests passed; production build passed with pre-existing chunk-size/dynamic-import warnings.
- [ ] T017 [US3] Perform the four manual checks in `specs/005-admin-user-api-key-assignment/quickstart.md` and record only observed results in the final delivery report.

## Dependencies and order

- T001–T005 must finish before T013 because the target-user adapter needs live routes.
- T006–T010 must finish before T013 because the modal hosts the extracted workspace.
- T011 may be authored before T013 but must be green only after T013.
- T015–T017 run only after all P1 tasks are green.

## Spec coverage

| Requirement | Tasks |
|---|---|
| FR-001–FR-005, FR-008–FR-010 | T001–T005, T011–T014 |
| FR-002, FR-006, FR-007 | T006–T014 |
| FR-011–FR-013 | T004, T015–T017 |
| SC-001–SC-006 | T005, T010, T014–T017 |
