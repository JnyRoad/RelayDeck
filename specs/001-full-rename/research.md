# Research: Full RelayDeck Rename

## Canonical Identity Mapping

**Decision**: Use `RelayDeck` for display names, `relaydeck` for runtime and
identifier names, and `RELAYDECK` for environment-variable prefixes.

**Rationale**: This preserves the capitalization conventions already used by
the project while removing every case form of the former identity.

**Alternatives considered**: Retaining mixed spelling such as `Relaydeck` was
rejected because it would create a new inconsistent identifier family.

## Repository and Image Destinations

**Decision**: Rewrite owned source URLs and Go self-imports to
`github.com/JnyRoad/RelayDeck`; target `jnyroad/relaydeck` in release and
container metadata.

**Rationale**: The user created and configured the canonical GitHub repository.
The Docker namespace is a source configuration target only; it is not assumed
to exist or be publishable.

**Alternatives considered**: Keeping the original GitHub/Docker values as
compatibility targets was rejected by the requested destructive rename.

## Unprovided DNS Destinations

**Decision**: Remove owned former-name domain links or replace them with the
canonical GitHub repository URL. Do not construct a `relaydeck.*` DNS name.

**Rationale**: The user supplied a repository, not a domain. Guessing a domain
could direct users to an unrelated service.

**Alternatives considered**: Replacing the former domain mechanically with
`relaydeck.io` was rejected as unsafe and unverified.

## Breaking Runtime Identifiers

**Decision**: Rename service units, binaries, container resources, default
database names, Redis/local-storage prefixes, client provider labels,
environment variables, and product protocol identifiers with no fallback.

**Rationale**: The user explicitly selected a complete breaking rename.

**Alternatives considered**: Aliases and dual-read/dual-write migration code
were rejected because they would violate the requested no-compatibility scope.

## Validation Strategy

**Decision**: Change an existing configuration expectation first to establish a
focused failing test, then apply the owned-identifier replacement. Validate by
case-insensitive source scan, Go tests/build, frontend typecheck/build, and
focused generated-client tests.

**Rationale**: The repository already has tests for default configuration and
generated client configuration. A scan catches the mechanical completeness
property that unit tests cannot express alone.

**Alternatives considered**: A text-only replacement with no test/build gates
was rejected because the Go module and generated client setup have compile-time
contracts.
