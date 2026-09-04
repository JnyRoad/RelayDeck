# Implementation Plan: Codex Deflate Window Support

**Branch**: `fix/codex-ws-deflate-offer` | **Date**: 2026-09-04 | **Spec**:
[spec.md](spec.md)

## Summary

RelayDeck must reproduce the current Codex client's OpenAI OAuth WebSocket
compression negotiation without claiming a smaller window than its encoder uses.
The implementation will replace the OpenAI-specific header rewrite with an
opt-in capability in a fixed project-local replacement of `coder/websocket`.
The replacement will offer the bare Codex parameter, accept only the Codex-supported
selected range (9–15), and construct its outgoing compressor with the selected
window. RelayDeck will opt in only in the OpenAI OAuth upstream dialer.

## Technical Context

**Language/Version**: Go 1.27.0

**Primary Dependencies**: `github.com/coder/websocket` v1.8.14, a fixed
project-local replacement of that module, and the already-direct
`github.com/klauspost/compress` v1.18.2

**Storage**: N/A; negotiated compression state exists only for a WebSocket
connection lifetime

**Testing**: Go `testing`, `testify`, local HTTP WebSocket servers, focused
project-local replacement tests, and the backend test/build gate

**Target Platform**: Linux and macOS hosts supported by RelayDeck's Go backend

**Project Type**: Web-service backend with a project-local Go module replacement

**Performance Goals**: Preserve existing message-size limits and connection
pooling; no added round trips or operator configuration

**Constraints**: The OpenAI OAuth upstream offer must be exactly
`permessage-deflate; client_max_window_bits`; selected values 9–15 require a
real encoder window of `2^N`; 8 and malformed selections must fail at handshake;
context takeover remains direction-specific

**Scale/Scope**: One upstream WebSocket dialer, one fixed project-local
replacement module, and focused tests. API-key providers and client-facing
WebSockets are out of scope.

## Constitution Check

| Principle | Evidence before design | Status |
|-----------|------------------------|--------|
| Canonical Product Identity | New source stays inside RelayDeck and uses its existing product identity. | Pass |
| Preserve Functional Interfaces Unless Named | The local capability is disabled by default; only the named OAuth upstream dialer opts in. | Pass |
| Evidence-Driven Delivery | Every protocol change has a failing unit or integration test before the minimum implementation. | Pass |
| Controlled Publication | This design creates no external repository or publication target. | Pass |
| Provider Transport Fidelity | The selected response value will configure the real outgoing compressor; no response header normalization remains. | Pass |

The post-design check remains passing: the local replacement is necessary because the current
library cannot encode a lower client window, while a header-only alternative
violates Principle VI.

## Project Structure

### Documentation

```text
specs/004-codex-deflate-window/
├── spec.md
├── research.md
├── data-model.md
├── contracts/
│   └── websocket-fork.md
├── quickstart.md
├── plan.md
└── tasks.md
```

### Source Code

```text
backend/
├── go.mod
├── go.sum
└── internal/service/
    ├── openai_ws_client.go
    └── openai_ws_client_test.go

backend/third_party/coder-websocket (project-local replacement module)
├── dial.go
├── dial_test.go
├── compress.go
├── compress_test.go
├── write.go
├── go.mod
└── go.sum
```

**Structure Decision**: The project-local replacement owns generic WebSocket
negotiation and encoding mechanics. RelayDeck owns only the OpenAI OAuth opt-in and its
integration tests. This preserves the existing internal `openAIWSClientDialer`
boundary and prevents a global compression policy change.

## Complexity Tracking

| Addition | Why Needed | Simpler Alternative Rejected Because |
|----------|------------|--------------------------------------|
| Project-local replacement module | The released library rejects selected client windows and uses a fixed 15-bit encoder. | Rewriting response headers can advertise an unsupported smaller encoder window. |
| Opt-in dial option | The generic replacement is used by other paths that must retain their existing defaults. | Enabling the behavior for every connection changes unrelated WebSocket semantics. |
