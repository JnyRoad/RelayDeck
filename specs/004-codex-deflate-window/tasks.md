# Tasks: Codex Deflate Window Support

**Input**: Design documents from `specs/004-codex-deflate-window/`

**Prerequisites**: `plan.md`, `spec.md`, `research.md`, `data-model.md`, and
`contracts/websocket-fork.md`

**Tests**: Required. Every behavior change follows red-green-refactor: write the
focused test, run it and observe the expected failure, then write the smallest
implementation and rerun it.

## Phase 1: Setup

**Purpose**: Add the project-local replacement without changing its behavior.

- [x] T001 Copy `github.com/coder/websocket` v1.8.14 source and its ISC license into `backend/third_party/coder-websocket/` with a nested `go.mod` that retains the original module path.
- [x] T002 Add the local module replacement for `github.com/coder/websocket` in `backend/go.mod` and update `backend/go.sum` without enabling the new dial capability.
- [x] T003 Run the existing WebSocket module suite from `backend/third_party/coder-websocket/` and the focused baseline in `backend/internal/service/openai_ws_client_test.go` to prove the copied baseline matches the released behavior.

---

## Phase 2: Foundational TDD Contracts

**Purpose**: Capture every missing behavior before changing the local module or
RelayDeck integration.

- [x] T004 Add failing unit cases for the opt-in bare Codex offer, selected values 9–15, default 15, and rejected value 8 in `backend/third_party/coder-websocket/dial_test.go`.
- [x] T005 Add a failing writer-selection case that proves the selected client window reaches the outgoing client compressor in `backend/third_party/coder-websocket/compress_test.go`.
- [x] T006 Add failing direct and proxy OAuth upstream cases for selected values 9–15 and rejected value 8 in `backend/internal/service/openai_ws_client_test.go`.
- [x] T007 Run the new focused tests from T004–T006 and record expected failures caused by the released module's unsupported `client_max_window_bits` behavior.

**Checkpoint**: The missing protocol behavior is reproducible by tests before
implementation begins.

---

## Phase 3: User Story 1 - Complete a negotiated OAuth WebSocket session (Priority: P1) 🎯 MVP

**Goal**: An OAuth upstream connection negotiates and actually encodes with any
Codex-supported selected client window.

**Independent Test**: A local direct or proxied upstream selects each value from
9 to 15, receives a RelayDeck compressed application message, and returns a
message RelayDeck can read.

- [x] T008 [US1] Implement the false-by-default `CompressionClientMaxWindowBits` dial option and exact bare-offer generation in `backend/third_party/coder-websocket/dial.go` and `backend/third_party/coder-websocket/compress.go`.
- [x] T009 [US1] Implement response validation and selected 9–15 client-window state in `backend/third_party/coder-websocket/dial.go`, defaulting an omitted selection to 15.
- [x] T010 [US1] Implement selected-window writer construction and context-takeover-safe writer pooling in `backend/third_party/coder-websocket/compress.go` and `backend/third_party/coder-websocket/write.go`.
- [x] T011 [US1] Run `go test ./...` from `backend/third_party/coder-websocket/` and confirm T004–T005 pass.
- [x] T012 [US1] Enable `CompressionClientMaxWindowBits` only in `backend/internal/service/openai_ws_client.go` and remove the OpenAI-only request/response header normalization.
- [x] T013 [US1] Run focused direct and proxy cases in `backend/internal/service/openai_ws_client_test.go` and confirm all selected values 9–15 pass.

**Checkpoint**: Supported negotiated windows complete an OAuth upstream session
without a header/codec mismatch.

---

## Phase 4: User Story 2 - Reject incompatible compression negotiation (Priority: P2)

**Goal**: An unsupported response cannot become a partially functional OAuth
WebSocket connection.

**Independent Test**: A local upstream selects 8 and observes no application
message because RelayDeck rejects the handshake.

- [x] T014 [US2] Implement the explicit 8 and malformed-selection rejection path in `backend/third_party/coder-websocket/dial.go` while preserving independent context-takeover handling.
- [x] T015 [US2] Run the local-replacement and RelayDeck rejected-selection tests in `backend/third_party/coder-websocket/dial_test.go` and `backend/internal/service/openai_ws_client_test.go` and confirm T006's failure case now passes.

**Checkpoint**: Unsupported negotiation fails before application data can be
exchanged.

---

## Phase 5: User Story 3 - Preserve unrelated WebSocket behavior (Priority: P3)

**Goal**: Existing callers retain their default WebSocket behavior.

**Independent Test**: Existing pool and non-opt-in local-module tests pass with
the new option unset.

- [x] T016 [US3] Add a default-disabled regression case in `backend/third_party/coder-websocket/dial_test.go` and a non-opt-in RelayDeck assertion in `backend/internal/service/openai_ws_client_test.go`.
- [x] T017 [US3] Run the default-disabled tests and existing pool tests in `backend/internal/service/openai_ws_pool_test.go` to confirm no new behavior outside the OAuth upstream dialer.

**Checkpoint**: The capability is opt-in and unrelated callers preserve their
current defaults.

---

## Phase 6: Polish and Validation

- [x] T018 Run `gofmt` over modified Go files in `backend/third_party/coder-websocket/` and `backend/internal/service/`.
- [x] T019 Run `go test ./...` from `backend/third_party/coder-websocket/`, `go test ./internal/service` from `backend/`, and `go test ./...` from `backend/`.
- [x] T020 Review `backend/go.mod`, `backend/go.sum`, `backend/third_party/coder-websocket/LICENSE.txt`, and the RelayDeck diff for project-local scope, license retention, and absence of external repository references.
- [x] T021 Re-run the validation scenarios in `specs/004-codex-deflate-window/quickstart.md` and mark the completed tasks in this file.

## Dependencies and Execution Order

T001 → T002 → T003 → T004–T007 → T008–T013 → T014–T015 → T016–T017 →
T018–T021.

User Story 1 is the MVP. User Story 2 depends on its negotiation boundary;
User Story 3 follows after the opt-in capability exists. No tasks are marked
parallel because the test-first sequence changes shared module state.

## Implementation Strategy

1. Establish the local dependency baseline and red tests.
2. Complete the supported-window path and validate it before moving to rejection.
3. Add rejection and non-opt-in regressions.
4. Run the local module and backend validation gates, then inspect the complete
   diff for scope and license requirements.
