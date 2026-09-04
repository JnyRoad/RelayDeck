# Feature Specification: Full RelayDeck Rename

**Feature Branch**: `001-full-rename`

**Created**: 2026-09-01

**Status**: Ready for planning

**Input**: User description: "Perform a complete breaking rename from the legacy
product identity to RelayDeck across all source, test, runtime, build, release,
deployment, documentation, legal, and historical artifacts. Use
github.com/JnyRoad/RelayDeck as the canonical source repository and leave no
compatibility layer."

## User Scenarios & Testing *(mandatory)*

### User Story 1 - Run the Renamed Product (Priority: P1)

An operator obtains the RelayDeck source and can build and start the application
using only RelayDeck names in its user-visible product defaults, runtime
identifiers, and build outputs.

**Why this priority**: A complete source and artifact identity is the minimum
deliverable of the requested breaking rename.

**Independent Test**: Build the backend and frontend, inspect generated runtime
metadata, and scan the in-scope files for the previous product identity.

**Acceptance Scenarios**:

1. **Given** a clean checkout, **When** the operator builds RelayDeck, **Then**
   its module, binary, container, and default service identifiers use RelayDeck.
2. **Given** the in-scope source and configuration files, **When** a
   case-insensitive legacy-name scan runs, **Then** it returns zero matches.

---

### User Story 2 - Deploy a New RelayDeck Instance (Priority: P2)

An operator preparing a new deployment can use the supplied service and
container configuration without encountering a previous product identity in a
runtime name, default database, cache key, volume, network, or environment
variable.

**Why this priority**: The rename is intentionally breaking, so every new
deployment surface must describe a single coherent product identity.

**Independent Test**: Render the deployment files and inspect their service,
image, data, and environment identifiers.

**Acceptance Scenarios**:

1. **Given** the provided deployment configuration, **When** an operator creates
   a fresh instance, **Then** all default names identify RelayDeck.
2. **Given** an existing instance using previous names, **When** it is upgraded,
   **Then** it receives no compatibility alias or automatic data migration.

---

### User Story 3 - Configure a Client for RelayDeck (Priority: P3)

A developer follows generated client configuration and UI guidance that refers
only to RelayDeck providers, environment variables, browser storage, cache
keys, and protocol labels.

**Why this priority**: Generated client setup is executable configuration and
must not leak the obsolete name after a full rename.

**Independent Test**: Run the focused client configuration tests and inspect
their generated artifacts for RelayDeck identifiers only.

**Acceptance Scenarios**:

1. **Given** a developer generates a client configuration, **When** they inspect
   the result, **Then** its provider and environment identifiers use RelayDeck.
2. **Given** a frontend session, **When** it stores application-scoped state,
   **Then** its storage and coordination keys use RelayDeck prefixes.

### Edge Cases

- A former-name occurrence is an owned product protocol or default: replace it
  deliberately, accepting the documented breaking change.
- A legacy occurrence in legal or historical text: rewrite the product
  reference while retaining a legally stated owner or author unless a new
  legal subject is explicitly supplied.
- A third-party branded URL or affiliate code: remove the branded path or
  referral parameter, or rewrite the surrounding text to a neutral
  description; do not invent a third-party replacement.
- An owned old domain has no user-provided RelayDeck domain: remove or point the
  source-code link to the canonical GitHub repository rather than inventing a
  domain.
- A target Docker image namespace has not been published: update the release
  configuration but do not claim registry availability.

## Requirements *(mandatory)*

### Functional Requirements

- **FR-001**: All in-scope executable, configuration, test, build, release, and
  deployment files MUST use `RelayDeck`, `relaydeck`, or `RELAYDECK` according
  to their naming context.
- **FR-002**: The in-scope file set MUST include version-controlled files with
  code or configuration extensions (`go`, `ts`, `tsx`, `js`, `jsx`, `vue`,
  `sh`, `sql`, `yaml`, `yml`, `json`, `toml`, `proto`, `service`, `html`,
  `css`) plus Dockerfiles, Makefiles, release manifests, and ignore files.
- **FR-003**: All Go self-imports and the module declaration MUST resolve to
  `github.com/JnyRoad/RelayDeck`.
- **FR-004**: Product-owned source repository links MUST resolve to
  `https://github.com/JnyRoad/RelayDeck`; no source code may retain the prior
  GitHub repository address.
- **FR-005**: Build, release, container, service, database, cache, browser
  storage, generated client, environment, and protocol identifiers that use the
  prior product name MUST use the corresponding RelayDeck identifier without
  fallback aliases.
- **FR-006**: Source files and directories whose names include the previous
  product name and are used for project tooling or deployment MUST be renamed
  and all references updated.
- **FR-007**: Old product-owned URLs without a supplied RelayDeck domain MUST be
  removed or replaced by the canonical repository URL; the implementation MUST
  NOT invent a RelayDeck DNS name.
- **FR-008**: The final validation MUST run a case-insensitive scan of the
  in-scope file set and report zero matches for the previous name.
- **FR-009**: Legal documents, third-party referral URLs, release history,
  archives, and OpenSpec historical evidence MUST contain no legacy product
  identity. Stated legal owners remain unchanged unless the user supplies a
  replacement; third-party branding and referral parameters are removed rather
  than invented.

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: The final in-scope legacy-name scan reports 0 matches and 0
  legacy-named tooling or deployment paths.
- **SC-002**: The backend unit suite and backend build complete successfully
  after the new module identity is applied.
- **SC-003**: The frontend typecheck and production build complete successfully
  after generated client and UI identifiers are renamed.
- **SC-004**: All configured source repository links point to the user-owned
  RelayDeck repository, with no production publish or deployment performed.

## Assumptions

- The confirmed canonical repository is `JnyRoad/RelayDeck`; its GitHub URL is
  the only new external destination supplied by the user.
- The intended Docker image namespace is `jnyroad/relaydeck`, but its registry
  availability is unverified and publication is out of scope.
- The requested breaking change intentionally invalidates existing previous-name
  service units, environment variables, browser keys, cache keys, default data
  names, generated clients, and protocol identifiers.
- The user explicitly authorized removal of the legacy identity from
  documentation, legal text, third-party links, and historical archives.
