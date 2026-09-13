---
title: "V3 delta-search validation"
description: "Opt-in delta search: parity, release gates, and reproducible Redis/Valkey measurements."
---

# V3 delta-search validation

`VersionedOptions.DeltaSearch` remains opt-in. This report evaluates the small-update
search view against the existing full rebuild path; it does not change the V3 storage
format or the default search behavior.

## Release gate

For each server and each million-keyword distribution (shared prefix, diverse prefix,
and Korean), run the baseline and delta modes in three independent processes. Compare
medians for additions and removals of one and 128 keywords:

| Metric | Required delta result |
|---|---|
| `WaitForVersion` ready time | At most 30% of full-rebuild baseline |
| Steady search p95 after readiness | At most 110% of baseline |
| Process peak RSS after compaction | No greater than baseline |

Correctness is mandatory even if the timing gates pass. Changes of 129 keywords must
fall back to a full rebuild. Every search API must agree with a fully rebuilt engine
while the overlay is active and after compaction. Failed compaction must retain the
already serving version and report the error. A missed gate keeps the option off by
default and is reported rather than hidden by an aggregate across servers.

## Measurement method

`scripts/benchmark-v3.sh` runs `TestVersionedDeltaScale` when
`ACOR_V3_DELTA_COMPARE=1`. It starts a fresh Go process per mode, distribution, and
repetition against a disposable real server, and cleans up only its uniquely named
collection keys. The test uses the fixed seed 20260906 and MemoryEfficient preset,
with a 50 ms polling interval. It loads one million entries, measures add/remove 1,
add/remove 128, add/remove 129, then measures search during idle compaction. The
ready interval includes the Redis write and `WaitForVersion`; the search p95 is sampled
for 500 ms immediately after readiness using the same short query in both modes.
The RSS field is the process high-water mark at each sample, not an isolated allocation
count for that phase. The final RSS comparison includes compaction. `heap_alloc` is a
point-in-time Go heap observation. Search timing is local and excludes Redis I/O.

The current evaluator also records p95 separately for `Find`, `FindSet`,
`FindIndex`, `FindMatches`, `Contains`, `FindStream`, `FindBatch`,
`FindParallel`, `FindIndexParallel`, `Scan`, `MaskText`, and `ReplaceText`.
Each mode uses identical text for added-keyword, removed-keyword, and
delta-miss cases. The removed-base case actually deletes a word from the
million-word base. Parallel calls use 2 workers, 128-rune chunks, and text
long enough to create multiple chunks. The p95 of each API/input pair must be
at most 1.10 times its full-rebuild counterpart. The evaluator records RSS
after the 129-keyword full rebuild and again after idle compaction; the
process-wide high-water mark after compaction is the RSS gate. Runs from the
older format, without `search_apis`, cannot satisfy the current evaluator.

The R1/R2 reports used a development workstation with persistence disabled. This
comparison should use the same host, Go version, Redis 8.10.1, and Valkey 9.1.2 with
`prefetch-batch-max-size 0`, running one server workload at a time. The Valkey source
archive SHA-256 is recorded in the [R1 report](../versioned-performance/).

```sh
ACOR_V3_SCALE_ADDR=127.0.0.1:6379 \
ACOR_V3_DELTA_COMPARE=1 \
ACOR_V3_SCALE_COUNTS=1000000 \
ACOR_V3_SCALE_MODES='baseline delta' \
ACOR_V3_SCALE_REPEATS=3 \
ACOR_V3_SCALE_OUTPUT=/tmp/acor-v3-delta-redis \
  make bench-v3
python3 scripts/evaluate-v3-delta.py /tmp/acor-v3-delta-redis
```

Run the same command with a separate output directory against Valkey. The evaluator
exits nonzero if a log is missing, a run failed, or any numerical gate fails. It does
not combine Redis and Valkey results into one passing average.

## Final results (2026-09-14)

The final matrix ran on macOS/Darwin 25.6.0, arm64, 10 logical CPUs, Go
1.26.7, Redis 8.10.1, and source-built Valkey 9.1.2. Both servers used
localhost with persistence disabled; Valkey used `prefetch-batch-max-size 0`.
Three independent processes per mode and distribution produced the medians in
[`v3-delta-20260914-redis.json`](https://github.com/skyoo2003/acor/blob/main/benchmarks/results/v3-delta-20260914-redis.json)
and [`v3-delta-20260914-valkey.json`](https://github.com/skyoo2003/acor/blob/main/benchmarks/results/v3-delta-20260914-valkey.json).
The [results dashboard](../versioned-delta-dashboard/) shows every server and
distribution, plus the API-level p95 failures.

| Server | Search p95 failures (of 555) | Worst p95 ratio (≤1.10) | Worst ready ratio (≤0.30) | Korean peak RSS ratio (≤1.00) |
|---|---:|---:|---:|---:|
| Redis | 202 | 2.61 | 0.164 | 1.552 |
| Valkey | 208 | 2.62 | 0.164 | 1.527 |

All 30 ready-time checks passed, but 410 of 1,110 search p95 comparisons
failed. The Korean peak-RSS check failed on both servers. No API sample
overlapped a compaction build. Peak RSS is a process high-water mark, so the
comparison does not isolate an individual allocation. These workstation
measurements do not establish production latency or memory guarantees.

The release gate **fails**. `DeltaSearch` remains opt-in.
