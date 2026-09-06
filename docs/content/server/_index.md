---
title: "Server"
weight: 5
---

# Server

`acor/server` turns a keyword collection into a service: an HTTP/JSON API, a gRPC API, and
the middleware that makes either observable.

> **The `acor/server` module is experimental.** It lives in the separate
> `github.com/skyoo2003/acor/server` module, which publishes no version tags of its own and
> is **not covered by the core module's compatibility promise** — its API can change in any
> release. The core library (`pkg/acor`) is unaffected.
>
> `go get github.com/skyoo2003/acor/server` resolves a pseudo-version from `main`. Pin the
> core module explicitly in the same `go.mod`: Go ignores a dependency's own `replace`
> directive, so without a pin you get whichever core version the server module's `require`
> names.

## There is no server binary

No `acor-server` to install, no image to pull. `acor/server` is a library — it hands you an
`http.Handler`, a `*grpc.Server`, and middleware, and **you** write the `main` that wires
them to a collection and listens.

That follows from the module being experimental: a published binary is a contract, and this
module does not offer one yet. What it offers instead is that the wiring is about forty
lines, and [Running a Server](running/) is those forty lines, complete and copy-pasteable.

## Sections

- [Running a Server](running/) — the `main` for each protocol, with readiness checks and clean shutdown
- [HTTP API](http-api/) — nine JSON endpoints, their shapes, and every error they return
- [gRPC API](grpc-api/) — the `acor.server.v1.Acor` service and the observability constructors

Metrics, structured logging, and tracing are configured the same way whichever protocol you
serve, so they live in [Operations → Monitoring](../operations/monitoring/).
