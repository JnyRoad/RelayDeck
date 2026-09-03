# Feature Specification: Host Codex App-Server Bridge

**Feature Branch**: `002-host-app-server-bridge`

**Created**: 2026-09-02

**Status**: Ready for planning

**Input**: User description: "Allow the RelayDeck container to use the Codex
app-server and authenticated Codex session that run on this Mac. RelayDeck is
reachable only on the local network. Do not mount or copy the host's complete
Codex home directory into the container."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Add an Official App-Server Account from the Host (Priority: P1)

An administrator of a RelayDeck instance running on a Mac can begin the
official app-server login in RelayDeck and complete the authorization with the
Codex runtime already operating on that Mac.

**Why this priority**: This is the requested outcome: a containerized
RelayDeck instance must be able to use the administrator's local Codex runtime
without requiring a second Codex installation or credential copy inside the
container.

**Independent Test**: With the host bridge available and an administrator
logged into RelayDeck, start a device-code authorization, complete it in the
browser, and create an official app-server account that can be used by
RelayDeck.

**Acceptance Scenarios**:

1. **Given** a running host bridge associated with the administrator's local
   Codex session, **When** the administrator starts an official app-server
   device-code login from RelayDeck, **Then** RelayDeck displays the
   authorization information supplied by that host runtime.
2. **Given** a completed authorization, **When** the administrator creates
   the account in RelayDeck, **Then** the account is available for the
   supported official app-server workflow without exporting the host session's
   credentials into the RelayDeck application data.

---

### User Story 2 - Keep the Host Runtime Private (Priority: P2)

An operator can run RelayDeck for devices on the local network without making
the host Codex runtime a separate service that those devices can call.

**Why this priority**: The host runtime represents the administrator's Codex
identity and must not become a general-purpose local-network endpoint.

**Independent Test**: Inspect the deployed connection boundary and attempt an
unauthorized connection from a non-authorized client; verify that it cannot
start or control a host app-server session.

**Acceptance Scenarios**:

1. **Given** RelayDeck is accessible on the local network, **When** another
   local-network device reaches the RelayDeck host, **Then** it cannot directly
   access or control the host Codex runtime.
2. **Given** an unauthorized connection attempt reaches the host bridge,
   **When** it does not present the deployment's dedicated connection
   credential, **Then** it is rejected without disclosing Codex session data.

---

### User Story 3 - Diagnose an Unavailable Host Runtime (Priority: P3)

An administrator receives a clear, non-secret error when the Mac host runtime
is stopped, unavailable, or no longer authorized.

**Why this priority**: The host runtime depends on the Mac remaining available;
operators need a recoverable diagnosis rather than a generic login failure.

**Independent Test**: Stop the host bridge while RelayDeck is running and
start an official app-server login; verify that the resulting error identifies
the unavailable host runtime and the recovery action.

**Acceptance Scenarios**:

1. **Given** the host bridge is unavailable, **When** the administrator starts
   an app-server login, **Then** RelayDeck fails the request promptly with a
   message that identifies the local host runtime as unavailable.
2. **Given** the bridge becomes available again, **When** the administrator
   starts a new login, **Then** the login can proceed without redeploying
   RelayDeck or changing its other account records.

### Edge Cases

- The Mac is asleep, powered off, or its Codex runtime has stopped: RelayDeck
  must report that this host dependency is unavailable and must not fall back
  to a copied host credential.
- The dedicated bridge credential is missing, unreadable, or mismatched:
  RelayDeck must reject the connection and report a configuration problem
  without printing the credential.
- A deployment does not opt into the host bridge: existing official app-server
  behavior remains selected; no host service is assumed.
- A device on the local network can reach RelayDeck: it must not gain a direct
  route to the host Codex runtime merely because RelayDeck is reachable.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: The system MUST allow an operator to opt a containerized
  RelayDeck deployment into using a Codex app-server runtime that remains on
  the same Mac as the deployment host.
- **FR-002**: The system MUST preserve the host Codex session on the Mac and
  MUST NOT copy or mount the host's complete Codex credential directory into
  the RelayDeck container or application data.
- **FR-003**: The bridge between RelayDeck and the host runtime MUST require a
  deployment-specific connection credential and reject unauthenticated
  clients.
- **FR-004**: The host runtime MUST not be directly controllable by arbitrary
  devices on the local network solely because RelayDeck is accessible there.
- **FR-005**: The system MUST support the existing official app-server login
  lifecycle through the host runtime, including starting, observing,
  completing, and cancelling a login.
- **FR-006**: When the host runtime is unavailable, unreachable, or rejects
  the bridge credential, the system MUST give the administrator a clear,
  non-secret recovery message.
- **FR-007**: Deployments that do not opt into the host bridge MUST retain
  their existing official app-server runtime selection behavior.
- **FR-008**: The supplied deployment materials MUST document setup, normal
  operation, credential rotation, and rollback without requiring users to
  inspect source code.

### Key Entities *(include if feature involves data)*

- **Host runtime endpoint**: The administrator-controlled local Codex
  app-server service that owns the Mac's Codex session and accepts only
  authorized bridge connections.
- **Bridge credential**: A deployment-specific secret that authorizes
  RelayDeck to communicate with the host runtime without granting access to
  the host's complete Codex credential store.
- **Host-managed app-server profile**: The account reference created after a
  successful official app-server login, whose active credential ownership
  remains with the host runtime.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: On a configured Mac host, an administrator can complete one
  official app-server device-code login and create its RelayDeck account in a
  single attempt without installing a second Codex runtime in the container.
- **SC-002**: A missing or stopped host runtime produces an actionable,
  non-secret failure within 15 seconds of the administrator starting a login.
- **SC-003**: A connection without the dedicated bridge credential cannot
  obtain a usable app-server session or authorization data.
- **SC-004**: Removing the host-bridge configuration and restarting the
  affected RelayDeck application restores its previous runtime-selection
  behavior without modifying database, Redis, or existing account data.

## Assumptions

- RelayDeck is reachable by devices on the local network, but the host bridge
  is intended only for the local Docker deployment on this Mac.
- The Mac is a user-controlled machine with an active official Codex session
  and must remain powered on for host-managed app-server accounts to work.
- The deployment has one trusted administrator responsible for creating,
  rotating, and revoking the bridge credential.
- The scope is limited to the local Mac deployment. Remote servers, multiple
  host runtimes, and automatic failover are out of scope.
