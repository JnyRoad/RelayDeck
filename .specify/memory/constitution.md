<!--
Sync Impact Report
- Version change: template → 1.0.0
- Modified principles: none (initial adoption)
- Added sections: Product Identity, Delivery Workflow
- Removed sections: none
- Follow-up TODOs: none
-->
# RelayDeck Constitution

## Core Principles

### I. Canonical Product Identity

All newly created or modified runtime, build, test, release, and deployment
artifacts MUST use RelayDeck as the product identity. Its case-normalized forms
are `RelayDeck`, `relaydeck`, and `RELAYDECK`; they are selected by the naming
context, not mixed arbitrarily.

### II. Intentional Breaking Rename

Approved full renames MUST remove the previous product identity from every
in-scope executable or configuration artifact. Compatibility aliases, fallback
names, and automatic migrations are forbidden unless the user explicitly
authorizes an exception. This makes each compatibility break visible and
auditable.

### III. Preserve Functional Interfaces Unless Named

Branding changes MUST preserve public API behavior, request semantics, and data
models unless the user explicitly includes those interfaces in the breaking
rename. A string that is an external protocol value or third-party identifier
MUST be classified before it is changed.

### IV. Evidence-Driven Delivery

Every behavior-affecting change MUST be verified by a focused failing test when
one can express the intended behavior, followed by the smallest code change
that passes it. The final rename MUST also pass an in-scope legacy-token scan,
the backend test/build gate, and the frontend typecheck/build gate.

### V. Controlled Publication

Remote repository, image registry, and deployment changes MUST target the
user-designated RelayDeck destinations. Creating or reconfiguring a remote does
not authorize pushing, publishing images, deploying, or deleting the old
resources.

## Product Identity

The canonical source repository is `github.com/JnyRoad/RelayDeck`; the original
`github.com/Wei-Shaw/sub2api` repository is read-only upstream. Runtime names,
environment variables, cache prefixes, default databases, service units,
container names, release metadata, generated client setup, and package/module
identifiers are part of the product identity. Legal texts, third-party links,
and archived snapshots are outside a code-only rename unless separately
authorized.

## Delivery Workflow

Before bulk modification, inventory all case variants and classify each match as
an owned product identifier, an external protocol/third-party identifier, or
out-of-scope historical/legal content. Update every owned identifier in scope
atomically, including related filenames. Re-run the inventory with the same
scope after modification, then run formatters and the repository's relevant
backend and frontend validation commands. Report unverified external registry
or domain availability explicitly.

## Governance

This constitution governs implementation artifacts for RelayDeck. Amendments
require a documented rationale, an updated sync report, and a semantic version
bump: MAJOR for incompatible principle changes, MINOR for new or expanded
principles, and PATCH for clarifications. Every implementation review MUST
verify the applicable principles and retain fresh command evidence for claimed
validation.

**Version**: 1.0.0 | **Ratified**: 2026-09-01 | **Last Amended**: 2026-09-01
