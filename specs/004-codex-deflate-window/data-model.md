# Data Model: Codex Deflate Window Support

This feature has no persisted database entities. It adds one connection-scoped
negotiated state.

## Negotiated Deflate Policy

| Field | Valid values | Source | Lifetime | Rule |
|-------|--------------|--------|----------|------|
| Client window support offered | disabled or enabled | RelayDeck's OAuth upstream dialer | Handshake | Enabled only for the named OpenAI OAuth route. |
| Selected client window | 9–15; defaults to 15 when omitted | Upstream handshake response | Connection | Determines the outgoing client compressor's maximum window. |
| Client context takeover | enabled or disabled | Upstream handshake response | Connection | Controls whether outgoing client messages reuse compression history. |
| Server context takeover | enabled or disabled | Upstream handshake response | Connection | Controls whether received server messages reuse compression history. |

## State Transitions

```text
compression disabled
  └─ OAuth dialer enables Codex support → bare offer emitted
       ├─ server omits selection → selected client window = 15
       ├─ server selects 9–15 → selected client window = value
       └─ invalid or 8 selection → handshake fails; no connection state created
```

There is no migration, storage retention, or operator-visible configuration.
