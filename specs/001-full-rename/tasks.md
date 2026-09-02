# Tasks: Full RelayDeck Rename

**Input**: Design documents from `/specs/001-full-rename/`

**Prerequisites**: [plan.md](plan.md), [spec.md](spec.md),
[research.md](research.md), [data-model.md](data-model.md),
[breaking-identity-contract.md](contracts/breaking-identity-contract.md)

**Tests**: Tests are required. The focused configuration test is changed first,
observed failing, then made green before the bulk rename. Final repository
validation covers backend, frontend, and the scoped source scan.

## Phase 1: Scope Preparation

**Purpose**: Establish the repeatable code-only boundary and replacement map.

- [X] T001 Inventory case-varied former-name occurrences in tracked code and
  configuration files under `backend/`, `frontend/`, `deploy/`, `.github/`,
  `skills/`, root Dockerfiles, Makefiles, manifests, and ignore files.
- [X] T002 Record the canonical mapping and excluded historical/legal content in
  `specs/001-full-rename/{spec.md,plan.md,research.md,data-model.md,contracts/breaking-identity-contract.md,quickstart.md}`.

---

## Phase 2: Foundational Red-Green Gate

**Purpose**: Prove the primary configuration default must change before source
implementation begins.

- [X] T003 [P] Change the expected default identity values in
  `backend/internal/config/config_test.go` to RelayDeck values.
- [X] T004 Run the focused test in `backend/internal/config/config_test.go` and
  record the expected failure caused by the still-legacy implementation.
- [X] T005 Update the corresponding default identity implementation in
  `backend/internal/config/` and run the focused test until it passes.

**Checkpoint**: The test-first gate is green before the mechanical rename.

---

## Phase 3: User Story 1 - Run the Renamed Product (Priority: P1) 🎯 MVP

**Goal**: Build the backend under the RelayDeck module and runtime identity.

**Independent Test**: The Go test/build commands resolve all self-imports from
`github.com/JnyRoad/RelayDeck`.

- [X] T006 [US1] Rewrite Go module declaration and self-imports in
  `backend/go.mod` and `backend/**/*.go` to the canonical repository path.
- [X] T007 [US1] Replace owned product identifiers in backend runtime,
  migrations, generated code, tests, and scripts under `backend/`, including
  protocol labels and default persistence prefixes.
- [X] T008 [US1] Format changed Go files under `backend/` and rerun the focused
  configuration test.

**Checkpoint**: The backend contains only RelayDeck product identifiers in the
in-scope code boundary.

---

## Phase 4: User Story 2 - Deploy a New RelayDeck Instance (Priority: P2)

**Goal**: Provide fresh deployment and release definitions with RelayDeck-only
names.

**Independent Test**: Deployment, CI, and root build metadata refer to RelayDeck
services, artifacts, images, and source repository values.

- [X] T009 [P] [US2] Rewrite release, container, and CI identity values in
  `Dockerfile*`, `.goreleaser*`, `.github/workflows/`, `Makefile`, and
  `.dockerignore`.
- [X] T010 [US2] Rewrite runtime/deployment identifiers in `deploy/`, including
  service units, install scripts, Compose resources, defaults, and configuration
  templates.
- [X] T011 [US2] Rename legacy-named project tooling and deployment paths under
  `deploy/` and `skills/`, then update every in-scope code reference.

**Checkpoint**: New deployment definitions expose no former product service,
container, database, cache, environment, or file-path identifiers.

---

## Phase 5: User Story 3 - Configure a Client for RelayDeck (Priority: P3)

**Goal**: Generate RelayDeck-only frontend and client configuration.

**Independent Test**: Focused client setup tests emit only RelayDeck provider,
environment, storage, and protocol identifiers.

- [X] T012 [US3] Rewrite owned identity values, generated configuration, UI
  defaults, i18n text, tests, and package metadata in `frontend/`.
- [ ] T013 [US3] Run focused generated-client tests in
  `frontend/src/components/keys/__tests__/UseKeyModal.spec.ts` and update any
  relay-specific expectations.
- [X] T014 [US3] Replace product-owned former-name URLs in `frontend/src/` with
  the canonical repository URL or remove them when no RelayDeck destination is
  supplied.

**Checkpoint**: Newly generated client setup and browser-scoped identifiers are
RelayDeck-only.

---

## Phase 6: Cross-Cutting Validation

**Purpose**: Prove the code-only destructive rename and compilation integrity.

- [X] T015 Run the case-insensitive tracked-code scan defined in
  `specs/001-full-rename/quickstart.md` and resolve every in-scope match.
- [X] T016 Run `go test ./...` and `go build ./cmd/server` from `backend/`.
- [ ] T017 Run `pnpm --dir frontend run typecheck` and
  `pnpm --dir frontend run build`.
- [X] T018 Run `git diff --check`, confirm changed legacy-named paths are gone,
  and document any unverified Docker registry or DNS state in
  `specs/001-full-rename/quickstart.md`.

## Dependencies & Execution Order

- Phase 1 precedes all source changes.
- T003 through T005 are mandatory red-green gates before T006.
- User Story 1 must complete before final backend validation.
- User Stories 2 and 3 can proceed after T005 but are executed serially here to
  preserve a clear bulk-change audit trail.
- Phase 6 requires all preceding tasks.

## Implementation Strategy

1. Establish the code-only scope and test-first evidence.
2. Rename backend module and runtime identifiers.
3. Rename deployment/release resources and relevant paths.
4. Rename frontend/client identities.
5. Validate the zero-legacy-token invariant and both builds without publishing.
