---
title: "V3 performance and acceptance report"
description: "Reproducible million-keyword measurements on Redis and Valkey."
---

# V3 performance and acceptance report

## Sharded layout qualification

The sharded V3 release adds a reproducible measurement schema and regression gate.
It does not yet have a controlled 1M/10M/upper-bound performance report or a live
multi-node Cluster failure campaign. Functional tests validate legacy compatibility,
generation installation, snapshot-safe layout transitions, and single-tag scripts;
they do not establish deployment latency, memory, or failover limits. No speedup or
maximum dictionary size is claimed.

Use a disposable Redis/Valkey endpoint and a fresh result directory for each matrix.
Approve the host's RAM capacity before setting the upper bound: every serving process
loads the full searchable index, and full replacement or resharding can need old and
new engines at once. The default matrix uses 1M, 10M, and the configured upper bound
for each of `shared`, `diverse`, and `korean`, with fixed shard profiles 1, 4, and 16
and three repetitions. Profile 1 is legacy V3. Each workload/profile/repetition gets
its own Go process so its RSS high-water mark is independent.

```sh
ACOR_V3_SCALE_ADDR=127.0.0.1:6379 \
ACOR_V3_SCALE_UPPER_N=20000000 \
ACOR_V3_SCALE_ENVIRONMENT='host-hardware-and-redis-config-revision' \
ACOR_V3_SCALE_OUTPUT=/tmp/acor-v3-baseline \
env -u GOROOT sh scripts/benchmark-v3.sh
# Repeat on the same reserved host/endpoint/configuration and Go runtime,
# with candidate code and a new ACOR_V3_SCALE_OUTPUT directory.
python3 scripts/test-v3-results.py
python3 scripts/check-v3-regression.py \
  /tmp/acor-v3-baseline/results.json /tmp/acor-v3-candidate/results.json
```

The 20M upper bound above is illustrative and requires a capacity-approved host.
Use `ACOR_V3_SCALE_COUNTS='1000'`, `ACOR_V3_SCALE_REPEATS=1`, and a separate output
directory for a small smoke matrix. Smoke measurements prove the harness/schema
works; they cannot qualify the larger deployment. `ACOR_V3_SCALE_TIMEOUT` defaults
to 60m per run. The separate million-entry safety test still runs once per matrix.
`ACOR_V3_SCALE_PROFILES` can select a bounded set of valid counts for a deployment
profile; compare the same profile keys on both revisions. Keep persistence, CPU
limits, server configuration, background load, and memory policy unchanged and
encode their revision in `ACOR_V3_SCALE_ENVIRONMENT`. Recorded environment fields
include host, Go/OS/architecture, CPU count, `GOMAXPROCS`, server version/build/mode,
endpoint, seed, preset, polling/debounce, shard concurrency, `GOGC`, and `GOMEMLIMIT`.
This metadata cannot automatically prove that a host was otherwise idle or its
Redis configuration identical.
The gate requires identified host/runtime/server fields even when both inputs have
identical metadata. The harness fails on an unavailable `INFO server` response or
missing server identity. Coincident CRUD sizes (one keyword, 1,000 keywords, and 1%)
are measured once per run so custom small counts retain unique operation keys.

Schema version 2 stores each operation's measurements:

| Field | Interpretation |
|---|---|
| `shard_count`, `shard_min_keywords`, `shard_max_keywords`, `shard_skew_ratio` | Installed layout and keyword balance; skew is max / mean, including empty shards, or zero for an empty dictionary |
| `changed_shards` | Changed-shard count, derived by comparing immutable manifest IDs; IDs themselves are not emitted. Zero for an identical replacement; all target shards for a layout change |
| `commit_ms`, `ready_ms`, `refresh_lag_ms` | Write duration, write-through-local-readiness duration, and duration from write return to `WaitForVersion` returning; includes polling, debounce, and wait overhead |
| `search_samples`, `search_p50_ns`, `search_p95_ns`, `search_p99_ns` | Timed local searches during the write/readiness window; very short no-op windows can have only one sample |
| `max_rss_bytes` | Process-lifetime RSS high-water mark at that operation; not instantaneous heap or Redis memory |
| `gc_pause_ns`, `gc_cycles` | Go stop-the-world pause total and completed GC-cycle deltas for the measurement window; pause total is not a pause percentile |

The gate rejects missing/invalid shard and timing evidence, no search samples,
mismatched environments, different repetition counts, and missing workload/profile/
operation keys. Fresh schema-2 baselines are required; archived legacy JSON is not
comparable. It gates ready time, search p95, refresh lag, and RSS per operation using
the median across repetitions; add/remove operations also gate `commit_ms` at the
same changed-shard count. An operation's regression cannot be hidden by unrelated
operation medians. p50/p99, skew, and GC are diagnostic evidence, not additional
threshold gates. The default relative ceiling is 25%, intended as a workstation
guard; controlled release hosts should set an agreed ceiling through
`--max-regression`. A zero baseline allows no positive increase, so tiny no-op
measurements may need a longer, separately designed observation window. Passing
this gate is evidence for the recorded matrix only, not a performance guarantee.

Local searches contact no Redis server, including during refresh. Shards distribute
storage and bound changed-shard rebuild work; each serving replica still needs the
entire local index. Measure node-level network/storage balance separately on an
actual Cluster before making placement or failover claims. See
[V3 memory, resharding, and recovery guidance](../versioned/).

## Archived R1 baseline

The following tables describe legacy R1 only. For the implementation and measurements that followed, see the
[R2/R3 comparison](../r2-r3-performance/).

Measured 2026-09-06. The final matrix completed **36 runs**: Redis and Valkey × three
dictionary sizes × three deterministic distributions × two repetitions. The separate
million-entry safety scenario passed on both servers. These are measurements of one
environment, not throughput or memory guarantees.

## Environment and method

- Apple M4, 10 logical CPUs, 16 GiB RAM; macOS 26.5.2, Darwin 25.5.0, arm64.
- Go 1.26.7; Redis 8.10.1; source-built Valkey 9.1.2.
- Standalone TCP on localhost; RDB snapshots and AOF disabled — no production durability
  or cross-node network overhead is included.
- MemoryEfficient engine; 50 ms version polling for measurement (the library default is 30
  seconds); five-minute leases renewed every minute.
- A separate Go process per size/distribution/repetition, two complete repetitions, on a
  developer workstation with some development checks running on the host. Treat the ranges
  as baseline observations, not isolated microbenchmark confidence intervals. Each server
  ran one scale workload at a time.
- Fixed seed 20260906. Shared: `shared-prefix-%08d`. Diverse: a random 12-bit hex prefix
  plus a unique eight-digit index. Korean: `한국어-%04x-키워드-%08d` with a random 16-bit
  prefix. Full replacement appends `-x` to every keyword.

**Valkey qualification.** An exploratory run of the local Valkey 9.1.2 build with its
default prefetch setting exited with SIGSEGV in `hashtableIncrementalFindStep` via
`prefetchCommandQueueKeys`. The final successful Valkey matrix used
`prefetch-batch-max-size 0`, so this report does **not** establish support for that build's
default configuration. The library returned an error when the server disappeared, and the
root cause has not been established. The Valkey source archive
SHA-256 was `19c23908e7d57e8d91ef85b41f5646307582f10f4f0fb999bbf89ed24ec9c983` (tag
9.1.2), built with `make -j4 valkey-server`. Prefetch was disabled through its existing
configuration option, without modifying server source.

## Initial load and startup

Cells show minimum–maximum across two runs. Load-ready is elapsed from `Replace` to the
local engine serving its version. Startup opens a new collection instance against the
existing dictionary and builds its first engine. Peak RSS is the process lifetime
high-water mark across the entire run, including subsequent changes.

| Server | Keywords | Distribution | Load-ready (s) | Startup (s) | Peak RSS (GiB) |
|---|---:|---|---:|---:|---:|
| redis | 10,000 | shared | 0.52–0.53 | 0.23–0.24 | 0.03–0.03 |
| redis | 10,000 | diverse | 0.51–0.61 | 0.25–0.25 | 0.07–0.07 |
| redis | 10,000 | korean | 0.53–0.53 | 0.26–0.26 | 0.14–0.14 |
| redis | 100,000 | shared | 0.64–0.69 | 0.31–0.32 | 0.13–0.14 |
| redis | 100,000 | diverse | 0.74–0.74 | 0.41–0.42 | 0.43–0.43 |
| redis | 100,000 | korean | 0.94–0.99 | 0.56–0.58 | 0.88–0.92 |
| redis | 1,000,000 | shared | 1.81–1.84 | 1.19–1.23 | 1.20–1.20 |
| redis | 1,000,000 | diverse | 3.03–3.07 | 2.23–2.25 | 3.14–3.55 |
| redis | 1,000,000 | korean | 3.89–3.96 | 2.88–2.98 | 3.02–3.74 |
| valkey | 10,000 | shared | 0.51–0.51 | 0.24–0.24 | 0.03–0.03 |
| valkey | 10,000 | diverse | 0.51–0.52 | 0.24–0.25 | 0.07–0.07 |
| valkey | 10,000 | korean | 0.53–0.53 | 0.26–0.26 | 0.13–0.14 |
| valkey | 100,000 | shared | 0.63–0.64 | 0.31–0.32 | 0.14–0.15 |
| valkey | 100,000 | diverse | 0.74–0.74 | 0.41–0.42 | 0.42–0.43 |
| valkey | 100,000 | korean | 0.92–0.92 | 0.57–0.57 | 0.85–0.89 |
| valkey | 1,000,000 | shared | 1.83–1.84 | 1.19–1.53 | 1.21–1.30 |
| valkey | 1,000,000 | diverse | 2.98–3.06 | 2.21–2.51 | 3.02–3.80 |
| valkey | 1,000,000 | korean | 3.86–3.87 | 2.85–2.88 | 3.37–3.69 |

## Million-keyword changes

Commit covers normalization, bucket reads and preparation, manifest storage, and the final
pointer commit. Ready adds notification/polling and engine rebuilding; their difference is
the calling instance's serving-version lag, and fleet-wide simultaneous activation is not
asserted. Unchanged buckets are reused, but every changed generation still rebuilds its
entire local engine.

| Server | Distribution | Add 1 commit / ready (s) | Add 1,000 ready (s) | Add 1% ready (s) | Full replace ready (s) |
|---|---|---:|---:|---:|---:|
| redis | shared | 0.006–0.006 / 1.20–1.21 | 1.40–1.41 | 2.07–2.08 | 2.86–2.88 |
| redis | diverse | 0.006–0.006 / 2.28–2.30 | 2.70–2.71 | 3.01–3.38 | 3.98–4.34 |
| redis | korean | 0.006–0.006 / 2.96–3.17 | 3.55–4.48 | 4.63–6.11 | 7.43–7.71 |
| valkey | shared | 0.005–0.005 / 1.21–1.23 | 1.40–1.41 | 2.05–2.08 | 2.86–2.88 |
| valkey | diverse | 0.005–0.007 / 2.33–2.40 | 2.42–2.60 | 3.03–3.54 | 4.08–4.33 |
| valkey | korean | 0.005–0.005 / 2.90–3.40 | 4.03–4.38 | 3.98–4.49 | 6.78–7.31 |

The JSON report also covers removals of 1, 1,000 and 1%, and identical `Replace`. At
100,000 entries, 1,000 and 1% are the same count and are measured as two successive
add/remove cycles. Identical `Replace` still reads and compares buckets; it creates no
generation and rebuilds no engine.

## Search during replacement, and retained storage

Search sampling uses a short string assembled from three dictionary keywords, taken at
roughly one-millisecond intervals while a write and its engine refresh run. The timer
includes that string construction and the `Find` call. The old engine keeps serving until
replacement; initial load starts with an empty engine. These are not general text-search
throughput figures. The cells summarize full replacement at one million keywords.

| Server | Distribution | Search p50 / p95 / p99 (µs) | Redis before prune (MiB) | Redis after prune (MiB) |
|---|---|---:|---:|---:|
| redis | shared | 2.33–2.33 / 3.46–3.50 / 9.75–12.04 | 94.29–94.31 | 31.42–31.42 |
| redis | diverse | 2.50–2.62 / 4.71–6.29 / 12.21–14.79 | 66.61–66.63 | 23.06–23.08 |
| redis | korean | 3.00–3.04 / 12.38–13.00 / 30.08–31.38 | 134.60–134.60 | 43.82–43.84 |
| valkey | shared | 2.38–2.38 / 3.33–3.62 / 9.50–10.33 | 94.49–94.51 | 31.60–31.60 |
| valkey | diverse | 2.50–2.71 / 3.96–5.46 / 12.33–14.38 | 66.82–66.82 | 23.25–23.26 |
| valkey | korean | 3.04–3.04 / 11.62–14.00 / 25.04–29.92 | 134.78–134.78 | 43.99–43.99 |

Redis memory is server-wide `used_memory`, including server overhead, receipts, script
cache, and retained manifests and chunks. The pruning measurement artificially ages
inactive generation registry timestamps beyond 24 hours; the active generation stays
protected, and actual retention is tested separately. Network byte counters in the JSON
include writes, engine downloads, polling, protocol, and measurement overhead — not just
payload keyword bytes.

## Acceptance evidence

- The 36 scale runs completed initial loading, reopening, all paginated entries,
  naive-reference search comparison, no-op and full replacements, 1/1,000/1% additions and
  removals, `WaitForVersion`, and explicit pruning.
- `TestVersionedMillionSafety` passed on both real servers: of two writers using the same
  million-entry expected version, one won and the other conflicted. A pinned million-entry
  snapshot stayed readable across commit and `Prune`, contained no competing-generation
  keywords, and was collected only after close.
- Unit and race tests compare V2 and V3 search results for Korean, multilingual,
  nested/overlapping, case-sensitive, and stream-boundary cases. Concurrent batch scans
  preserve a single engine generation.
- Fault injection covers chunk preparation errors, abandoned prepared generations, lost
  commit responses and delayed retries, dropped Pub/Sub, failed engine loads, reconnect,
  lease expiry, and a stale pruning fence. These are deterministic simulations, not a
  production failover or forced process-kill test.
- Chunk-write instrumentation confirms an identical dictionary writes no chunks and a
  single change writes only its affected bucket. The final Lua commit checks fixed keys,
  writes a receipt and marker, and swaps the pointer — it parses no manifest and iterates
  no keywords.
- Root and server race suites, root/server lint, all-module vet, benchmark-module tests,
  API snapshot/audit, documentation compilation, unchanged module manifests, and license
  attribution passed. FuzzFind and FuzzAdd ran 30 seconds each; V3 normalization fuzzing
  ran 10 seconds. No production `acor` function remained at zero coverage in the normal
  test suite.

No real Cluster/Sentinel failover matrix or production persistence recovery was run for V3
here. All collection keys use one hash tag and the existing connection implementations —
do not read standalone measurements as multi-node results.

## Reproduce

Use a disposable server with the same persistence, preset, and polling settings. From the
repository root:

```sh
ACOR_V3_SCALE_ADDR=127.0.0.1:6379 \
ACOR_V3_SCALE_OUTPUT=/tmp/acor-v3-redis \
ACOR_V3_SCALE_REPEATS=2 make bench-v3
```

Repeat against Valkey with `prefetch-batch-max-size 0` and a separate output directory.
`scripts/benchmark-v3.sh` compiles one test binary, starts a fresh process for every
size/distribution, then runs the million-entry safety scenario. It removes only its own
uniquely named collection keys, never `FLUSHDB`. Do not run another workload on the same
endpoint during measurement — Redis memory and network counters are server-wide. Raw
operation summaries, environment strings, startup/prune measurements, and source hashes
are in `test/benchmarks/results/v3-20260906.json`; a new run also writes
`$ACOR_V3_SCALE_OUTPUT/results.json` for the regression gate.

## Regression gate

Compare two result files from the same disposable Redis/Valkey environment:

```sh
ACOR_V3_BASELINE_JSON=test/benchmarks/results/v3-20260906.json \
ACOR_V3_CANDIDATE_JSON=test/benchmarks/results/v3-20260915-redis.json \
make v3-regression-check
```

The gate compares the median of operation medians for `ready_ms`, `search_p95_ns`,
and `max_rss_bytes` within each matching size and distribution. This reduces
single-operation p95 noise while still covering every operation in the run. It allows a 25% increase by
default; set `ACOR_V3_MAX_REGRESSION` for a controlled environment. Missing or
incomparable measurements fail instead of being silently skipped.
