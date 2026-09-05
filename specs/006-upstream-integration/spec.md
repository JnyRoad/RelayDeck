# Upstream integration, 2026-09-05

## Scope

Integrate upstream commit `07bf8b92fda067516b7989412f09b75bb39bc113`
into RelayDeck, starting at `8c3f7522b87e95076eeb03b841617a14fde65e29`.
The common ancestor is `0d27f45ead1b58908548ec21afd923ecaf7339bc`.
Preserve upstream ancestry, including all 89 upstream-only commits.

Included behavior: group Fast/free-Fast policies, model-scoped reasoning mapping
and ceilings, pinned-account Codex manifests, WebSocket replay and continuation
fixes, protocol/model compatibility, custom pricing reload, upstream request IDs,
proxy error attribution, payment reconciliation, and frontend fixes.

## Compatibility requirements

- Preserve RelayDeck identity, Go module path, deployment defaults, and README
  branding. Keep the existing exclusion of upstream sponsor advertisements.
- Preserve administrator target-user API Key management and idempotency,
  complete model-call tracing and replay, and the local coder/websocket fork.
- Preserve all previously committed SQL migrations byte-for-byte. New upstream
  migrations coexist with local migrations sharing a numeric prefix; execution
  and checksums are keyed by the full filename.
- Retain recovery for invalid or unready indexes when adding the new concurrent
  usage-request-ID index. Keep new policy defaults opt-in.
- Keep the primary checkout's five uncommitted deployment files unchanged.
- This task integrates source code; production deployment and live-data
  migrations are outside scope.

## Acceptance

Backend unit tests, frontend lint/typecheck/tests/build, backend build, targeted
WebSocket/tracing race checks, and database integration tests must verify the
combined code. Exercise both new-install and existing-schema migration paths in
disposable test databases. Any unavailable or failed check is recorded explicitly.
Review the integration before merging into the project main branch.
