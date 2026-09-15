---
title: "V3 delta-search validation archive"
description: "Archived measurements for the retired V3 delta-search experiment."
---

# V3 delta-search validation archive

This page preserves the Redis and Valkey measurements from the retired
`VersionedOptions.DeltaSearch` experiment. The experiment used a base automaton
plus an additions automaton and deletion set for small updates, but its release
gate failed: search p95 reached 2.62 times the full-rebuild baseline and Korean
peak RSS reached 1.55 times the baseline.

V3 now serves one immutable engine for every version. `DeltaSearch` remains in
the public option type for source compatibility, but it has no effect, and
`DeltaSearch`/`DeltaKeywords` report false/zero in `VersionedStatus`.

The archived raw results remain in
[`benchmarks/results/v3-delta-20260914-redis.json`](https://github.com/skyoo2003/acor/blob/main/benchmarks/results/v3-delta-20260914-redis.json)
and
[`benchmarks/results/v3-delta-20260914-valkey.json`](https://github.com/skyoo2003/acor/blob/main/benchmarks/results/v3-delta-20260914-valkey.json).
They are historical workstation measurements, not current release gates.

For current V3 measurements, run the single-engine scale benchmark:

```sh
ACOR_V3_SCALE_ADDR=127.0.0.1:6379 \
ACOR_V3_SCALE_OUTPUT=/tmp/acor-v3-scale \
ACOR_V3_SCALE_REPEATS=2 \
  make bench-v3
```

The benchmark uses disposable collection names, does not run `FLUSHDB`, and
includes the million-keyword safety and pruning scenario.
