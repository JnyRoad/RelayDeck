# Research: Codex Deflate Window Support

## Decision: Mirror the official Codex supported range, not the RFC maximum

**Decision**: Accept upstream-selected `client_max_window_bits` values 9 through
15 and reject 8.

**Rationale**: RFC 7692 permits 8 through 15, but the current OpenAI-maintained
Tungstenite fork used by Codex explicitly defines its supported range as 9
through 15. Its default offer is bare `client_max_window_bits`, and its default
context settings retain context takeover. Matching current executable behavior
is the defined compatibility target.

**Alternatives considered**:

- Support RFC value 8: rejected because it would diverge from the reference
  client without an official implementation to verify.
- Offer only `permessage-deflate`: rejected because it changes the observed
  Codex handshake and prevents the server from selecting the client window.

## Decision: Use a project-local replacement of `coder/websocket` v1.8.14

**Decision**: Copy source commit `7d7c644330e727379c3e33fddc154ac208b925f3`
(v1.8.14) into `backend/third_party/coder-websocket`, preserve its ISC license,
apply the minimum client-side window support, and use it through a project-local
Go module replacement.

**Rationale**: RelayDeck's current library rejects a response containing
`client_max_window_bits`, and the Go standard compressor has only a 15-bit
window. The existing direct `klauspost/compress` dependency provides a custom
window writer, so the replacement adds no new supplier. Keeping the altered
source in RelayDeck avoids a private Go-module credential requirement in Docker
and CI while retaining public ISC license notices.

**Alternatives considered**:

- Use the current upstream `coder/websocket`: rejected because it cannot encode
  the selected lower window.
- Adopt upstream PR #534: rejected because it is unmerged and handles the
  server acceptance path, not RelayDeck's client `Dial` negotiation.
- Write a new WebSocket client in RelayDeck: rejected because it duplicates
  protocol, proxy, and control-frame logic already tested by the copied module.
- Keep the current response-header normalization: rejected because it can claim
  a smaller window while sending 15-bit output.

## Decision: Make the capability explicitly opt-in

**Decision**: Add one false-by-default `DialOptions` capability in the local replacement that
emits the bare offer and permits the selected response value. Only
`coderOpenAIWSClientDialer` enables it.

**Rationale**: The same generic WebSocket module is used by paths outside the
OpenAI OAuth upstream route. An opt-in preserves their current API and runtime
behavior while removing RelayDeck's OpenAI-specific transport header mutation.

**Alternatives considered**:

- Globally accept client window selections: rejected because it changes all
  callers' negotiation behavior without a scoped requirement.
- Keep the RelayDeck transport wrapper as the source of the offer: rejected
  because the protocol contract belongs with the codec that validates it.

## Decision: Prove selection and encoding separately

**Decision**: Local-replacement tests will prove header generation, 9–15 validation, 8
rejection, selected-window writer construction, and context behavior. RelayDeck
tests will prove the OAuth direct/proxy handshake, selected-window message flow,
and unaffected existing route contracts.

**Rationale**: A permissive 15-bit decoder can read a stream generated with a
smaller window, so a successful receive-only integration test cannot prove the
client encoder respected the selected limit. The dependency's focused test owns
the selection-to-writer mapping; RelayDeck tests own integration behavior.

**Alternatives considered**:

- Test only a 101 handshake: rejected because it does not exercise an outgoing
  compressed message.
- Infer correctness from the response header: rejected because that is the
  original failure mode.
