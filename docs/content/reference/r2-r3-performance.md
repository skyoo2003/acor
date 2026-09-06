---
title: "R2/R3 verification and R1 comparison"
description: "Incremental download, engine memory and bounded source-text API evidence."
---

The R2/R3 implementation completed 36 real-server runs on 2026-09-06 using the
same R1 workload: Redis/Valkey × 10,000/100,000/1,000,000 keywords × shared/diverse/
Korean distributions × two repetitions. The R1 measurements remain unchanged in
`benchmarks/results/v3-20260906.json`. New measurements, source hashes and safety
results are in `benchmarks/results/r2-r3-20260906.json`.

Environment: Apple M4, 10 logical CPUs, 16 GiB RAM, macOS 26.5.2, Go 1.26.7,
Redis 8.10.1 and source-built Valkey 9.1.2. Both are standalone localhost TCP,
with RDB/AOF disabled. The preset remains MemoryEfficient; polling remains 50 ms
for measurement. R2 adds the default fixed 20 ms refresh debounce. Each run uses
a fresh Go process. These are developer-workstation measurements with possible
background host activity, not latency guarantees or confidence intervals.

Valkey uses `prefetch-batch-max-size 0`, as in the successful R1 baseline. The
local default-prefetch build previously crashed; this work does not establish
support for that configuration or diagnose its root cause. See the
[R1 report](../versioned-performance/) for source checksum and crash evidence.

## R2 changes

- The installed engine keeps verified immutable keyword slices and its manifest.
  Unchanged buckets share those slices; only changed nonempty buckets download
  chunk data. Candidate caches are installed with the engine only after success.
- MemoryEfficient builds consume the bucket sequence directly, avoiding the
  flattened full dictionary slice and an additional keyword-set map. Nodes keep
  their only child inline; a map is allocated only when the node branches. BFS
  uses a compact integer queue. The first-rune Bloom filter sizes itself from
  distinct first runes rather than total keyword count.
- Builds check cancellation in insertion, failure-link construction and table
  filling for all three presets. Cancellation discards the private candidate;
  no detached builder goroutine continues after return. Allocations, rune-count
  calls and sorting finish before the next checkpoint.
- A fixed debounce window merges burst notifications. New events cannot extend
  that window; an in-flight build completes under ordinary writes, preventing
  repeated cancellation from starving refresh. Close cancels its build context.
- Status reports the last successful build's DownloadedBuckets/ReusedBuckets and
  cumulative CompletedBuilds. Redis schema, expected-version writes, receipt
  resolution, leases and pruning semantics are unchanged.

## Million-keyword comparison

Ranges below are the minimum–maximum of two runs. Ready is write preparation,
commit, refresh detection and engine construction combined. RSS is the process
high-water mark across the full run, including all update scenarios. Network
counters are server-wide and include protocol, polling and engine downloads;
they do not isolate only keyword payloads.

| Server | Distribution | Add 1 ready: R1 → R2 (s) | Received bytes reduction for add 1 | Peak RSS: R1 → R2 (GiB) |
|---|---|---:|---:|---:|
| redis | shared | 1.20–1.21 → 0.55–0.80 | 94.7% | 1.20–1.20 → 1.01–1.09 |
| redis | diverse | 2.28–2.30 → 1.11–1.47 | 91.9% | 3.14–3.55 → 2.05–2.05 |
| redis | korean | 2.96–3.17 → 1.22–1.32 | 96.2% | 3.02–3.74 → 2.33–2.45 |
| valkey | shared | 1.21–1.23 → 0.53–0.55 | 94.7% | 1.21–1.30 → 1.05–1.16 |
| valkey | diverse | 2.33–2.40 → 0.92–1.27 | 91.9% | 3.02–3.80 → 2.02–2.19 |
| valkey | korean | 2.90–3.40 → 1.21–1.60 | 96.2% | 3.37–3.69 → 2.33–2.50 |

All six million-keyword server/distribution combinations reduced single-change
received bytes and peak RSS in these runs. A full engine is still rebuilt after
a change; this is not an incremental automaton or a constant-memory design.
Larger changes touch more buckets and therefore reuse less downloaded data.

| Server | Distribution | Add 1,000 ready: R1 → R2 (s) | Add 1% ready: R1 → R2 (s) | Full replace ready: R1 → R2 (s) |
|---|---|---:|---:|---:|
| redis | shared | 1.40–1.41 → 0.95–1.22 | 2.07–2.08 → 1.82–2.93 | 2.86–2.88 → 2.38–3.02 |
| redis | diverse | 2.70–2.71 → 1.32–1.46 | 3.01–3.38 → 2.11–4.08 | 3.98–4.34 → 3.06–3.96 |
| redis | korean | 3.55–4.48 → 1.38–1.90 | 4.63–6.11 → 2.64–2.82 | 7.43–7.71 → 3.24–3.92 |
| valkey | shared | 1.40–1.41 → 0.82–1.25 | 2.05–2.08 → 1.80–1.89 | 2.86–2.88 → 2.21–2.67 |
| valkey | diverse | 2.42–2.60 → 1.21–1.29 | 3.03–3.54 → 2.14–2.19 | 4.08–4.33 → 2.77–3.61 |
| valkey | korean | 4.03–4.38 → 1.45–1.60 | 3.98–4.49 → 2.64–2.70 | 6.78–7.31 → 4.17–4.92 |

Improvement is not uniform: some Redis large-change repetitions overlap or
exceed the R1 timing range. Two repetitions do not establish a universal speedup.

The JSON also records 10,000/100,000-entry results, initial loading/startup,
identical replacement, removals, search p50/p95/p99 while refreshing, Redis
memory and pruning. The same seed 20260906 and generation functions are used.
Prune measurements simulate the retention horizon by aging generation registry
scores; they do not wait 24 hours. Separate million-entry safety tests passed
against both servers after the matrix, with no concurrent workload on a measured
endpoint.

## R3 contracts and verification

`Scan`, `MaskText` and `ReplaceText` are additive on AhoCorasick and
VersionedCollection. See [bounded text processing](../text-processing/) for the
complete defaults and error behavior. Existing unlimited APIs are unchanged.

| Area | Evidence |
|---|---|
| Original positions | All presets match existing FindMatches for overlapping and leftmost-longest results; original byte slices survive Korean, emoji, `İ` case folding and malformed UTF-8 input |
| Result limits | At most MaxMatches are retained; Truncated requires an additional eligible result |
| Work/input bounds | Oversize input and excess raw candidates fail explicitly, including candidates later filtered by word boundaries/overlap |
| Atomic rewrite | Match, output and work exhaustion return no partial rewrite; empty literal replacement deletes; masks preserve original rune count, including NUL masks |
| Overlap | A bounded pending-start heap selects leftmost-longest matches without collecting all raw matches |
| Fuzz | FuzzScanLeftmostParity completed 30 seconds and 819,033 executions against the existing leftmost-longest implementation |
| Refresh | Verified bucket reuse, failed-candidate cache preservation and burst coalescing tests pass |
| Cancellation | All three presets preserve the previous engine on cancellation during sequence ingestion and CPU construction; unrelated panics are not swallowed |
| Safety | Both real servers pass the million-entry conflict, pinned paging and lease-safe pruning scenario |

Resource bounds are per call. Scratch input indexing is proportional to the
bounded input size, and the pending window is bounded by input/longest-keyword
length and candidate count. A caller's custom WordRune function controls its own
resource use. R3 does not claim a wall-clock bound on callbacks or allocation.

Root and server race tests, root/server lint, all-module vet, benchmark-module
tests, API snapshot/audit and documentation compilation pass. Legacy Find/Add
fuzz targets are rerun for 30 seconds each. No HTTP/gRPC surface or storage
schema migration is introduced by R2/R3.

## Reproduce

Run the unchanged `scripts/benchmark-v3.sh` against each disposable endpoint with
`ACOR_V3_SCALE_REPEATS=2` and separate output directories. See the
[R1 reproduction instructions](../versioned-performance/#reproduce) for the exact
environment and dictionary distributions. The R2/R3 defaults select the new
engine and refresh behavior automatically; existing V1/V2 public contracts remain
covered by the regression suite.
