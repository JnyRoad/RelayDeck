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
| Local main fast-forward and final preservation checks | Passed; merge `de30f7f93bba46d05857ddcfe34748d0e90dfee4`, 0 missing commits through the frozen upstream tip |

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

## Post-merge evidence

- Local main fast-forwarded to the reviewed two-parent merge commit
  `de30f7f93bba46d05857ddcfe34748d0e90dfee4`.
- `git merge-base --is-ancestor 07bf8b92fda067516b7989412f09b75bb39bc113 HEAD`
  succeeded; `git rev-list --count HEAD..07bf8b92fda067516b7989412f09b75bb39bc113`
  returned `0`.
- The merged `backend` and `frontend` trees are identical to the validated
  integration branch. The original deployment changes remain unstaged/untracked.
- Post-merge `GOMAXPROCS=2 go test -tags=unit ./migrations` passed in the main
  checkout without accessing any database.
- All five deployment-file SHA-256 values match their pre-integration values.
- Upstream deletion of two old sponsor images is included; their original
  contents remain recoverable from the baseline Git commit.

## PR #12 review repairs (2026-09-05)

The review baseline is `aba5e3516448d40be9e0caa2d706459b43dd8c9e` on
`chore/integrate-upstream-20260905`. Repairs use an isolated worktree; local
`main` and its five deployment edits are not changed.

The review contains 12 inline findings, two outside-diff findings, and one
redundant-condition cleanup. The follow-up is restricted to those findings and
their regression coverage. Runtime fixes preserve the existing public API,
schema, lockfiles, upstream history, and local WebSocket replacement.

- Channel-pricing failure attribution now counts only accounts passing the
  other eligibility gates. Mixed RPM/model-cooldown regressions failed before
  the fix; quota, platform, and model-support exclusions are also covered.
- Managed proxy names colliding with `unknown` or `direct/no_proxy` use the
  existing `proxy` label in event snapshots and legacy JSON reads. IDs and stored
  proxy names are retained; no new proxy-name validation policy or data migration
  is introduced. Both collision cases failed before the fix.
- Group fixtures now save both reasoning-effort policy fields. A PostgreSQL
  round-trip failed before the fix; the full group repository suite and relevant
  auth projections pass after it. Schema checks also assert both Fast flags
  default to false.
- Grok video-content usage combines status and content headers (content wins);
  Gemini's HTTP retry-exhausted `countTokens` estimate preserves request ID and
  response headers. The separate transport-error fallback is unchanged.
- Free Fast treats unavailable Standard pricing as recoverable and keeps the
  baseline usage record, while other pricing errors still propagate.
- The one-hour channel-price finding is not a production defect:
  `applyChannelTokenPriceOverrides` already copies `CacheWrite1hPrice` into
  `CacheCreation1hPrice`, and the resolver calls that helper. A new regression
  checks both resolved pricing and calculated cost and passed without changing
  pricing production code. No redundant assignment was added.
- Codex manifest cache bodies now have a 64 MiB aggregate budget alongside the
  existing combined per-entry 1 MiB limit and 512-entry cap. Tests cover byte
  accounting on insert, replacement, expiry, and oldest-entry eviction. The
  refresh test waits for a fresh expiry without dispatching extra requests; the
  malformed fixture now reaches and asserts the non-array error path.
- WebSocket pending requests count as active before `response.created`; early
  upstream EOF fails instead of reporting a completed relay. Existing adapter
  error-close handling is retained. Non-request binary-frame tests use a
  session-level payload so transport observation is tested independently.
- Frontend identifier-copy feedback includes identifier type as well as value;
  Grok SSO import passes the configured request-ID header in `extra`. The
  redundant manifest enabled-hint condition is removed without styling changes.

Fresh repair validation:

| Check | Result |
| --- | --- |
| PostgreSQL group suite, relevant API-Key auth projections, schema/default assertions | Passed (`CI=1`, disposable PostgreSQL/Redis only) |
| Frontend full Vitest | Passed, 258 files / 1,870 tests |
| Frontend full lint and production build (including type check) | Passed |
| WebSocket relay full race suite | Passed, 7.183s |
| Pre-fix combined service regression | 54 passed, 3 expected failures: Gemini attribution, Grok attribution, and Free Fast missing-price usage |
| Final full backend unit suite | Passed, 18,239 passing test/subtest events, 0 failures, 24 skips; service package 199.987s |
| Independent repair review (two bounded passes) | No actionable findings in the final repair diff |

Reproducible backend command:
`GOMAXPROCS=2 GOMEMLIMIT=2GiB go test -json -p 2 -gcflags='github.com/JnyRoad/RelayDeck/internal/service=-l' -tags=unit ./...`.
Default-compiler static checks and latest-head GitHub results are reported in
the PR timeline; the local regression result does not substitute for those checks.

The first service regression attempts exposed a new test's incorrect stub field
and an unrelated missing 429 persistence dependency. The test now uses a 503
response to exercise the same HTTP retry-exhaustion branch without invoking
rate-limit persistence. Failed or stopped runs are not passing evidence. Local
service regression compilation disables inlining for that package only to
reduce memory pressure; source, assertions, and CI compiler settings are unchanged.

### External-check boundary

The initial PR checks passed for backend tests/lint/security, frontend
tests/lint/typecheck/build/security, and shell checks. CLA failed because 12
historical upstream commit authors have not signed this repository's agreement;
the PR author's signature is already recognized. This repair does not sign on
anyone's behalf or modify CLA enforcement. CodeRabbit's docstring-coverage
warning is advisory and does not justify unrelated comment churn.

No production deployment, live-provider call, browser acceptance, or PR merge is
performed by this review repair. Any new CI result must be checked against the
repair's latest SHA rather than the original integration SHA.
