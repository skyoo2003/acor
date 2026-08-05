---
title: "Benchmarks"
weight: 5
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
two `HGETALL` calls into one trip. V1's round-trip cost is on **writes**, where it
walks the trie node by node, so the cost grows with the length of the keyword being
added rather than with the size of the dictionary.

```sh
go test -run RTT ./pkg/acor

# The same counts against a real server, which is what makes them structural
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
several minutes, and its timings are not published; see the caveats below.

### Find, 1000 keywords

| Configuration | ns/op | B/op | allocs/op | vs V1 |
|---|---|---|---|---|
| V1 | 129,062 | 39,519 | 1,070 | baseline |
| V2, no cache | 224,738 | 121,142 | 2,060 | ~1.7x slower |
| V2 + `EnableCache`, warm | 8,631 | 7,898 | 62 | ~15x faster |
| `PresetBalanced` | 2,204 | 2,048 | 4 | **~59x faster** |

### Find, 100 keywords

| Configuration | ns/op | B/op | allocs/op | vs V1 |
|---|---|---|---|---|
| V1 | 91,724 | 8,136 | 158 | baseline |
| V2, no cache | 79,885 | 11,144 | 219 | ~1.1x faster |
| V2 + `EnableCache`, warm | 3,120 | 2,857 | 10 | ~29x faster |
| `PresetBalanced` | 2,214 | 2,048 | 4 | ~41x faster |

At 100 keywords V1 and V2 are close enough that the winner changes between runs;
only the 1000-keyword gap is stable.

### Add

| Configuration | ns/op | vs V1 |
|---|---|---|
| V1 | 4,956,892 | baseline |
| V2 | 353,149 | **~14x faster** |

### Bulk load, `AddMany`

`AddMany` plans the whole batch in one pass and commits it in a single
transaction, so it costs two round trips regardless of batch size.

On the Apple M4 with Redis 8 on loopback setup above, one sample measured:

| Keywords | ns/op | B/op | allocs/op |
|---|---|---|---|
| 100 | 422,665 | 199,850 | 1,079 |
| 1,000 | 2,958,026 | 1,668,568 | 8,816 |

Reproduce with `ACOR_INTEGRATION_ADDR=localhost:6379 make bench-module`.

### Run-to-run variance

The `ns/op` columns are one run. Repeat runs on the same idle laptop moved every
absolute number by 20-25% while the ratios held to within about 15%.

That is why the ratios are stated approximately and the raw numbers are labelled a
sample. Absolute figures differing from ours is expected; *ratios* differing
substantially is worth reporting as an issue.

## What these numbers mean

**V2 without caching is still somewhat slower than V1 on reads**, because of
payload rather than round trips. Both cost one trip, but V1's `SMEMBERS` returns
just the keyword set while V2 must read an outputs hash carrying one entry per
trie state. The gap widens with the dictionary: roughly parity at 100 keywords,
~1.7x at 1000.

Both schemas memoize the automaton, so an unchanged collection is not re-parsed or
rebuilt between reads. Uncached V2 was ~9x slower than V1 before that memoization
landed; what remains is inherent to reading the whole outputs hash, and
`EnableCache` is the fix for it.

**The large read speedups come from caching, not from the schema.** 15x to 59x
belongs to `EnableCache` and the `Preset` engines. Choosing V2 and nothing else
does not deliver them.

**V2's unambiguous win is writes.** ~14x on `Add()`, and it is the only schema
that supports caching or preset engines at all. Use `AddMany` rather than a loop
over `Add`: it commits in a single transaction. In the Apple M4/Redis 8 loopback
sample, 1,000 keywords cost 3.0 ms instead of the ~350 ms the same writes cost one
at a time.

Practical reading: choose V2, and enable `EnableCache` or a `Preset` if your
workload is read-heavy. V2 with neither is the one configuration these numbers
do not recommend.

## Caveats

- Loopback Redis has almost no network latency. Over a real network both schemas
  still pay one round trip on `Find()`, so V2's larger payload matters more rather
  than less, and V1's per-node `Add()` cost grows with the added latency.
- These figures compare ACOR configurations against each other, not against other
  Aho-Corasick implementations. A single process with a static dictionary is better
  served by an in-memory library. ACOR earns its cost when several instances share
  one dictionary that changes at runtime.
- Every other benchmark in the repository runs on miniredis, an in-process
  emulator with no round-trip cost. Those exist for regression detection and are
  deliberately not published here.
