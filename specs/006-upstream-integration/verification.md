# Integration verification

## Frozen inputs and source checks

- RelayDeck baseline: `8c3f7522b87e95076eeb03b841617a14fde65e29`.
- Upstream: `07bf8b92fda067516b7989412f09b75bb39bc113`.
- Common ancestor: `0d27f45ead1b58908548ec21afd923ecaf7339bc`.
- The merge incorporates 89 upstream-only commits (61 non-merge commits).
- Six text-conflicted files were reconciled. README sponsor exclusions and
  RelayDeck identity were retained; new Go imports use the RelayDeck module.
- `git diff --check HEAD` passed. No old upstream-owned identity remains under
  `backend/internal` or `frontend/src`.
- Existing SQL migrations are unchanged; seven new SQL migrations were added.
- The local coder/websocket replacement, module/lock files, modeltrace package,
  and Docker build definitions are unchanged.
- Ent group-generated files match upstream after module-path normalization.
  The combined schema, mutation, and runtime files retain the local API Key
  idempotency field alongside new upstream group fields.
- Existing codebase-graph coverage is stale for this integration worktree;
  migration, ORM, WebSocket, and frontend conclusions use current source files.

## Compatibility adjustments

1. The new usage-request-ID concurrent index uses RelayDeck's existing
   `dropUnusableIndexIfPresent`, retaining recovery for both invalid and unready
   indexes. A regression test covers recovery and the healthy/absent case.
2. The administrator UsageView test now includes RelayDeck's fourth, tracing
   tab and verifies switching away from tracing restores the normal filters.
3. Two pre-existing channel-monitor parameter-matrix cases used live DNS names.
   They now use a public IP literal, without sending requests, so DNS results
   cannot mask the intended missing-key/model assertions. Production endpoint
   and SSRF validation are unchanged.
4. Independent review identified a broken reactive binding in the upstream
   Codex manifest editor. A real GroupsView/dialog/field regression reproduced
   the missing UI update before the fix. The parent now applies emitted changes
   with `Object.assign` instead of replacing its reactive proxy. The regression
   covers enablement, fallback, account removal/selection, update payload, and
   reopening with API data. It and the four existing field tests pass after the
   two-line production fix.
5. Embedded-frontend tests still requested the historical `/logo.png` although
   RelayDeck already ships `/logo.svg`. Both resource tests now request the
   actual SVG and assert its MIME type and content. Static-serving production
   code and logo assets are unchanged.

## Executed checks

| Check | Result |
| --- | --- |
| Focused baseline tracing/migrations/compatibility/domain tests | Passed |
| `npx --yes pnpm@9 install --frozen-lockfile` | Passed; lockfile unchanged |
| Frontend `typecheck` and `lint:check` | Passed |
| Frontend UsageView and request-ID field tests | Passed, 18 tests |
| Backend `CI=1 go test -tags=integration -p 2 ./...` | Passed, including repository and service integration suites |
| Deployment shell syntax, Apple-container mocks, Compose security/gateway environment/runtime-resource checks, Caddy cache checks | Passed |
| Backend `go test -json -tags=unit ./...` | Passed; 18,214 passing test/subtest events, 0 failures, 24 skips |
| Backend `make build` | Passed |
| Backend `GOMAXPROCS=2 GOFLAGS=-tags=embed make build` | Passed with the final frontend bundle |
| Backend `GOMAXPROCS=2 go test -tags='unit embed' ./internal/web` | Passed, including the corrected static-resource fixtures |
| `GOMAXPROCS=2 GOMEMLIMIT=2GiB go run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.13.0 run --concurrency=2 --timeout=15m ./...` | Passed, 0 issues |
| `GOMAXPROCS=2 go test -race -p 2 -tags=unit ./internal/modeltrace ./internal/service/openai_ws_v2` | Passed, both complete packages |
| Focused service WebSocket/replay/encrypted-content race checks | Passed, 91 test/subtest events, no failures or skips |
| Frontend `vitest run --silent --maxWorkers=2 --minWorkers=2` | Final rerun passed, 258 files / 1,868 tests |
| Frontend `npx --yes pnpm@9 run lint:check` and `run build` | Final rerun passed, including TypeScript check |
| Independent integration review and focused re-review | Completed; one P1 reactive-binding issue fixed and independently confirmed closed |
| Local main fast-forward and final preservation checks | Pending |

Database integration ran only against disposable PostgreSQL/Redis containers.
The added upgrade test reconstructs the old RelayDeck schema, seeds API Key
idempotency, usage, trace/session, and attempt records, applies all migrations
twice, and verifies preserved records, opt-in defaults, complete migration
ledger, and usable indexes. Existing integration fixtures cover new installs.

The initial full frontend run exposed the old three-tab assertion; the initial
backend unit run exposed the two DNS-dependent assertions. These failures were
not accepted as successful validation; final rerun results are recorded above.

A subsequent full frontend run passed 1,866 of 1,867 tests but timed out in an
unchanged SettingsView test under concurrent build load. The final rerun limits
Vitest to two workers without changing assertions or timeouts. Redundant focused
Go tests and the first unfinished race/lint runs were stopped to reduce host
load; stopped checks are not counted as passed.

The installed Browserslist database and large frontend chunk warnings are
non-fatal. The Vue transformed
`const` model binding warning was investigated and fixed, not waived.
Dependency and lockfile updates are not part of this merge.

## Acceptance boundaries

- No production deployment, service restart, live database migration, push, or
  pull request is performed by this source-integration task.
- Browser-driven acceptance and real upstream-provider calls are not executed.
  Automated tests and builds do not claim those checks have passed.
- The primary checkout's three modified deployment files and two untracked
  deployment files remain outside the integration commit and must retain their
  pre-integration SHA-256 hashes.
