---
title: "V3 single-engine benchmark — 2026-09-15"
description: "Redis measurements for the V3 single-engine path at up to one million keywords."
---

# V3 single-engine benchmark — 2026-09-15

This report measures the V3 single-engine path after removing the experimental
two-automaton overlay. It ran against Redis at `127.0.0.1:6379` using commit
`212179f`, with three fresh Go processes per size and distribution.

## Environment

- Apple M4, 10 logical CPUs, macOS Darwin 25.6.0, arm64
- Go 1.26.7; Redis 8.10.1; standalone localhost TCP
- MemoryEfficient preset, 50 ms polling, default five-minute leases
- Sizes: 10,000, 100,000, and 1,000,000 keywords
- Distributions: shared prefix, diverse prefix, and Korean
- Seed: `20260906`
- All 27 scale runs and the million-keyword safety scenario passed

These are developer-workstation measurements, not production latency or memory
guarantees. Redis memory and network counters are server-wide. Prune advances
generation timestamps past the retention horizon instead of waiting 24 hours.
Valkey was not measured in this run.

## Million-keyword results

Values are min–max across three runs, with the median in parentheses. RSS is the
process high-water mark; search p95 is sampled during the update.

| Distribution | Initial ready (s) | Startup (s) | Add 1 ready (s) | Full replace ready (s) | Peak RSS (GiB) | Add 1 search p95 (µs) | Redis after prune (MiB) |
|---|---:|---:|---:|---:|---:|---:|---:|
| Shared | 1.193–1.216 (1.198) | 0.770–0.775 (0.774) | 0.509–0.529 (0.520) | 1.603–1.810 (1.643) | 1.098–1.280 (1.111) | 3.583–4.542 (3.834) | 31.483–31.489 (31.488) |
| Diverse | 1.639–1.728 (1.708) | 1.112–1.336 (1.262) | 0.878–0.917 (0.879) | 1.943–2.217 (2.099) | 2.199–2.444 (2.395) | 4.000–6.083 (4.166) | 23.157–23.171 (23.164) |
| Korean | 2.082–2.173 (2.112) | 1.343–1.530 (1.437) | 1.007–1.179 (1.019) | 2.578–2.805 (2.740) | 2.855–3.058 (2.961) | 4.583–12.834 (4.708) | 43.934–43.947 (43.941) |

The raw machine-readable result is
[`v3-20260915-redis.json`](../../../benchmarks/results/v3-20260915-redis.json).

## Reproduce

```sh
ACOR_V3_SCALE_ADDR=127.0.0.1:6379 \
ACOR_V3_SCALE_OUTPUT=/tmp/acor-v3-20260915-redis \
ACOR_V3_SCALE_REPEATS=3 \
  make bench-v3
```

The benchmark uses disposable collection names, does not run `FLUSHDB`, and
includes the million-keyword safety and pruning scenario.
