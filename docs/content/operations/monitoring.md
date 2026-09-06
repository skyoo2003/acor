---
title: "Monitoring"
weight: 2
---

# Monitoring

Two layers, and which you get depends on how you run ACOR:

| Layer | What it gives you | Covered by `v1` |
| ----- | ----------------- | --------------- |
| `pkg/acor` — the library | `CacheStats()`: hit rate, rebuild cost, invalidation lag | ✅ |
| `acor/server` — the service | Prometheus metrics, structured JSON logs, OpenTelemetry traces | ❌ experimental |

Embedding the library gets you the first row. Everything from
[Service layer](#service-layer) on means importing the separate, experimental
`acor/server` module — see [Server](../../server/) for what that implies for your
`go.mod`.

## Cache statistics

`CacheStats()` does no Redis I/O, so scraping it on a timer is cheap.

<!-- doccheck -->
```go
stats := ac.CacheStats()

// Hits+Misses is the read count. Both are zero before the first read.
hitRate := 0.0
if reads := stats.Hits + stats.Misses; reads > 0 {
    hitRate = float64(stats.Hits) / float64(reads)
}

// What one rebuild costs — the price a write makes every reader pay.
meanRebuild := time.Duration(0)
if stats.Rebuilds > 0 {
    meanRebuild = stats.RebuildDuration / time.Duration(stats.Rebuilds)
}

lag := stats.LastInvalidationLag
_, _, _ = hitRate, meanRebuild, lag
```

Wire those into whatever you already run — a Prometheus collector, an OTel meter, a log
line. ACOR depends on no metrics library on purpose.

### Reading the numbers

- **Per instance, per process.** Nothing is aggregated through Redis, so scrape every
  instance. A restart resets the counters.
- **`Rebuilds` will not equal `Misses`.** Concurrent misses coalesce onto one build, so
  `Misses - Rebuilds` is what coalescing saved; local writes rebuild off the read path and
  push it the other way. In `Preset` mode `Rebuilds` starts at 1, from the build during
  `Create`, and builds discarded after a generation conflict also count. Both are `uint64`
  — check `Misses > Rebuilds` before subtracting, or a write-heavy instance wraps to
  roughly 1.8e19.
- **One scanning call is one read, whatever it scans.** `FindParallel`,
  `FindIndexParallel`, and `FindMany` load the automaton once per call, so each adds 1 to
  `Hits`+`Misses` and their hit rate is comparable to a serial workload's. Writes,
  `Suggest`, and `Info` never reach the automaton and add nothing.
- **`LastInvalidationLag` needs a listener and carries clock skew.** It is populated only
  in `Preset` mode and in V2 with `EnableCache`; elsewhere zero means unavailable, not
  fast. Where it is populated, the publish timestamp comes from another machine's clock,
  so the value is the real delay plus that offset — it can understate as readily as
  overstate. Watch it for step changes, and check NTP before blaming Pub/Sub.
- **A zero hit rate is not always a bug.** Without `Preset` or `EnableCache` every read
  still checks Redis for freshness; a hit means the automaton was reused, not that the
  round trip was skipped.

Not here: match counts, keyword counts, Redis latency. Keyword and node counts come from
`Info()`, which does read Redis.

### Preset refresh failures

Track increases in `PresetReloadFailures` and `PresetPollFailures` alongside search errors.
A shared failed reload increments the first counter once even when many requests receive
the error; a failed version poll increments the second and retries on its next tick.
Cancellation is excluded from both, and both stay zero outside `Preset` mode.

A failed reload keeps the previous engine but **returns an error to the search** rather
than serving that engine as a fallback. Each waiting request answers its own cancellation
without cancelling other waiters; all waiters leaving, or `Close`, cancels the shared job.
Redis reads and engine builds run outside the state lock, and snapshots overtaken by local
state changes are rejected.

Polling reads only the version field, and detects a missed invalidation only after a
successful poll — the next search then fetches and builds the full dictionary. The
configured interval is not an upper bound on staleness during failures.

## Service layer

### Metrics

```go
import "github.com/skyoo2003/acor/server/metrics"
```

| Metric | Type | Description |
| ------ | ---- | ----------- |
| `acor_http_requests_total` | Counter | HTTP requests by method, path, status |
| `acor_http_request_duration_seconds` | Histogram | HTTP request latency |
| `acor_redis_operations_total` | Counter | Redis operations by type, status |
| `acor_redis_operation_duration_seconds` | Histogram | Redis operation latency |
| `acor_keywords_total` | Gauge | Registered keywords |
| `acor_trie_nodes_total` | Gauge | Trie nodes |
| `grpc_server_handled_total` | Counter | gRPC requests by method, code |
| `grpc_server_handling_seconds` | Histogram | gRPC request latency |

gRPC metrics use the standard `grpc_server_*` names from
`go-grpc-middleware/providers/prometheus`, wired by `NewGRPCServerWithObservability`.

Registering them does not expose them — serve `promhttp.Handler()` yourself:

```go
import (
    "net/http"

    "github.com/prometheus/client_golang/prometheus/promhttp"
    "github.com/skyoo2003/acor/server/metrics"
)

func main() {
    // nil registerer defaults to prometheus.DefaultRegisterer,
    // which is what promhttp.Handler() serves
    _ = metrics.NewRegistry(nil)

    http.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(":8080", nil)
}
```

### Logging

`logging.NewLogger(w, level)` takes an `io.Writer` and one of `debug`, `info`, `warn`,
`error`, and returns a zerolog logger that always emits structured JSON. `WithTraceID`
attaches trace and span IDs.

<!-- doccheck:server -->
```go
package main

import (
    "os"

    "github.com/skyoo2003/acor/server/logging"
)

func main() {
    logger := logging.NewLogger(os.Stdout, "info")

    logger.Info().
        Str("operation", "Find").
        Int("duration_ms", 12).
        Int("matches", 5).
        Msg("operation completed")

    // traceID/spanID usually come from an OpenTelemetry span context;
    // any hex strings work here.
    traceID := "4bf92f3577b34da6a3ce929d0e0e4736"
    spanID := "00f067aa0ba902b7"
    logger.WithTraceID(traceID, spanID).Info().Msg("request handled")
}
```

### Tracing

<!-- doccheck:server -->
```go
tracer, err := tracing.NewTracer(&tracing.Config{
    Enabled:     true,
    ServiceName: "my-service",
    Endpoint:    "localhost:4317",
    SampleRatio: 1.0,
})
if err != nil {
    // handle error
}
defer tracer.Shutdown()
```

`pkg/acor` emits no spans of its own. Request spans come from the middleware —
`tracing.HTTPMiddleware` for HTTP, the standard `otelgrpc` stats handler
(`tracing.GRPCStatsHandler`) for gRPC. To trace individual `Add`/`Find`/`Remove` calls,
wrap them in your own spans.

### Alerting

Latency percentiles, error rate, keyword count, and pool utilization are the four worth a
dashboard panel. As rules:

```yaml
groups:
  - name: acor
    rules:
      - alert: HighLatency
        expr: histogram_quantile(0.95, rate(acor_redis_operation_duration_seconds_bucket[5m])) > 0.1
        for: 5m
        annotations:
          summary: "ACOR operations are slow"

      - alert: HighRedisErrorRate
        expr: rate(acor_redis_operations_total{status="error"}[5m]) > 0.1
        for: 5m
        annotations:
          summary: "High Redis error rate"
```
