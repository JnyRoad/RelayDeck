# Quickstart Validation: 完整模型调用链追踪

## Prerequisites

- Local RelayDeck backend with PostgreSQL migration runner available.
- An administrator session in the frontend.
- A trace configuration with indexing and body capture enabled, and a retention value between 1 and 365 days.

## Automated validation

From `backend/`, run focused Go tests for `internal/modeltrace`, `internal/server/middleware`, `internal/repository`, and `internal/handler/admin`. Confirm tests cover:

1. Summary search by user/Key snapshot without body query.
2. Explicit three-turn session linkage and the single-turn unlinked boundary.
3. Two upstream attempts with request/response or error payloads in order.
4. Unredacted encrypted-body retention and recorder failure not changing the gateway response.
5. Retention validation and cleanup cascading attempts/payloads while leaving use records intact.

From `frontend/`, run the focused Vitest component tests, `vue-tsc --noEmit`, and the production build.

## Manual administrator acceptance

1. Open **Usage records → Model call tracing** and search by a known user and API Key. Verify the table shows historical attribution and no prompt/response text.
2. Open a middle turn in a known three-turn Responses conversation. Verify a full-screen, scrollable chat replay shows all three turns, marks the selected turn, and does not show unrelated calls.
3. Change to **Raw chain**. Verify the client request, route metadata, each numbered upstream attempt, response/error and final client response appear in time order; open one payload and verify its byte/hash/status metadata.
4. Copy one displayed body, then check the operation log: it contains an access action and identifiers only, never the copied content.
5. Set a shorter valid retention period, preview cleanup, confirm it, and verify expired traces/attempts/payloads disappear while usage records still exist.
