# Contract: RelayDeck Local WebSocket Capability

## Dependency Identity

- Source baseline: `github.com/coder/websocket` v1.8.14,
  `7d7c644330e727379c3e33fddc154ac208b925f3`
- Maintained source: `backend/third_party/coder-websocket`
- Consumer: RelayDeck's `backend/go.mod`, pinned through a project-local module
  replacement
- License: preserve the upstream ISC license and notices in the local module

## Client Dial Capability

The project-local replacement adds one false-by-default `DialOptions` field named
`CompressionClientMaxWindowBits`.

| Value | Preconditions | Handshake offer | Accepted response | Outgoing codec |
|-------|---------------|-----------------|-------------------|----------------|
| `false` | Any existing caller | Existing library behavior | Existing library behavior | Existing library behavior |
| `true` | Compression is enabled | `permessage-deflate; client_max_window_bits` | Omitted value means 15; explicit 9–15 accepted; 8 or invalid values rejected | Selected value bounds the client compressor |

`client_no_context_takeover` remains independent from the client window. It
resets only the client compressor history; it does not alter the selected
window. The analogous server setting remains independent for received messages.

## RelayDeck Integration Contract

Only `coderOpenAIWSClientDialer.Dial` sets
`CompressionClientMaxWindowBits: true`. It removes the OpenAI-only HTTP
transport rewriting and response normalization because the local replacement produces and
validates the extension contract itself.

The following must remain unchanged: caller-provided OAuth headers, proxy HTTP
client selection and reuse, 16 MiB read limit, error-body capture, pool-facing
`openAIWSClientConn` interface, and all non-OAuth WebSocket paths.
