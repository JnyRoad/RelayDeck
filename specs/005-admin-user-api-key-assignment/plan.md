# Implementation Plan: 管理员用户 API 密钥完整管理

**Branch**: `005-admin-user-api-key-assignment` | **Date**: 2026-09-04 | **Spec**: [spec.md](./spec.md)

**Input**: Approved full-parity requirement for using the user-management modal as a target-user version of `/keys`.

## Summary

Create explicit administrator routes scoped by target user ID and delegate every Key operation to the existing `APIKeyService` with that ID, preserving current business validation, owner checks and runtime cache invalidation. `UserHandler` receives this capability through a narrow internal interface and a setter, so existing constructor call sites and handler tests remain compatible while the production wire injects the concrete service. Parameterize the existing `KeysView` interactive surface with a typed ownership-bound adapter, then host that same surface through a thin reusable workspace in the administrator modal. This avoids duplicating its existing dialogs, use-Key panel and CCS import behavior.

## Technical Context

**Language/Version**: Go 1.27.0; TypeScript 5.6; Vue 3.4

**Primary Dependencies**: Gin, Ent, PostgreSQL; Vite, Vue Test Utils, Vitest, Pinia, vue-i18n

**Storage**: Existing PostgreSQL `api_keys` table and current administrator audit log; no schema migration

**Testing**: Go `testing` + Testify; Vitest + Vue Test Utils; `vue-tsc`

**Target Platform**: RelayDeck web application and API server

**Performance Goals**: Preserve server-side filtering and pagination; do not load another user's Keys after a stale modal response.

**Constraints**: Full behavior parity with `/keys`; list rendering stays masked; full Key may be copied but never added to notifications, errors or audit bodies; each Key read/mutation verifies target ownership.

**Scale/Scope**: One existing target-user modal, one existing Key page, five target-user API routes, no database migration.

## Constitution Check

- **Canonical product identity**: No product identity or release metadata changes.
- **Preserve functional interfaces unless named**: Existing `/keys` routes and behavior remain unchanged; administrator routes are additive and target-user scoped.
- **Evidence-driven delivery**: Every backend route and reused workspace behavior receives a focused failing test before implementation.
- **Controlled publication**: No commit, push, deployment or external publication is part of this plan.

The gate passes: no constitution exception is needed.

## Project Structure

```text
backend/
├── cmd/server/wire_gen.go                              # Inject existing APIKeyService through UserHandler's narrow setter
├── internal/handler/admin/user_handler.go              # Target-user Key handlers and request mapping
├── internal/handler/admin/user_api_key_handler_test.go # New focused route/ownership tests
├── internal/server/routes/admin.go                     # Add target-user Key routes
└── internal/server/middleware/audit_log.go             # Map new sensitive routes to non-secret audit actions

frontend/
├── src/api/keys.ts                                     # Current-user adapter for reusable workspace
├── src/api/admin/users.ts                              # Target-user adapter endpoints
├── src/components/keys/KeyManagementWorkspace.vue      # Reusable `/keys` behavior without page chrome
├── src/components/keys/__tests__/KeyManagementWorkspace.spec.ts
├── src/components/admin/user/UserApiKeysModal.vue       # Host workspace for selected target user
├── src/components/admin/user/__tests__/UserApiKeysModal.spec.ts
└── src/views/user/KeysView.vue                          # Shared interactive Key surface with default current-user adapter
```

**Structure Decision**: `KeysView` remains the sole interactive implementation and accepts a typed adapter plus embedded mode. `KeyManagementWorkspace` is the modal-safe host; `KeysView` supplies its existing current-user adapter by default, while `UserApiKeysModal` supplies the selected user's administrator adapter. This prevents behavioral drift while keeping page chrome and modal chrome separate.

## Implementation Sequence

1. Write backend handler tests covering successful target-user list/create/update/delete, invalid target and cross-user Key rejection; run them red.
2. Add a narrow `targetUserAPIKeyManager` contract and `SetAPIKeyManager` on `UserHandler`, then add target-user request mapping and handlers. Inject the concrete `APIKeyService` through the setter in the production wire, register routes and audit actions, and use the URL target user ID for every service call; run backend tests green.
3. Write a focused workspace test covering a supplied API adapter, masked display/copy, mutation refresh and stale-owner response discard; run it red.
4. Parameterize the existing `/keys` surface with the typed adapter and embedded mode, add the thin `KeyManagementWorkspace` host, and pass the original `KeysView` regression test.
5. Add the administrator adapter and host workspace in `UserApiKeysModal`; add modal tests for target ID propagation, create/update/delete/copy and stale target switches; run tests green.
6. Run typecheck, focused regressions and production build. Perform the quickstart manual acceptance before reporting completion.

## Complexity Tracking

| Violation | Why Needed | Simpler Alternative Rejected Because |
|---|---|---|
| Shared interactive Key surface | `/keys` and the administrator modal must remain fully behaviorally identical. | Copying the current page into the modal would create two large, independently drifting Key-management implementations. |
