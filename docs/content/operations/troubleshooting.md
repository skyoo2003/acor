---
title: "Troubleshooting"
weight: 3
---

# Troubleshooting

## Errors from `Create` and the matching API

| Error | Cause | Fix |
| ----- | ----- | --- |
| `ErrRedisConflictingTopology` | More than one topology configured at once | Use exactly one of `Addr`, `Addrs` (+`MasterName` for Sentinel), or `RingAddrs` — see [Redis topologies](../../getting-started/quick-start/#redis-topologies) |
| `ErrRedisClusterDB` | Non-zero `DB` together with `Addrs` | Cluster has no database selection; drop `DB`, or use `Addr` for a single standalone server |
| `ErrEmptyKeyword` | Empty string passed to `Add` | Trim and reject before calling |
| `ErrInvalidChunkSize` | Non-positive `ParallelOptions.ChunkSize` | `ChunkSize` is required and must be > 0 |
| `ErrRedisAlreadyClosed` | Operation on a closed instance | `defer ac.Close()` once, at the owning function's exit |
| `ErrV1ReadOnly` | `Add`/`Remove` on a V1 collection | Migrate: `acor -name mycollection migrate` |
| `ErrSuggestRequiresRedis` | `Suggest` in `Preset` mode | Suggest needs the Redis path; use a non-preset instance for it |

## Redis connection

| Message | Check |
| ------- | ----- |
| `connection refused` | `redis-cli ping`, the address, the firewall, network reachability |
| `NOAUTH Authentication required` | Set `Password` |
| `context deadline exceeded` | Redis load, network latency, and the timeouts below |

Zero values keep the go-redis defaults across every topology. Tune from measurements, not
from guesses:

<!-- doccheck -->
```go
args := &acor.AhoCorasickArgs{
    Addr:         "localhost:6379",
    Name:         "my-collection",
    DialTimeout:  5 * time.Second,
    ReadTimeout:  3 * time.Second,
    WriteTimeout: 3 * time.Second,
    MaxRetries:   3,
}
_ = args
```

`PoolSize` is the knob for measured connection contention.

## Preset cache looks stale

Preset mode reloads through best-effort Pub/Sub, and a disconnected subscriber misses an
invalidation. In multi-instance deployments, enable polling:

```go
args.InvalidationPollInterval = 30 * time.Second
```

Disabled by default, and ignored outside `Preset` mode. **The interval is not a freshness
bound** — recovery needs a successful version poll *and* a successful reload on the next
search. When updates stay invisible, check `CacheStats().PresetPollFailures` and
`PresetReloadFailures`; reload errors are returned to searches rather than silently served
from the retained engine. See
[invalidation safety](../../guides/redis-backed-engine/#invalidation-safety).

## Slow reads or high memory

```bash
acor -name mycollection schema-version   # V1 is the usual answer to "why is Find slow"
acor -name mycollection info             # keyword and node counts
redis-cli info memory
```

- On V1, migrate to V2 — see [Schema V2](../../reference/schema-v2/).
- On V2, a read-heavy workload wants `EnableCache` or a `Preset`; the schema alone does
  not make reads fast ([benchmarks](../../reference/benchmarks/#what-the-numbers-mean)).
- For large texts, use [parallel matching](../../guides/parallel-matching/).
- For high memory, remove unused keywords or move to `PresetMemoryEfficient`.

## Debugging

```bash
acor -name mycollection -debug find "test text"   # CLI debug logging
redis-cli keys "{mycollection}:*"                 # what the collection actually stores
```

In library code, set `Debug: true` for the default stdout logger, or supply your own
`Logger`.
