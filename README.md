# ACOR

**A**ho-**C**orasick automaton working **O**n **R**edis — A Go library for efficient multi-pattern string matching backed by Redis.

[![Current Release](https://img.shields.io/github/release/skyoo2003/acor.svg)](https://github.com/skyoo2003/acor/releases/latest)
[![CI Status](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml/badge.svg)](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml)
[![Docs](https://img.shields.io/badge/docs-github_pages-1b6b57)](https://skyoo2003.github.io/acor/)
[![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/acor/pkg/acor.svg)](https://pkg.go.dev/github.com/skyoo2003/acor/pkg/acor)
[![License](https://img.shields.io/github/license/skyoo2003/acor.svg)](LICENSE)
[![Sponsor](https://img.shields.io/badge/sponsor-GitHub-pink)](https://github.com/sponsors/skyoo2003)

## Should you use ACOR?

**Probably not, if you run a single process with a static dictionary.** An
in-memory library like [`cloudflare/ahocorasick`](https://github.com/cloudflare/ahocorasick)
or [`petar-dambovaliev/aho-corasick`](https://github.com/petar-dambovaliev/aho-corasick)
is lighter and needs no infrastructure.

**Use ACOR when several instances share one dictionary that changes at runtime.**
On an Apple M4 with Redis 8 on loopback, one sample measured a keyword added on
one instance reaching the others at a p50 of 211 µs (p99 2.3 ms). An in-memory
automaton has no equivalent number, because its answer is a redeploy.

Matching runs in-process once the automaton is warm, with no Redis round trip.
Over 1,000 keywords and a 640 B text on an Apple M4, `PresetSpeed` measures:

| Operation | ns/op | B/op | allocs/op |
| --------- | ----- | ---- | --------- |
| `FindSet` — which patterns appear | 791 | 152 | 3 |
| `Find` — every occurrence | 838 | 2,048 | 4 |
| `FindMatches` — occurrences with positions | 1,678 | 4,264 | 6 |

`PresetBalanced` holds that dictionary in 314 KiB of retained heap. Absolute times
are hardware-bound and move 20-25% between runs on the same machine, so read them
as an order of magnitude; the
[benchmarks page](https://skyoo2003.github.io/acor/reference/benchmarks/) has the
method and the full tables.

## Overview

ACOR implements the [Aho-Corasick algorithm](https://en.wikipedia.org/wiki/Aho%E2%80%93Corasick_algorithm) for efficient multi-pattern string matching, with all data structures persisted in Redis. This enables:

- **Distributed state** — one pattern dictionary shared across every application instance
- **Runtime updates** — change the dictionary without a redeploy; other instances pick it up via Pub/Sub
- **Persistence** — dictionaries survive application restarts without being rebuilt
- **Scalability** — Redis Sentinel, Cluster, and Ring topologies

## Use Cases

- Content filtering and profanity detection
- Log analysis and keyword extraction
- Intrusion detection systems
- Search term highlighting
- Real-time text classification

## Prerequisites

- Go >= 1.25
- Redis >= 3.0 or Valkey >= 7.2

ACOR talks the standard RESP protocol via [go-redis v9](https://github.com/redis/go-redis), so it works with any Redis- or Valkey-compatible server. RESP3 is negotiated automatically and falls back to RESP2 on older servers. Both Redis and Valkey are validated in CI.

## Installation

```sh
go get github.com/skyoo2003/acor/pkg/acor@v0.10.0
```

## Quick Start

<!-- doccheck -->
```go
package main

import (
 "fmt"
 "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
 args := &acor.AhoCorasickArgs{
  Addr:     "localhost:6379",
  Password: "",
  DB:       0,
  Name:     "sample",
 }
 ac, err := acor.Create(args)
 if err != nil {
  panic(err)
 }
 defer ac.Close()

 keywords := []string{"he", "her", "him"}
 for _, k := range keywords {
  if _, err := ac.Add(k); err != nil {
   panic(err)
  }
 }

 matched, err := ac.Find("he is him")
 if err != nil {
  panic(err)
 }
 fmt.Println(matched)

 if err := ac.Flush(); err != nil {
  panic(err)
 }
}
```

## Match Details and Streaming

`Find` reports every occurrence. Use `FindSet` to get each matched keyword once,
`FindMatches` for ordered rune spans (`FindMatchesAppend` to reuse a buffer),
`Contains` for an early-exit presence check, and `FindStream` for an `io.Reader`.
See the
[matching API reference](docs/content/reference/api.md#findmatches) for options
and a compiled example.

## Redis Topologies

ACOR supports standalone Redis, Sentinel, Cluster, and Ring configurations:

```go
// Sentinel
sentinelArgs := &acor.AhoCorasickArgs{
 Addrs:      []string{"localhost:26379", "localhost:26380"},
 MasterName: "mymaster",
 Password:   "",
 DB:         0,
 Name:       "sample",
}

// Cluster
clusterArgs := &acor.AhoCorasickArgs{
 Addrs:    []string{"localhost:7000", "localhost:7001", "localhost:7002"},
 Password: "",
 Name:     "sample",
}

// Ring
ringArgs := &acor.AhoCorasickArgs{
 RingAddrs: map[string]string{
  "shard-1": "localhost:7000",
  "shard-2": "localhost:7001",
 },
 Password: "",
 DB:       0,
 Name:     "sample",
}
```

## Schema Versions

ACOR supports two Redis schema versions:

| Version        | Description                  | Keys per 100K keywords |
| -------------- | ---------------------------- | ---------------------- |
| V1 (Legacy)    | Multiple keys per collection | ~500K                  |
| V2 (Optimized) | Fixed 3 keys per collection  | 3                      |

**V2 is recommended** for new collections: `Add()` is ~14x faster, and V2 is the
only schema that supports local caching or preset engines.

### Performance Comparison

Round trips are exact and enforced by tests. Timings are from Apple M4 with Redis
8 on loopback at 1000 keywords — reproduce them and see the full tables on the
[benchmarks page](https://skyoo2003.github.io/acor/reference/benchmarks/).

| Operation | V1 (Legacy) | V2 (Optimized) |
| --------- | ----------- | -------------- |
| Find() round trips | 1 | 1 |
| Add() round trips | grows with keyword length (53 at 5 chars, 507 at 26) | 2 |
| Add() time | baseline | **~14x faster** |
| Find() time, no cache | baseline | ~1.7x **slower** |
| Find() time, `EnableCache` | n/a | ~15x faster |
| Find() time, `PresetBalanced` | n/a | ~59x faster |

Uncached V2 `Find()` reads an outputs hash with one entry per trie state, where
V1 reads only the keyword set, so it stays slightly slower on reads at one round
trip each. The large read speedups come from `EnableCache` or a `Preset`, not
from the schema alone. For read-heavy workloads, enable one of them.

### Migration

```sh
# Preview migration
acor -name mycollection migrate --dry-run

# Execute migration
acor -name mycollection migrate

# Rollback to V1
acor -name mycollection migrate-rollback

# Check schema version
acor -name mycollection schema-version
```

## Batch Operations

ACOR supports batch operations for better performance when working with multiple keywords:

```go
// Add multiple keywords at once
result, err := ac.AddMany([]string{"he", "her", "him", "his"}, &acor.BatchOptions{
    Mode: acor.BatchModeTransactional, // or acor.BatchModeBestEffort
})

// Remove multiple keywords
result, err = ac.RemoveMany([]string{"he", "her"})

// Find matches in multiple texts
matches, err := ac.FindMany([]string{"he is him", "this is hers"})
```

`AddMany`/`RemoveMany` plan the whole batch in one pass and commit it in a single
transaction, so a batch costs two round trips regardless of size. In the Apple
M4/Redis 8 loopback sample, 1,000 keywords loaded in ~3 ms. Prefer them over a
loop of `Add`.

**Batch Modes:**

- `BatchModeBestEffort`: Continues on errors, returns partial results
- `BatchModeTransactional`: All-or-nothing

On V2 and preset collections both modes commit as one compare-and-set write, so a
failure leaves nothing written and the modes differ only in how they report it. V1
still writes per keyword and rolls back on failure.

## Parallel Matching

For large texts, use parallel matching to leverage multiple goroutines:

```go
matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
    Workers:  4,
    Boundary: acor.ChunkBoundaryWord, // ChunkBoundaryWord, ChunkBoundaryLine, or ChunkBoundarySentence
})
```

Chunk boundaries ensure matches aren't split across chunks:

- `ChunkBoundaryWord`: Split at word boundaries (default)
- `ChunkBoundaryLine`: Split at line breaks
- `ChunkBoundarySentence`: Split at sentence endings

## Redis-Backed Engine with Presets

For distributed deployments that need both Redis persistence and local speed, use the `Preset` field:

```go
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

ac.Add("hello")
matches, _ := ac.Find("hello world") // 0 RTT on hot path
```

Redis is the source of truth; a local preset-optimized automaton handles reads with no Redis I/O on the hot path. Cross-instance invalidation uses Redis Pub/Sub.

## Architecture Presets

| Preset | Engine | Best For | Trade-off |
|--------|--------|----------|-----------|
| `PresetSpeed` | Full DFA + flat array | Highest throughput; real-time inspection, latency-critical paths | Higher memory (states x alphabet) |
| `PresetBalanced` | Double-Array Trie + Banded DFA | General-purpose keyword filtering | Balanced speed and memory |
| `PresetMemoryEfficient` | Map-based + Bloom filter | Large-scale domain blocking, millions of patterns | Slower search |

## Local Caching

For read-heavy workloads with the original `AhoCorasick`, enable local caching to eliminate Redis round-trips:

```go
ac, _ := acor.Create(&acor.AhoCorasickArgs{
    Addr:        "localhost:6379",
    Name:        "my-collection",
    EnableCache: true,
})

// First Find() loads from Redis (1 RTT)
ac.Find("hello world")

// Subsequent Find() uses local cache (0 RTT)
ac.Find("another text")
```

**Cache Behavior:**
- Cache is invalidated via Redis Pub/Sub when any instance modifies the collection
- First Find() after invalidation reloads from Redis
- Works with Standalone, Sentinel, Cluster, and Ring topologies

## Observability

The server and observability packages live in a separate module, so the core
library stays dependency-light. Install it with:

```sh
go get github.com/skyoo2003/acor/server
```

```go
import (
    "github.com/skyoo2003/acor/pkg/acor"
    "github.com/skyoo2003/acor/server/metrics"
    "github.com/skyoo2003/acor/server/logging"
    "github.com/skyoo2003/acor/server/tracing"
    "github.com/skyoo2003/acor/server/health"
)
```

- **Metrics**: Prometheus metrics for HTTP, gRPC, and Redis operations
- **Logging**: Structured JSON logging with zerolog
- **Tracing**: OpenTelemetry distributed tracing
- **Health**: Kubernetes-compatible `/healthz` and `/readyz` endpoints

## CLI

ACOR includes a command-line interface for common operations:

```sh
# Install
go install github.com/skyoo2003/acor/cmd/acor@v0.10.0

# Add keywords
acor -name mycollection add "keyword1" "keyword2"

# Find matches
acor -name mycollection find "sample text"

# Find matches with their positions
acor -name mycollection find-index "sample text"

# Suggest keywords by prefix
acor -name mycollection suggest "sam"

# Show collection info
acor -name mycollection info

# Migrate / roll back / check schema version
acor -name mycollection migrate --dry-run
acor -name mycollection schema-version
```

Run `acor` with no arguments to see all commands (also: `remove`, `suggest-index`, `flush`, `migrate-rollback`).

## Documentation

Full documentation is available at [GitHub Pages](https://skyoo2003.github.io/acor/).

API reference: [pkg.go.dev](https://pkg.go.dev/github.com/skyoo2003/acor/pkg/acor)

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Support

See [SUPPORT.md](SUPPORT.md) for help channels and response times.

## Security

Please see our [Security Policy](SECURITY.md) for vulnerability reporting.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).

## Governance

See [GOVERNANCE.md](GOVERNANCE.md) for project decision-making and contribution model.

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for version history.

## License

[Apache License 2.0](LICENSE) - Copyright 2016-2026 Sungkyu Yoo
