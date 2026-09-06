---
title: "Preset-Optimized Engine"
weight: 3
---

# Preset-Optimized Engine

Setting `Preset` on `AhoCorasickArgs` builds a local automaton next to the Redis
collection: writes still go to Redis atomically, reads never touch it. This page is about
picking a preset. How the engine stays in sync across instances is
[Redis-Backed Engine](../redis-backed-engine/).

<!-- doccheck -->
```go
ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr:          "localhost:6379",
    Name:          "my-collection",
    Preset:        acor.PresetBalanced,
    CaseSensitive: false, // default; matching is case-insensitive
})
if err != nil {
    panic(err)
}
defer ac.Close()
```

The preset is fixed at creation. `Info()` reports the resulting `Keywords`, `Nodes`,
`MemoryBytes`, and `TrieDepth`.

## The three presets

| Preset | Engine | Best for | Costs |
|--------|--------|----------|-------|
| `PresetSpeed` | Full DFA + flat array trie + compact alphabet map | Packet inspection, high-rate log scanning, latency-critical paths | Memory proportional to states × alphabet size |
| `PresetBalanced` | Double-Array Trie + Banded DFA + output link compression | General backend filtering, search | Neither extreme |
| `PresetMemoryEfficient` | Map-based sparse trie + Bloom pre-filter + NFA | Millions of patterns under a memory cap | Slower search: failure-link traversal and map lookups |

**Start with `PresetBalanced`.** Move to `PresetSpeed` when latency is the constraint and
memory is not; to `PresetMemoryEfficient` when it is the other way round.

`PresetSpeed` measured fastest on every query shape on the
[benchmarks page](../../reference/benchmarks/), and `PresetBalanced` gives up some of that
for a much smaller transition table. Measure your own corpus before choosing by name.

## Refresh behavior

Stale reads share one reload and each observes its own cancellation. A failed reload
returns an error to the search rather than silently serving the old engine, and a later
search retries. Optional version polling recovers from missed Pub/Sub messages; its
interval is not a freshness bound. Full lifecycle and failure counters:
[invalidation safety](../redis-backed-engine/#invalidation-safety).
