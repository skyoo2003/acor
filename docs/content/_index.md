---
title: ACOR Documentation
hero_title: Aho-Corasick on Redis, with one API across multiple topologies.
hero_text: ACOR is a Go library and CLI for storing and querying Aho-Corasick patterns in Redis. It supports standalone Redis, Sentinel, Cluster, and Ring deployments through the same Create API.
---

## Getting Started

ACOR requires Go 1.24 or newer and Redis 3.0 or newer.

```sh
go get -u github.com/skyoo2003/acor
```

```go
package main

import (
  "fmt"

  "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
  ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr: "localhost:6379",
    Name: "sample",
  })
  if err != nil {
    panic(err)
  }
  defer ac.Close()

  _, _ = ac.Add("he")
  _, _ = ac.Add("her")
  matched, _ := ac.Find("he is her")
  fmt.Println(matched)
}
```

## Redis Topologies

- **Standalone**: use `Addr` for a single Redis server.
- **Sentinel**: use `Addrs` with `MasterName` for failover-aware access.
- **Cluster**: use `Addrs` with `DB` left at `0`.
- **Ring**: use `RingAddrs` with shard-to-address mappings.

```go
sentinelArgs := &acor.AhoCorasickArgs{
  Addrs:      []string{"localhost:26379", "localhost:26380"},
  MasterName: "mymaster",
  Name:       "sample",
}

clusterArgs := &acor.AhoCorasickArgs{
  Addrs: []string{"localhost:7000", "localhost:7001", "localhost:7002"},
  Name:  "sample",
}

ringArgs := &acor.AhoCorasickArgs{
  RingAddrs: map[string]string{
    "shard-1": "localhost:7000",
    "shard-2": "localhost:7001",
  },
  Name: "sample",
}
```

## CLI Commands

<ul>
  <li><code>add &lt;keyword&gt;</code> inserts a keyword into the collection.</li>
  <li><code>remove &lt;keyword&gt;</code> removes a keyword and prunes related trie state.</li>
  <li><code>find &lt;input&gt;</code> and <code>find-index &lt;input&gt;</code> return matches or match offsets.</li>
  <li><code>suggest &lt;input&gt;</code> and <code>suggest-index &lt;input&gt;</code> return prefix suggestions and their offsets.</li>
  <li><code>info</code> returns keyword and node counts.</li>
  <li><code>flush</code> clears stored trie state.</li>
</ul>

| Flag | Purpose |
| --- | --- |
| `-addr` | Standalone Redis server address |
| `-addrs` | Sentinel or Cluster seed addresses |
| `-master-name` | Redis Sentinel master name |
| `-ring-addrs` | Comma-separated `shard=addr` pairs |
| `-password` | Redis password |
| `-db` | Redis DB number |
| `-name` | Pattern collection name, default `default` |
| `-debug` | Enable debug logging |

## HTTP and gRPC Adapters

The HTTP handler exposes JSON endpoints on `/v1/*` plus `/healthz`.

- `POST /v1/add`
- `POST /v1/remove`
- `POST /v1/find`
- `POST /v1/find-index`
- `POST /v1/suggest`
- `POST /v1/suggest-index`
- `GET /v1/info`
- `POST /v1/flush`

The gRPC service name is `acor.server.v1.Acor` and mirrors the same operations.

## Batch Operations

For better performance when working with multiple keywords:

```go
// Add multiple keywords with transactional mode
result, _ := ac.AddMany([]string{"he", "her", "him"}, &acor.BatchOptions{
    Mode: acor.Transactional,
})

// Find matches in multiple texts
matches, _ := ac.FindMany([]string{"he is him", "this is hers"})
```

## Parallel Matching

Process large texts using multiple goroutines:

```go
matches, _ := ac.FindParallel(largeText, &acor.ParallelOptions{
    Workers:       4,
    ChunkBoundary: acor.ChunkWord,
})
```

## Observability

ACOR provides built-in observability packages:

- **Metrics**: Prometheus metrics for HTTP, gRPC, and Redis
- **Logging**: Structured JSON logging with zerolog
- **Tracing**: OpenTelemetry distributed tracing
- **Health**: Kubernetes-compatible health checks

```go
import (
    "github.com/skyoo2003/acor/pkg/metrics"
    "github.com/skyoo2003/acor/pkg/logging"
    "github.com/skyoo2003/acor/pkg/tracing"
    "github.com/skyoo2003/acor/pkg/health"
)
```

## Documentation Sections

- [Getting Started](/getting-started/) - Installation and quick start guide
- [Guides](/guides/) - Batch operations and parallel matching
- [Reference](/reference/) - API and schema documentation
- [Operations](/operations/) - Deployment and monitoring
- [Extending](/extending/) - Custom storage backends

## Development Workflow

Run local checks with:

```sh
make test
make build
```

CI runs on pushes and pull requests against `master`. Releases are tag-based, and the GitHub Pages site is built from the Hugo source in `docs/`.

Source code is MIT licensed. Contributions should include tests when behavior changes.
