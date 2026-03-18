# ACOR

**A**ho-**C**orasick automation working **O**n **R**edis — A Go library for efficient multi-pattern string matching backed by Redis.

[![Current Release](https://img.shields.io/github/release/skyoo2003/acor.svg)](https://github.com/skyoo2003/acor/releases/latest)
[![CI Status](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml/badge.svg)](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml)
[![Docs](https://img.shields.io/badge/docs-github_pages-1b6b57)](https://skyoo2003.github.io/acor/)
[![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/acor.svg)](https://pkg.go.dev/github.com/skyoo2003/acor)
[![Go Report Card](https://goreportcard.com/badge/github.com/skyoo2003/acor)](https://goreportcard.com/report/github.com/skyoo2003/acor)
[![License](https://img.shields.io/github/license/skyoo2003/acor.svg)](LICENSE)

## Overview

ACOR implements the [Aho-Corasick algorithm](https://en.wikipedia.org/wiki/Aho%E2%80%93Corasick_algorithm) for efficient multi-pattern string matching, with all data structures persisted in Redis. This enables:

- **Fast pattern matching** — O(n + m) complexity where n is text length and m is number of matches
- **Distributed state** — Share pattern dictionaries across multiple application instances
- **Persistence** — Pattern dictionaries survive application restarts
- **Scalability** — Support for Redis Sentinel, Cluster, and Ring topologies

## Use Cases

- Content filtering and profanity detection
- Log analysis and keyword extraction
- Intrusion detection systems
- Search term highlighting
- Real-time text classification

## Prerequisites

- Go >= 1.24
- Redis >= 3.0

## Installation

```sh
go get -u github.com/skyoo2003/acor
```

## Quick Start

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

**V2 is recommended** for new collections and provides 50-60x faster `Find()` operations.

### Performance Comparison

| Operation | V1 (Legacy)   | V2 (Optimized) |
| --------- | ------------- | -------------- |
| Find()    | O(N×3-5) RTT  | 3 RTT (fixed)  |
| Add()     | O(M×3-10) RTT | 2-3 RTT        |

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
    Mode: acor.Transactional, // or acor.BestEffort
})

// Remove multiple keywords
result, err := ac.RemoveMany([]string{"he", "her"})

// Find matches in multiple texts
matches, err := ac.FindMany([]string{"he is him", "this is hers"})
```

**Batch Modes:**
- `BestEffort`: Continues on errors, returns partial results
- `Transactional`: Rolls back all changes if any error occurs

## Parallel Matching

For large texts, use parallel matching to leverage multiple goroutines:

```go
matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
    Workers:       4,
    ChunkBoundary: acor.ChunkWord, // ChunkWord, ChunkLine, or ChunkSentence
})
```

Chunk boundaries ensure matches aren't split across chunks:
- `ChunkWord`: Split at word boundaries (default)
- `ChunkLine`: Split at line breaks
- `ChunkSentence`: Split at sentence endings

## Observability

ACOR provides built-in observability support:

```go
import (
    "github.com/skyoo2003/acor/pkg/acor"
    "github.com/skyoo2003/acor/pkg/metrics"
    "github.com/skyoo2003/acor/pkg/logging"
    "github.com/skyoo2003/acor/pkg/tracing"
    "github.com/skyoo2003/acor/pkg/health"
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
go install github.com/skyoo2003/acor/cmd/acor@latest

# Add keywords
acor -name mycollection add "keyword1" "keyword2"

# Find matches
acor -name mycollection find "sample text"

# List keywords
acor -name mycollection list
```

## Documentation

Full documentation is available at [GitHub Pages](https://skyoo2003.github.io/acor/).

API reference: [pkg.go.dev](https://pkg.go.dev/github.com/skyoo2003/acor)

## Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).

## Changelog

See [CHANGELOG.md](CHANGELOG.md) for version history.

## License

[MIT License](LICENSE) - Copyright (c) 2016-2026 Sungkyu Yoo
