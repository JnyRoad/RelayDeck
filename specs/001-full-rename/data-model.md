# Identity-Bearing Data Model

This feature changes no schema shape. It changes canonical values carried by
existing configuration, runtime state, and generated client artifacts.

| Entity | Previous value family | New value family | Lifecycle |
|---|---|---|---|
| Go module | `github.com/Wei-Shaw/sub2api` | `github.com/JnyRoad/RelayDeck` | Compile-time import resolution |
| Product display name | mixed display variants | `RelayDeck` | UI, logs, templates, release metadata |
| Runtime namespace | lower-case former product prefix | `relaydeck` | service, binary, containers, cache, storage |
| Environment namespace | upper-case former product prefix | `RELAYDECK` | generated client and deployment settings |
| Default persistence names | former product database/key prefixes | `relaydeck` equivalents | new-instance defaults only |
| Product protocol labels | former product labels and paths | `relaydeck` equivalents | intentionally breaking client contract |

No data migration, alias, fallback, or dual-read behavior is introduced.
