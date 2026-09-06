---
title: "Deployment"
weight: 1
---

# Deployment

ACOR is a library: you deploy your application, and it connects to Redis. Connection
fields per topology are in
[Redis topologies](../../getting-started/quick-start/#redis-topologies) — standalone for
development and small workloads, Sentinel for failover, Cluster for horizontal scaling,
Ring for client-side sharding.

Read credentials from the environment rather than the source:

```go
ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr:     os.Getenv("REDIS_ADDR"),
    Password: os.Getenv("REDIS_PASSWORD"),
    Name:     "production",
})
if err != nil {
    panic(err)
}
```

## Kubernetes

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: acor-config
data:
  REDIS_ADDR: "redis-service:6379"
  ACOR_COLLECTION: "production"
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: acor-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: acor-app
  template:
    metadata:
      labels:
        app: acor-app
    spec:
      containers:
        - name: app
          image: myapp:latest
          envFrom:
            - configMapRef:
                name: acor-config
```

## Docker Compose

```yaml
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"

  app:
    build: .
    depends_on:
      - redis
    environment:
      - REDIS_ADDR=redis:6379
```

## Health endpoints

> `server/health` belongs to the experimental `acor/server` module, versioned separately
> and not covered by the core compatibility promise. See [Server](../../server/).

`health.RegisterHTTPHandlers(mux, checker)` registers two Kubernetes-compatible routes:

| Route | Meaning |
| ----- | ------- |
| `/healthz` | Liveness — `200 OK` while the process is up. Put nothing about Redis here |
| `/readyz` | Readiness — runs every registered `Checker`, `503` if any is unhealthy |

Keep Redis reachability out of liveness: a Redis outage would fail every replica's
liveness probe at once, restart all of them, and repair nothing. It is a *take me out of
the load balancer* signal.

The complete `main` — checker with its own deadline, mux composition, graceful shutdown —
is [Running a Server](../../server/running/), which also explains why a readiness check
built on `Info()` costs what the dictionary costs.

## Checklist

1. Use the V2 schema for new collections (the default).
2. Set timeouts and pool size from measurements, not from guesses.
3. Monitor Redis memory and the [cache counters](../monitoring/).
4. Call `Close()` on shutdown.
