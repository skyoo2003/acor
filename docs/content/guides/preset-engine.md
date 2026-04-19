---
title: "Preset-Optimized Engine"
weight: 3
---

# Preset-Optimized Engine

ACOR provides a Redis-backed Aho-Corasick engine with selectable architecture presets. Created via the unified `Create` API with a `Preset` field. Writes go to Redis atomically (V2 Lua scripts with optimistic locking); reads hit the local engine with no Redis I/O.

## When to Use

- Production deployments requiring Redis persistence
- Distributed systems with multiple instances sharing a keyword collection
- High-throughput text matching with zero read-latency on the hot path
- Applications needing both durability and speed

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
    ac, _ := acor.Create(&acor.AhoCorasickArgs{
        Addr:   "localhost:6379",
        Name:   "my-collection",
        Preset: acor.PresetBalanced,
    })
    defer ac.Close()

    ac.Add("he")
    ac.Add("her")
    ac.Add("him")

    matches, _ := ac.Find("he is him")
    fmt.Println(matches) // [he him]

    positions, _ := ac.FindIndex("he is him")
    fmt.Println(positions) // map[he:[0] him:[6]]

    info, _ := ac.Info()
    fmt.Printf("Keywords: %d, Nodes: %d, Memory: %d bytes\n",
        info.Keywords, info.Nodes, info.MemoryBytes)
}
```

## Architecture Presets

Each preset optimizes for a different trade-off between speed, memory, and feature set. The preset is fixed at creation time.

| Preset | Engine | Best For | Trade-off |
|--------|--------|----------|-----------|
| `PresetSpeed` | Full DFA + flat array trie + compact alphabet mapping | Real-time packet inspection, high-speed log scanning, latency-critical paths | Higher memory proportional to states x alphabet size |
| `PresetBalanced` | Double-Array Trie + Banded DFA + output link compression | General-purpose backend keyword filtering, search engines | Balanced speed and memory |
| `PresetMemoryEfficient` | Map-based sparse trie + Bloom filter pre-filtering + standard NFA | Large-scale domain blocking, malware signature matching, millions of patterns | Slower search due to failure link traversal and map lookups |
| `PresetUltimate` | SIMD-aware byte scanning pre-filter + Double-Array Trie + Banded DFA + deferred bit-set output collection | Production systems needing highest throughput | Reasonable memory with highest speed |

### Choosing a Preset

- **Start with `PresetBalanced`** — it provides the best speed-to-memory ratio for most workloads.
- Use `PresetSpeed` when latency is critical and memory is available.
- Use `PresetMemoryEfficient` when you have millions of patterns and memory is constrained.
- Use `PresetUltimate` for production systems that need maximum throughput.

## Case Sensitivity

By default, matching is case-insensitive. Enable case-sensitive matching when needed:

```go
ac, _ := acor.Create(&acor.AhoCorasickArgs{
    Addr:          "localhost:6379",
    Name:          "my-collection",
    Preset:        acor.PresetBalanced,
    CaseSensitive: true,
})
defer ac.Close()
```

## API Reference

```go
// Create
ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr:   "localhost:6379",
    Name:   "my-collection",
    Preset: acor.PresetBalanced,
})
defer ac.Close()

// Add/Remove — returns 1 if changed, 0 if no-op
ac.Add("keyword")
ac.Remove("keyword")

// Find (0 RTT on hot path — reads from local engine)
matches, _ := ac.Find("text")          // ([]string, error)
positions, _ := ac.FindIndex("text")   // (map[string][]int, error)

// Stats
info, err := ac.Info()              // (*AhoCorasickInfo, error)

// Reset
ac.Flush()
```

## Next Steps

- [Redis-Backed Engine](redis-backed-engine/) - Redis persistence details
- [API Reference](../reference/api/) - Complete API documentation
