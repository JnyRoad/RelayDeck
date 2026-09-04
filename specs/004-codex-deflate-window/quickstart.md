# Quickstart: Validate Codex Deflate Window Support

## Prerequisites

- A checkout of RelayDeck containing the project-local replacement source.
- Go 1.27.0 or the repository's backend build environment.

## Focused Local Replacement Validation

From `RelayDeck/backend/third_party/coder-websocket`, run:

```bash
go test ./...
```

Expected behavior: replacement-module tests confirm the exact bare offer, accepted values
9–15, rejected value 8, and that a selected window reaches the client writer.

## Focused RelayDeck Validation

From `RelayDeck/backend`, run:

```bash
go test ./internal/service -run 'TestCoderOpenAIWSClientDialer'
go test ./internal/service -run 'TestOpenAIWSConnPool'
```

Expected behavior: direct and proxy handshakes offer the Codex value exactly;
each supported selected value exchanges a message; an 8-bit selection fails
before application data; current pool tests remain green.

## Backend Gate

From the RelayDeck repository root, run:

```bash
go test ./backend/...
```

Expected behavior: all backend packages compile and tests pass. No live OAuth
account is needed for this deterministic local validation.
