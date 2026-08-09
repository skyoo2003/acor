---
title: "Running a Server"
weight: 1
---

# Running a Server

ACOR ships no server binary. `acor/server` gives you an `http.Handler` and a
`*grpc.Server`; the `main` that wires them to a collection and listens is yours to write.
This page is that `main`, in full, for each protocol.

> **The `acor/server` module is experimental.** It publishes no version tags of its own and
> is **not covered by the core module's compatibility promise**. See the
> [section overview](../) for what that means for your `go.mod`.

## Dependencies

```sh
go get github.com/skyoo2003/acor/server
go get github.com/skyoo2003/acor/pkg/acor@latest
```

Both lines matter. `acor/server` resolves to a pseudo-version from `main`, and it carries a
`require` on the core module that Go will not override from the dependency's own `replace`
directive — so name the core version yourself, in your own `go.mod`.

## HTTP

The collection is the service. Every method `server.Service` requires — `Add`, `Remove`,
`Find`, `FindIndex`, `Suggest`, `SuggestIndex`, `Flush`, `Info` — is already a method on
`*acor.AhoCorasick`, so the collection satisfies the interface with no adapter.

<!-- doccheck:server -->
```go
package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/skyoo2003/acor/pkg/acor"
	"github.com/skyoo2003/acor/server"
	"github.com/skyoo2003/acor/server/health"
)

// redisChecker reports whether the collection can still reach Redis.
//
// Info() is the only exported call that proves the Redis path works, but it is
// not free: on V2 it HGETALLs the trie hash and unmarshals the whole keyword
// and prefix arrays just to count them, so its cost grows with the dictionary.
// See "Readiness costs what Info() costs" below before pointing a probe at it.
//
// The timeout is not tidiness. Check() runs inline in both probe paths, so a
// checker that blocks blocks the prober.
type redisChecker struct{ ac *acor.AhoCorasick }

func (c redisChecker) Check() health.CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.ac.InfoContext(ctx); err != nil {
		return health.CheckResult{Status: health.StatusUnhealthy, Details: err.Error()}
	}
	return health.CheckResult{Status: health.StatusHealthy}
}

func main() {
	ac, err := acor.Create(&acor.AhoCorasickArgs{
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		Name:     "production",
	})
	if err != nil {
		log.Fatalf("create collection: %v", err)
	}
	defer ac.Close()

	checker := health.NewChecker()
	checker.Register("redis", redisChecker{ac})

	mux := http.NewServeMux()
	health.RegisterHTTPHandlers(mux, checker)  // /healthz and /readyz
	mux.Handle("/", server.NewHTTPHandler(ac)) // /v1/*

	srv := &http.Server{
		Addr:    ":8080",
		Handler: mux,
		// ReadHeaderTimeout alone leaves the body unbounded in time: a client
		// that sends good headers and then trickles bytes holds a goroutine
		// indefinitely. The 1 MiB cap bounds size, not duration.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serve: %v", err)
		}
	}()
	log.Println("listening on", srv.Addr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
```

### Why the mux is built by hand

`server.NewHTTPServer(addr, service)` is a one-liner that does most of this, and it is the
right choice if you do not need readiness checks. What it cannot give you is `/readyz`:
`NewHTTPHandler` builds its own private `http.ServeMux` internally and returns it as an
`http.Handler`, so there is no mux for you to register anything else on.

Composing them on an outer mux, as above, works and does not panic — the two `/healthz`
registrations are on different muxes and never meet. On the outer mux, Go routes `/healthz`
to the `health` package's handler because an exact pattern outranks the `/` catch-all. The
practical effect:

| Path | Served by | Body |
| ---- | --------- | ---- |
| `/healthz` | `server/health` — shadows the API's built-in one | `{"status":"ok"}` |
| `/readyz` | `server/health` | `{"status":"healthy","checks":{...}}` |
| `/v1/*` | `server.NewHTTPHandler` | see [HTTP API](../http-api/) |

The two `/healthz` implementations return the same body for a `GET`, so the shadowing costs
you nothing. They differ only in how they reject a non-`GET`: `server/health` replies in
`text/plain` via `http.Error`, the API's own replies in JSON.

## gRPC

<!-- doccheck:server -->
```go
package main

import (
	"context"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/skyoo2003/acor/pkg/acor"
	"github.com/skyoo2003/acor/server"
	"github.com/skyoo2003/acor/server/health"
	"github.com/skyoo2003/acor/server/logging"
	"github.com/skyoo2003/acor/server/metrics"
)

// The deadline matters more here than on the HTTP side: the gRPC health
// poller calls Check() inline on its ticker, so a checker that blocks stalls
// every later poll and the poller's own response to cancellation.
type redisChecker struct{ ac *acor.AhoCorasick }

func (c redisChecker) Check() health.CheckResult {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := c.ac.InfoContext(ctx); err != nil {
		return health.CheckResult{Status: health.StatusUnhealthy, Details: err.Error()}
	}
	return health.CheckResult{Status: health.StatusHealthy}
}

func main() {
	// This ctx bounds the background health-status poller as well as shutdown:
	// cancelling it stops that goroutine and marks the server NOT_SERVING.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ac, err := acor.Create(&acor.AhoCorasickArgs{
		Addr:     os.Getenv("REDIS_ADDR"),
		Password: os.Getenv("REDIS_PASSWORD"),
		Name:     "production",
	})
	if err != nil {
		log.Fatalf("create collection: %v", err)
	}
	defer ac.Close()

	checker := health.NewChecker()
	checker.Register("redis", redisChecker{ac})

	srv := server.NewGRPCServerWithObservability(ctx, ac, &server.Observability{
		Metrics: metrics.NewRegistry(nil),
		Logger:  logging.NewLogger(os.Stdout, "info"),
		Health:  checker,
		// Tracer is nil here, which skips tracing. See Operations → Monitoring.
	})

	lis, err := net.Listen("tcp", ":9090")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}

	go func() {
		<-ctx.Done()
		// GracefulStop waits for every in-flight RPC with no deadline, and
		// grpc.health.v1.Watch is a stream that stays open: the health server's
		// Shutdown only pushes NOT_SERVING to watchers, it does not close them.
		// One connected watcher would otherwise block shutdown forever.
		stopped := make(chan struct{})
		go func() {
			srv.GracefulStop()
			close(stopped)
		}()
		select {
		case <-stopped:
		case <-time.After(15 * time.Second):
			srv.Stop()
		}
	}()

	log.Println("gRPC listening on", lis.Addr())
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
}
```

Every field of `Observability` is optional: a nil field skips that pillar, so you can start
with logging only and add the rest later. `server.NewGRPCServer(service, opts...)` is the
same server with no observability at all.

Prometheus metrics registered here are *collected* but not *exposed* — gRPC has no
`/metrics` endpoint. Serve `promhttp.Handler()` on a separate HTTP listener; see
[Operations → Monitoring](../../operations/monitoring/).

## Readiness costs what `Info()` costs

`Info()` is the only exported call that touches Redis and returns quickly *on a small
collection*, which is why it is the readiness check here. It is not a ping. On the default
V2 schema it runs `HGETALL` against the trie hash and JSON-unmarshals the complete keyword
and prefix arrays in order to return their two lengths, so its cost and its allocations
scale with the whole dictionary.

Two things multiply that:

- **`/readyz` runs the checkers on every request**, and it is unauthenticated.
- **The gRPC health poller runs them every 5 seconds**, whether or not anyone is probing.

On a large dictionary that is significant Redis traffic and garbage, generated hardest
exactly when the service is already struggling. If that describes your collection, make the
**readiness** check a direct `redis.Client.Ping` against the same address instead — that is
a `PING`, not a dictionary scan — and accept that it proves connectivity rather than
collection health.

Keep it out of liveness either way. `/healthz` answers "is this process alive", and nothing
about Redis belongs in that answer: a Redis outage would fail every replica's liveness probe
at once and have the orchestrator restart all of them, which cannot repair Redis and drops
whatever the processes were still serving. Redis reachability is a readiness signal —
take me out of the load balancer — not a restart signal.

The 2-second timeout is load-bearing either way. `HealthChecker.Check` calls every
registered checker inline, and the gRPC poller calls `Check` inline on its ticker, so one
checker that hangs stalls every later poll **and** the poller's response to context
cancellation. A checker without its own deadline turns a slow Redis into a stuck health
service.

## What you still have to decide

The examples above hard-code answers this page cannot make for you:

1. **Listen address.** `:8080` and `:9090` are placeholders.
2. **Redis credentials and topology.** `Addr` is standalone. Sentinel and Cluster use
   `Addrs`, and Sentinel also needs `MasterName` — see
   [Operations → Deployment](../../operations/deployment/).
3. **TLS.** Neither constructor configures it. For gRPC, pass `grpc.Creds(...)` as a
   `grpc.ServerOption`; for HTTP, use `ListenAndServeTLS` or terminate at your ingress.
4. **Authentication.** There is none. Both surfaces expose `/v1/flush` and `Flush`, which
   delete every key in the collection. Do not put either on a network you do not control.
5. **Which protocol to serve.** They are independent; run one, the other, or both on
   separate listeners.

## Navigation

← [Server](../) | [HTTP API](../http-api/) →
