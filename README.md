# ACOR

**A**ho-**C**orasick automaton working **O**n **R**edis — distributed multi-pattern matching for Go.

[![Current Release](https://img.shields.io/github/release/skyoo2003/acor.svg)](https://github.com/skyoo2003/acor/releases/latest)
[![CI Status](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml/badge.svg)](https://github.com/skyoo2003/acor/actions/workflows/ci.yaml)
[![Docs](https://img.shields.io/badge/docs-github_pages-1b6b57)](https://skyoo2003.github.io/acor/)
[![Go Reference](https://pkg.go.dev/badge/github.com/skyoo2003/acor/pkg/acor.svg)](https://pkg.go.dev/github.com/skyoo2003/acor/pkg/acor)
[![License](https://img.shields.io/github/license/skyoo2003/acor.svg)](LICENSE)
[![Sponsor](https://img.shields.io/badge/sponsor-GitHub-pink)](https://github.com/sponsors/skyoo2003)

ACOR keeps one Aho-Corasick dictionary in Redis and reaches it from a Go library, a CLI,
and an experimental server module. Every application instance shares that dictionary,
updates it at runtime, and matches against a local copy with no Redis I/O on the hot path.

Typical uses: content filtering, keyword extraction, intrusion detection, search
highlighting, real-time text classification.

## Highlights

- **Shared state** — every instance reads the same Redis-backed dictionary
- **Runtime updates** — Pub/Sub invalidation, with optional polling for missed messages
- **Fast reads** — preset engines match locally, 0 round trips
- **Topologies** — standalone Redis, Sentinel, Cluster, Ring, and Valkey
- **Full matching API** — occurrences, positions, sets, streams, batches, parallel scans

## Install

Requires Go 1.25+ and Redis 3.0+ or Valkey 7.2+.

```sh
go get github.com/skyoo2003/acor/pkg/acor@latest
```

## Quick start

Start Redis locally, then:

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

New collections use the optimized V2 Redis schema.

## Choosing a preset

| Goal | Preset |
| ---- | ------ |
| General-purpose speed and memory | `PresetBalanced` |
| Highest matching throughput | `PresetSpeed` |
| Lowest memory usage | `PresetMemoryEfficient` |

Redis stays the source of truth for every preset. Trade-offs are in the
[preset guide](docs/content/guides/preset-engine.md); multi-instance invalidation is in the
[Redis-backed engine guide](docs/content/guides/redis-backed-engine.md).

## Large dictionaries (V3)

`OpenVersioned` opens a separate V3 collection with leased snapshots, expected-version
writes, and background engine replacement. V1/V2 APIs are unaffected. See the
[V3 guide](docs/content/reference/versioned.md), its
[performance report](docs/content/reference/versioned-performance.md), and
[bounded text processing](docs/content/reference/text-processing.md) for `Scan`,
`MaskText`, and `ReplaceText`.

## Documentation

The [documentation site](https://skyoo2003.github.io/acor/) covers Redis topologies, the
matching and streaming API, batch and parallel guides, the V2 schema and benchmarks,
deployment and troubleshooting, the `acor` CLI, the experimental server module, and what
the `v1` line promises. Package signatures are on
[pkg.go.dev](https://pkg.go.dev/github.com/skyoo2003/acor/pkg/acor).

## Project

[Contributing](CONTRIBUTING.md) · [Support](SUPPORT.md) · [Security](SECURITY.md) ·
[Code of Conduct](CODE_OF_CONDUCT.md) · [Governance](GOVERNANCE.md) ·
[Changelog](CHANGELOG.md)

## License

[Apache License 2.0](LICENSE) — Copyright 2016-2026 Sungkyu Yoo
