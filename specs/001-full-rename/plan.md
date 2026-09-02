# Implementation Plan: Full RelayDeck Rename

**Branch**: `001-full-rename` | **Date**: 2026-09-01 | **Spec**:
[spec.md](spec.md)

## Summary

Replace every owned `Sub2API` identity in the code-and-configuration scope with
RelayDeck, including the Go module path, generated client configuration,
runtime names, build/release metadata, deployment resources, and code-bearing
filenames. This is intentionally breaking: previous protocol and persistence
identifiers receive no alias or migration. Historical, legal, and third-party
content remains outside the scope.

## Technical Context

**Language/Version**: Go 1.27, TypeScript 5.6, Vue 3.4, shell, YAML, SQL

**Primary Dependencies**: Gin, Ent, Vue, Vite, Vitest, pnpm, Docker, systemd

**Storage**: PostgreSQL, Redis, browser storage, filesystem data directories

**Testing**: Go test suite, focused Vitest suite, Vue typecheck, frontend build,
backend build, source scan

**Target Platform**: Linux systemd, Docker Compose, Apple container, browsers

**Project Type**: Web API gateway with a Vue administration frontend

**Performance Goals**: Preserve existing build and runtime behavior; no new
runtime work is introduced by the rename.

**Constraints**: No aliases or automatic migration; no push, registry publish,
deployment, or invented DNS destination; legal, archived, and third-party
content stays untouched.

**Scale/Scope**: Approximately 1,700 source/configuration files and 4,499
case-varied occurrences identified before implementation.

## Constitution Check

| Principle | Gate | Status |
|---|---|---|
| Canonical Product Identity | Map every case variant consistently | Pass — mapping is defined in research.md |
| Intentional Breaking Rename | Do not retain aliases or migrations | Pass — explicitly required by the user |
| Preserve Functional Interfaces Unless Named | Classify third-party values first | Pass — user-owned identifiers change; third-party/history is excluded |
| Evidence-Driven Delivery | Use a red-green focused test and final build/scan gates | Pass — tasks put test before source edit |
| Controlled Publication | Do not publish or deploy | Pass — only local source/config changes are planned |

## Project Structure

### Documentation (this feature)

```text
specs/001-full-rename/
├── spec.md
├── plan.md
├── research.md
├── data-model.md
├── contracts/
│   └── breaking-identity-contract.md
├── quickstart.md
└── tasks.md
```

### Source Code (repository root)

```text
backend/                 # Go module, handlers, configuration, tests, migrations
frontend/                # Vue UI, generated client setup, i18n, tests, package metadata
deploy/                  # Service units, container definitions, installer scripts
.github/workflows/       # CI and release metadata
skills/relaydeck-admin/  # Project administration tooling after path rename
Dockerfile*              # Container artifact and runtime identity
Makefile                 # Root validation entry points
```

**Structure Decision**: The existing backend/frontend/deployment layout remains
unchanged. This change only replaces owned identities and renames the few
identity-bearing paths.

## Complexity Tracking

No constitution violation requires an exception. The mechanical replacement is
performed after source classification, with targeted manual handling for
repository URLs, unprovided domains, Docker image names, and filesystem paths.
