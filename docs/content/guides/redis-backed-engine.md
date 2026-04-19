---
title: "Redis-Backed Engine"
weight: 4
---

# Redis-Backed Engine

The preset-optimized Redis mode (enabled via `Preset` on `AhoCorasickArgs`) combines Redis persistence with a local preset-optimized automaton. Redis is the source of truth; reads hit the local engine with no Redis I/O on the hot path.

## When to Use

- Distributed deployments across multiple instances
- Need for Redis persistence and cross-instance synchronization
- Want preset-optimized local speed without giving up Redis durability
- Migrating from the original `AhoCorasick` for better read performance

## Architecture

```
                    Write Path
Instance A ──Add()──▶ Lua Script (optimistic lock) ──▶ Redis
                                                  │
                       Pub/Sub invalidate ◀────────┘
                            │
Instance B ◀────────────────┘
      │
      └─ ensureValid() ──▶ reload from Redis ──▶ rebuild local engine

                    Read Path
Instance A ──Find()──▶ local engine (0 RTT)
```

- **Writes**: V2 Lua scripts with optimistic locking (up to 3 retries with backoff)
- **Reads**: Local preset-optimized automaton — no Redis I/O
- **Invalidation**: Redis Pub/Sub notifies all instances on mutation
- **Degraded mode**: If reload fails, the last-good engine continues serving reads

## Quick Start

```go
package main

import (
    "fmt"
    "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
    ac, err := acor.Create(&acor.AhoCorasickArgs{
        Addr:          "localhost:6379",
        Name:          "my-collection",
        Preset:        acor.PresetBalanced,
        CaseSensitive: false,
    })
    if err != nil {
        panic(err)
    }
    defer ac.Close()

    added, err := ac.Add("hello")
    if err != nil {
        panic(err)
    }
    fmt.Printf("Added: %d\n", added)

    matches, err := ac.Find("hello world")
    if err != nil {
        panic(err)
    }
    fmt.Println(matches) // [hello]
}
```

## AhoCorasickArgs (Preset field)

The `AhoCorasickArgs` struct includes a `Preset` field for selecting the local engine architecture:

```go
type AhoCorasickArgs struct {
    // ... Addr, Addrs, RingAddrs, Password, DB, Name ...
    Preset         Preset       // Architecture preset (default: PresetBalanced)
    CaseSensitive   bool         // Enable case-sensitive matching (default: false)
    // ... other fields ...
}
```

All standard Redis topologies are supported (Standalone, Sentinel, Cluster, Ring) via the connection fields on `AhoCorasickArgs`.

## Preset Selection

The same [architecture presets](preset-engine/#architecture-presets) are available:

| Preset | Use Case |
|--------|----------|
| `PresetSpeed` | Latency-critical, memory available |
| `PresetBalanced` | Default — best speed-to-memory ratio |
| `PresetMemoryEfficient` | Millions of patterns, memory constrained |
| `PresetUltimate` | Maximum throughput production systems |

## API Reference

```go
// Create
ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr:   "localhost:6379",
    Name:   "my-collection",
    Preset: acor.PresetBalanced,
})

// Add/Remove
added, err := ac.Add("keyword")      // (int, error)
removed, err := ac.Remove("keyword") // (int, error)

// Find (0 RTT on hot path — reads from local engine)
matches, err := ac.Find("text")        // ([]string, error)
positions, err := ac.FindIndex("text") // (map[string][]int, error)

// Stats
info, err := ac.Info()   // (*AhoCorasickInfo, error)

// Flush and Close
err := ac.Flush()
err := ac.Close()
```

## Comparison with AhoCorasick

| Feature | `AhoCorasick` (no Preset) | `AhoCorasick` (with Preset) |
|---------|--------------|-----------------|
| Read latency | 3 RTT (V2) or cached | 0 RTT (local engine) |
| Write latency | Lua script | Lua script + optimistic lock |
| Cross-instance sync | Pub/Sub cache invalidation | Pub/Sub engine rebuild |
| Schema | V1 or V2 | V2 only |
| Presets | N/A | Speed, Balanced, MemoryEfficient, Ultimate |
| Suggest/SuggestIndex | Yes | No |
| Batch operations | Yes | No |
| Parallel matching | Yes | No |

Use a `Preset`-optimized `AhoCorasick` when you need the fastest possible reads in a distributed setup and can accept the V2-only constraint.

## Next Steps

- [Preset-Optimized Engine](preset-engine/) - Redis-backed engine with local speed
- [API Reference](../reference/api/) - Complete API documentation
