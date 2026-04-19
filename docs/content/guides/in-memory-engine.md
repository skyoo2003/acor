---
title: "In-Memory Engine"
weight: 3
---

# In-Memory Engine

ACOR provides a pure in-memory Aho-Corasick engine with selectable architecture presets, created via the unified `Create` API with `InMemory: true`. No Redis or external dependencies are required.

## When to Use

- Unit tests and local tooling
- Single-instance deployments
- Applications where Redis is overkill
- Embedding keyword matching into a library or CLI tool

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
    ac, _ := acor.Create(&acor.AhoCorasickArgs{
        InMemory: true,
        Name:     "my-collection",
        Preset:   acor.PresetBalanced,
    })

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
    InMemory:     true,
    Name:         "my-collection",
    Preset:       acor.PresetBalanced,
    CaseSensitive: true,
})
```

## API Reference

```go
// Create
ac, err := acor.Create(&acor.AhoCorasickArgs{
    InMemory: true,
    Name:     "my-collection",
    Preset:   acor.PresetBalanced,
})

// Add/Remove — returns 1 if changed, 0 if no-op
ac.Add("keyword")
ac.Remove("keyword")

// Find
matches := ac.Find("text")          // []string
positions := ac.FindIndex("text")   // map[string][]int

// Stats
info, err := ac.Info()              // (*AhoCorasickInfo, error)

// Reset
ac.Flush()
```

## Next Steps

- [Redis-Backed Engine](redis-backed-engine/) - Add Redis persistence with local speed
- [API Reference](../reference/api/) - Complete API documentation
