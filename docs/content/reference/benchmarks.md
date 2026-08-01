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
| V1 | 128,828 | 39,651 | 1,073 | baseline |
| V2, no cache | 221,253 | 121,255 | 2,063 | ~1.7x slower |
| V2 + `EnableCache`, warm | 8,561 | 8,010 | 65 | ~15x faster |
| `PresetBalanced` | 2,224 | 2,160 | 7 | **~58x faster** |

### Find, 100 keywords

| Configuration | ns/op | B/op | allocs/op | vs V1 |
|---|---|---|---|---|
| V1 | 71,928 | 8,238 | 161 | baseline |
| V2, no cache | 79,853 | 11,256 | 222 | ~1.1x slower |
| V2 + `EnableCache`, warm | 2,772 | 2,969 | 13 | ~26x faster |
| `PresetBalanced` | 2,135 | 2,160 | 7 | ~34x faster |

### Add

| Configuration | ns/op | vs V1 |
|---|---|---|
| V1 | 4,951,188 | baseline |
| V2 | 391,556 | **~12x faster** |

### Run-to-run variance

The `ns/op` columns are one run. Repeat runs on the same idle-ish laptop moved
every absolute number by 20-25% while the ratios held to within about 15%.

This is why the ratios are stated approximately and the raw numbers are labelled
as a sample. If your absolute figures differ from ours, that is expected. If your
*ratios* differ substantially, that is worth reporting as an issue.

## What these numbers mean

**V2 without caching is still somewhat slower than V1 on reads**, and the reason
is payload rather than round trips. Both cost one trip, but V1's `SMEMBERS`
returns just the keyword set while V2 must read an outputs hash carrying one
entry per trie state. That gap widens with the dictionary: roughly parity at 100
keywords, ~1.7x at 1000.

Both schemas memoize the automaton, so an unchanged collection is not re-parsed
or rebuilt between reads. Uncached V2 was ~9x slower than V1 before that
memoization landed; the remainder is inherent to reading the whole outputs hash,
and `EnableCache` is the fix for it.

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
