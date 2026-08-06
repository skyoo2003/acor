# ACOR

**A**ho-**C**orasick automaton working **O**n **R**edis — distributed multi-pattern matching for Go.

[![Current Release](https://img.shields.io/github/release/skyoo2003/acor.svg)](https://github.com/skyoo2003/acor/releases/latest)
[![CI Status](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml/badge.svg)](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml)
[![Docs](https://img.shields.io/badge/docs-github_pages-1b6b57)](https://skyoo2003.github.io/acor/)
[![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/acor/pkg/acor.svg)](https://pkg.go.dev/github.com/skyoo2003/acor/pkg/acor)
[![License](https://img.shields.io/github/license/skyoo2003/acor.svg)](LICENSE)
[![Sponsor](https://img.shields.io/badge/sponsor-GitHub-pink)](https://github.com/sponsors/skyoo2003)

ACOR stores a shared Aho-Corasick pattern dictionary in Redis and exposes it
through a Go library and CLI. Multiple application instances can update the
same dictionary at runtime while preset engines serve matches from local memory.

Typical uses include content filtering, keyword extraction, intrusion detection,
search highlighting, and real-time text classification.

## Highlights

- **Shared state** — every application instance uses the same Redis-backed dictionary
- **Runtime updates** — Pub/Sub invalidation, with optional polling for missed messages
- **Fast reads** — preset engines match locally without Redis I/O on the hot path
- **Flexible deployment** — standalone Redis, Sentinel, Cluster, Ring, and Valkey
- **Complete matching API** — occurrences, positions, sets, streams, batches, and parallel matching

## Installation

ACOR requires Go 1.25 or newer and Redis 3.0 or newer, or Valkey 7.2 or newer.

```sh
go get github.com/skyoo2003/acor/pkg/acor@latest
```

## Quick Start

Start Redis locally, then create a matcher:

<!-- doccheck -->
```go
package main

import (
	"fmt"

	"github.com/skyoo2003/acor/pkg/acor"
)

func main() {
	ac, err := acor.Create(&acor.AhoCorasickArgs{
		Addr:   "localhost:6379",
		Name:   "sample",
		Preset: acor.PresetBalanced,
	})
	if err != nil {
		panic(err)
	}
	defer ac.Close()

	if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
		panic(err)
	}

	matches, err := ac.Find("he is him")
	if err != nil {
		panic(err)
	}
	fmt.Println(matches)
}
```

New collections use the optimized V2 Redis schema by default. `PresetBalanced`
is the recommended starting point when reads should run locally.

## Choosing a Preset

| Goal | Preset |
| ---- | ------ |
| General-purpose speed and memory | `PresetBalanced` |
| Highest matching throughput | `PresetSpeed` |
| Lowest memory usage | `PresetMemoryEfficient` |

Redis remains the source of truth for every preset. See the
[preset guide](docs/content/guides/preset-engine.md) for trade-offs and the
[Redis-backed engine guide](docs/content/guides/redis-backed-engine.md) for
multi-instance invalidation safety.

## Documentation

- [Getting started and Redis topologies](docs/content/getting-started/quick-start.md)
- [Matching and streaming API](docs/content/reference/api.md)
- [Compatibility](docs/content/reference/compatibility.md) — what the `v1` line promises
- [Batch operations](docs/content/guides/batch-operations.md) and [parallel matching](docs/content/guides/parallel-matching.md)
- [Schema V2 and migration](docs/content/reference/schema-v2.md) and [benchmarks](docs/content/reference/benchmarks.md)
- [Deployment](docs/content/operations/deployment.md), [monitoring](docs/content/operations/monitoring.md), and [troubleshooting](docs/content/operations/troubleshooting.md)
- [CLI installation](docs/content/getting-started/installation.md#cli-installation)

Browse the [full documentation](https://skyoo2003.github.io/acor/) or the
[Go API reference](https://pkg.go.dev/github.com/skyoo2003/acor/pkg/acor).

## Project

[Contributing](CONTRIBUTING.md) · [Support](SUPPORT.md) · [Security](SECURITY.md) ·
[Code of Conduct](CODE_OF_CONDUCT.md) · [Governance](GOVERNANCE.md) ·
[Changelog](CHANGELOG.md)

## License

[Apache License 2.0](LICENSE) — Copyright 2016-2026 Sungkyu Yoo
