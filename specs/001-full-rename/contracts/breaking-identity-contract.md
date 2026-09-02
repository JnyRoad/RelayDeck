# Breaking Identity Contract

## Canonical Names

| Surface | Canonical RelayDeck value |
|---|---|
| Source repository | `https://github.com/JnyRoad/RelayDeck` |
| Go module | `github.com/JnyRoad/RelayDeck` |
| Docker image target | `jnyroad/relaydeck` |
| Product display | `RelayDeck` |
| Lower-case identifier | `relaydeck` |
| Environment prefix | `RELAYDECK` |

## Intentionally Breaking Client Surfaces

Generated client configuration, provider names, environment-variable names,
browser-storage keys, cache keys, and product-specific protocol identifiers use
the canonical RelayDeck value. Previous values are not accepted or emitted.

## External Destination Rule

Product-owned repository links use the canonical repository URL. The user has
not supplied a RelayDeck domain, so source code must not create one. A former
owned domain link is removed or sent to the canonical repository instead.
