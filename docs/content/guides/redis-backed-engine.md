---
title: "Redis-Backed Engine"
weight: 4
---

# Redis-Backed Engine

With `Preset` set, Redis stays the source of truth while every instance serves reads from
its own automaton. This page covers how that stays consistent. Which preset to pick is
[Preset-Optimized Engine](../preset-engine/).

> **Redis or Valkey.** ACOR connects over RESP via
> [go-redis v9](https://github.com/redis/go-redis), so Redis 3.0+ or Valkey 7.2+ works in
> Standalone, Sentinel, Cluster, and Ring topologies. Cross-instance invalidation uses
> server Pub/Sub, which behaves identically on both. CI covers both.

## Architecture

```text
                    Write Path
Instance A ──Add()──▶ Lua Script (optimistic lock) ──▶ Redis
                                                  │
                       Pub/Sub invalidate ◀────────┘
                            │
Instance B ◀────────────────┘
      │
      └─ ensureValid() ──▶ reload from Redis ──▶ rebuild local engine

                    Read Path
Instance A ──Find()──▶ local engine (0 RTT)
```

| Path | Behavior |
| ---- | -------- |
| Writes | V2 Lua scripts with optimistic locking, up to 3 retries with backoff |
| Reads | Local automaton, no Redis I/O |
| Invalidation | Redis Pub/Sub on every mutation |
| Failed reload | Previous engine is retained but the waiting search returns an error; a later search retries |

## Invalidation safety

Pub/Sub is best effort — a disconnected subscriber misses invalidations. In multi-instance
deployments set `InvalidationPollInterval`:

<!-- doccheck -->
```go
args := &acor.AhoCorasickArgs{
    Addr:                     "localhost:6379",
    Name:                     "my-collection",
    Preset:                   acor.PresetBalanced,
    InvalidationPollInterval: 30 * time.Second,
}
_ = args
```

Zero disables polling, and polling applies to Preset mode only. Each poll reads just the
`version` hash field; a change marks the engine stale and the next search loads the full
snapshot.

**The interval is not a freshness bound.** Redis failures, query latency, and rebuild time
all delay recovery. This is eventual refresh, not strong consistency.

Concurrent stale reads share one reload job while each request observes its own context
cancellation — cancelling one waiter leaves the others running, and all waiters leaving
(or `Close`) cancels the job. Redis reads and engine builds run outside the state lock,
and a generation check rejects snapshots overtaken by local writes.

Watch `CacheStats().PresetReloadFailures` (once per failed shared job) and
`PresetPollFailures` (per failed version poll) alongside your search errors. Cancellation
is excluded from both; both stay zero outside Preset mode. A retained previous engine does
not turn a failed reload into a successful response. See
[Monitoring](../../operations/monitoring/).

## Topologies and connection tuning

All four topologies are configured through the connection fields on `AhoCorasickArgs` —
see [Redis topologies](../../getting-started/quick-start/#redis-topologies).

`DialTimeout`, `ReadTimeout`, `WriteTimeout`, `MaxRetries`, and `PoolSize` pass straight
to go-redis for every topology. Zero keeps the go-redis default; `-1` disables the
read/write timeouts or command retries where supported.

## Preset mode versus plain V2

| | No `Preset` | With `Preset` |
|---|---|---|
| Read latency | 1 RTT, or 0 with `EnableCache` | 0 RTT |
| Write latency | Lua script | Lua script + optimistic lock |
| Cross-instance sync | Pub/Sub cache invalidation | Pub/Sub engine rebuild |
| Schema | V1 or V2 | V2 only |
| `Suggest` / `SuggestIndex` | Yes | No — `ErrSuggestRequiresRedis` |
| Batch, parallel matching | Yes | Yes |

`Preset` is unset by default (`PresetNone`), which runs the original mode. Choose it when
you need the fastest reads across several instances and can accept V2-only, no-`Suggest`.
