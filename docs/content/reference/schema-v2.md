---
title: "Schema V2 (Optimized)"
weight: 4
---

# Schema V2 (Optimized)

V2 is the default for new collections — no configuration needed. A collection occupies at
most 3 keys whatever the dictionary size.

| Key | Holds | When it exists |
|-----|-------|----------------|
| `{name}:trie` | Serialized trie: keywords, prefixes, version | Always, from creation |
| `{name}:outputs` | Output mappings, state → keywords | Once the collection has a keyword |
| `{name}:nodes` | Node metadata | Only after `MigrateV1ToV2`; cleaned up by flush |

Most collections hold two keys; a fresh one holds a single `:trie`. Nothing but migration
writes `:nodes`. Budget for three, expect to count fewer.

## Field layout

```text
{name}:trie      (hash)
  keywords -> ["keyword1", "keyword2", ...]
  prefixes -> ["", "h", "he", ...]
  version  -> <int64 optimistic lock>

{name}:outputs   (hash)
  he  -> ["he"]
  she -> ["he", "she"]

{name}:nodes     (hash, migration only)
  keyword1 -> ["s0","s1","s2"]
```

Collections written before v0.11 also carry a `suffixes` field on `:trie`. It is never
read, writes leave it alone, and the next `Flush()` drops it.

## Against V1

Round trips are counted by tests on every CI run; timings are a sample from the hardware
named on the [benchmarks page](../benchmarks/).

| Metric | V1 | V2 |
|--------|----|----|
| Keys per 100K keywords | ~500K | 2 |
| `Find()` round trips | 1 | 1 |
| `Add()` round trips | Grows with keyword length: 53 at 5 chars, 507 at 26 | 2 |
| `Add()` time | baseline | ~14x faster |
| `Find()` time, no cache | baseline | ~1.7x **slower** at 1000 keywords |

**V2's win is writes, not reads.** Both schemas read in one round trip, and uncached V2 is
slightly slower on `Find()` because it reads an outputs hash with one entry per trie state
where V1 reads only the keyword set. The large read speedups belong to `EnableCache` and
the preset engines, not to the schema.

## Migrating from V1

```bash
acor -name mycollection schema-version   # what you have now
acor -name mycollection migrate --dry-run
acor -name mycollection migrate
```
