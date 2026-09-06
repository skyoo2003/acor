---
title: "Schema V1 (Deprecated)"
weight: 3
---

# Schema V1 (Deprecated)

> **V1 is deprecated and read-only as of `v1.5.0`.** Reads, `Suggest`, and `Info` work,
> and `MigrateV1ToV2` converts a collection in place, but `Add` and `Remove` return
> `ErrV1ReadOnly`. `Flush` still works and still deletes every key — read-only refuses
> keyword writes, it does not protect the collection. V1 gains no features: preset engines
> and `EnableCache` both require V2. The read path stays for the whole `v1` line and is
> removed no earlier than `v2`.

V1 spreads one collection across many keys — roughly 5 per 100 keywords.

| Key pattern | Purpose |
|-------------|---------|
| `{name}:keyword` | Set of keywords |
| `{name}:prefix` | Trie prefix edges |
| `{name}:suffix` | Trie suffix links |
| `{name}:output:{state}` | Output keywords per state |
| `{name}:node:{keyword}` | Node metadata |

`Find()` costs O(N×3-5) round trips for N visited states. `Add()` cost O(M×3-10) for a
keyword of length M — no longer reachable, and recorded only to explain what migration is
worth. Compare against [Schema V2](../schema-v2/#against-v1).

## Migrating

```bash
acor -name mycollection migrate --dry-run   # preview
acor -name mycollection migrate             # execute
acor -name mycollection migrate-rollback    # back to V1
```
