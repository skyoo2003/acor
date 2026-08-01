---
title: "Benchmarks"
weight: 4
---

# Benchmarks

Every performance number ACOR publishes is on this page, with the command that
produces it. Nothing here is an estimate.

The evidence comes in two kinds, and they are not interchangeable:

- **Round trips** are structural. They are counted at the storage seam, so they
  are identical on miniredis and on a real server, and no hardware difference
  can move them. They are enforced by tests on every CI run.
- **Timings** are hardware-bound. Absolute nanoseconds on your machine will not
  match ours. The reproducible quantity is the **ratio** between configurations.

## Round trips per operation

Enforced by `TestRTT*` in `pkg/acor/rtt_claims_test.go`. If any of these change,
CI fails.

| Operation | V1 | V2 |
|---|---|---|
| `Find()` | 1 | 1 |
| `Find()` with `EnableCache`, warm | n/a | 0 |
| `Find()` with a `Preset` engine | n/a | 0 |
| `Add()`, 5-character keyword | 53 | 2 |
| `Add()`, 26-character keyword | 507 | 2 |

Both schemas read in a single round trip: V1 issues one `SMEMBERS`, V2 pipelines
two `HGETALL` calls into one trip. V1's round-trip cost is on **writes**, where
it walks the trie node by node — cost grows with the length of the keyword being
added, not with the size of the dictionary.

```sh
go test -run RTT ./pkg/acor

# Same counts against a real server — that is what makes them structural
ACOR_INTEGRATION_ADDR=localhost:6379 go test -run RTT ./pkg/acor
```

## Timings

Measured on: Apple M4, `darwin/arm64`, Go 1.26, Redis 8 on loopback,
`-benchtime=200x`. The exact command that produced the tables below, which takes
about 20 seconds:

```sh
redis-server --port 6379 --save "" --daemonize yes
ACOR_INTEGRATION_ADDR=localhost:6379 \
  go test -bench RealServer -benchmem -benchtime=200x -run '^$' ./pkg/acor
```

`make bench` runs the full sweep including the miniredis benchmarks. That takes
several minutes and its timings are not published — see the caveats below.

### Find, 1000 keywords

| Configuration | ns/op | B/op | allocs/op | vs V1 |
|---|---|---|---|---|
| V1 | 135,067 | 39,631 | 1,073 | baseline |
| V2, no cache | 1,163,098 | 1,264,718 | 11,704 | **~9x slower** |
| V2 + `EnableCache`, warm | 8,281 | 8,472 | 65 | ~15x faster |
| `PresetBalanced` | 2,076 | 2,160 | 7 | **~60x faster** |

### Find, 100 keywords

| Configuration | ns/op | B/op | allocs/op | vs V1 |
|---|---|---|---|---|
| V1 | 71,497 | 8,258 | 161 | baseline |
| V2, no cache | 177,541 | 169,224 | 1,302 | ~2.5x slower |
| V2 + `EnableCache`, warm | 2,942 | 2,995 | 13 | ~24x faster |
| `PresetBalanced` | 1,927 | 2,160 | 7 | ~37x faster |

### Add

| Configuration | ns/op | vs V1 |
|---|---|---|
| V1 | 4,979,354 | baseline |
| V2 | 385,242 | **~12x faster** |

### Run-to-run variance

The `ns/op` columns above are one run. A second run on the same idle-ish laptop
produced 167,907 / 1,567,861 / 11,207 / 2,924 ns/op for the 1000-keyword Find
cases — every absolute number about 20-25% higher, while the ratios moved by
under 15% (9.3x slower, 15.0x faster, 57x faster).

This is the reason the ratios are stated approximately and the raw numbers are
labelled as a sample. If your absolute figures differ from ours, that is
expected. If your *ratios* differ substantially, that is worth reporting as an
issue.

## What these numbers mean

**V2 without caching is slower than V1 on reads.** Uncached V2 `Find()` fetches
the full trie and outputs hashes and rebuilds the match engine on every call —
1.26 MB and 11,704 allocations per operation at 1000 keywords. V1 fetches only
the keyword set and memoizes the engine it builds from it, which amounts to a
local cache V2 does not get by default. Both cost one round trip; the difference
is payload and rebuild work, which round-trip counting cannot see.

**The large read speedups come from caching, not from the schema.** 16x to 65x
belongs to `EnableCache` and the `Preset` engines. Attributing it to V2 alone
would misdescribe what a user gets by choosing V2 and nothing else.

**V2's unambiguous win is writes.** 12.9x on `Add()`, and it is the only schema
that supports caching or preset engines at all.

Practical reading: choose V2, and enable `EnableCache` or a `Preset` if your
workload is read-heavy. V2 with neither is the one configuration these numbers
do not recommend.

## Caveats

- Loopback Redis has almost no network latency. Over a real network both schemas
  still pay one round trip on `Find()`, so V2's larger payload matters more, not
  less; V1's per-node `Add()` cost also grows with real latency.
- These figures compare ACOR configurations against each other. They are not a
  comparison against in-memory Aho-Corasick libraries, which answer a different
  question — a single process with a static dictionary should use one of those.
- Every other benchmark in the repository runs on miniredis, an in-process
  emulator with no round-trip cost. Those exist for regression detection and are
  deliberately not published here.
