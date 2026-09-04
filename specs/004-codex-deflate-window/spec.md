# Feature Specification: Codex Deflate Window Support

**Feature Branch**: `004-codex-deflate-window`

**Created**: 2026-09-04

**Status**: Draft

**Input**: Support the current OpenAI Codex WebSocket compression behavior for
RelayDeck OpenAI OAuth upstream connections.

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Complete a negotiated OAuth WebSocket session (Priority: P1)

An operator using an OpenAI OAuth subscription account can route a Codex WebSocket
session through RelayDeck when the upstream selects any compression window that the
current Codex client supports.

**Why this priority**: A successful handshake alone is insufficient if later
compressed client messages cannot be decoded by the upstream.

**Independent Test**: A local upstream selects each supported window value and
receives a compressed RelayDeck message that conforms to that selected value.

**Acceptance Scenarios**:

1. **Given** an OpenAI OAuth WebSocket route, **When** the upstream selects a
   supported client compression window, **Then** RelayDeck establishes the
   connection and exchanges compressed messages using that selected limit.
2. **Given** an OpenAI OAuth WebSocket route, **When** the upstream accepts
   compression without selecting a client window, **Then** RelayDeck uses the
   reference client's default maximum window and completes the session.

---

### User Story 2 - Reject incompatible compression negotiation (Priority: P2)

An operator receives a deterministic connection failure rather than a session
whose advertised compression settings disagree with its actual messages.

**Why this priority**: Rejecting an unsupported response at the handshake
boundary prevents opaque failures after an OAuth request has begun.

**Independent Test**: A local upstream selects an unsupported value and the
connection fails before it carries an application message.

**Acceptance Scenarios**:

1. **Given** an OpenAI OAuth WebSocket route, **When** the upstream selects a
   value outside the current Codex-supported range, **Then** RelayDeck rejects
   the handshake and does not send an application message on that connection.

---

### User Story 3 - Preserve unrelated WebSocket behavior (Priority: P3)

An operator continues to use existing non-OAuth and inbound WebSocket paths
without a new compression mode or changed default behavior.

**Why this priority**: The feature is a fidelity rule for a named upstream path,
not a system-wide compression policy change.

**Independent Test**: Existing focused WebSocket tests pass with the new
capability disabled for all paths except the OpenAI OAuth upstream dialer.

**Acceptance Scenarios**:

1. **Given** a WebSocket connection outside the OpenAI OAuth upstream path,
   **When** it is created, **Then** its existing compression behavior remains
   unchanged.

### Edge Cases

- The upstream selects no client compression-window value; this represents the
  reference client's maximum supported window.
- The upstream selects 8 or a value outside the reference client's supported
  range of 9 through 15; the connection is rejected before application data is
  sent.
- The upstream returns an invalid compression response; RelayDeck fails the
  handshake rather than removing or rewriting the parameter.
- Either direction disables context takeover; the corresponding direction resets
  compression context without changing its negotiated window limit.
- The OpenAI OAuth path uses an HTTP proxy or a pooled connection; the same
  negotiation rules apply.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: For OpenAI OAuth upstream WebSocket connections, RelayDeck MUST
  offer the reference client's compression extension value exactly as
  `permessage-deflate; client_max_window_bits`.
- **FR-002**: RelayDeck MUST accept each client compression-window value from
  9 through 15 selected by the upstream and constrain outgoing compressed
  messages to the selected value.
- **FR-003**: RelayDeck MUST use the reference client's default maximum window
  when the upstream accepts compression without selecting a client value.
- **FR-004**: RelayDeck MUST reject a selected value of 8 or any malformed,
  unsupported compression negotiation before application data is exchanged.
- **FR-005**: RelayDeck MUST preserve the negotiated context-takeover behavior
  independently for each traffic direction.
- **FR-006**: RelayDeck MUST apply this compatibility behavior only to the
  OpenAI OAuth upstream WebSocket dialer and leave other WebSocket callers at
  their current defaults.
- **FR-007**: RelayDeck MUST retain existing OAuth request headers, proxy
  behavior, pooling behavior, and message-size safeguards for the affected
  connections.
- **FR-008**: RelayDeck MUST not require an operator configuration change to
  enable this support for an OpenAI OAuth subscription account.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: Focused automated tests establish and exchange an application
  message for all seven supported values: 9, 10, 11, 12, 13, 14, and 15.
- **SC-002**: Focused automated tests confirm a selected value of 8 fails before
  an application message is received by the upstream.
- **SC-003**: Focused automated tests observe the exact reference compression
  offer on direct and proxied OpenAI OAuth upstream handshakes.
- **SC-004**: The existing focused OpenAI WebSocket client and pool test suites
  pass without changing the behavior asserted for unrelated callers.

## Assumptions

- The target reference is the current OpenAI Codex open-source implementation,
  whose supported client-window range is 9 through 15.
- This work applies only to the OpenAI OAuth upstream connection path; it does
  not alter API-key providers or RelayDeck's client-facing WebSocket behavior.
- A fixed, project-local replacement module may provide the missing codec
  capability until its upstream publishes an equivalent client implementation.
