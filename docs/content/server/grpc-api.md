---
title: "gRPC API"
weight: 3
---

# gRPC API

The service is `acor.server.v1.Acor`, defined in
[`server/proto/acor/v1/acor.proto`](https://github.com/skyoo2003/acor/blob/main/server/proto/acor/v1/acor.proto).
It mirrors the [HTTP API](../http-api/) one for one. The `main` that serves it is
[Running a Server](../running/).

> The `acor/server` module is [experimental and separately versioned](../) — this service
> definition can change in any release.

## Methods

| RPC | Request | Response |
| --- | ------- | -------- |
| `Add` | `KeywordRequest{keyword}` | `CountResponse{count}` |
| `Remove` | `KeywordRequest{keyword}` | `CountResponse{count}` |
| `Find` | `InputRequest{input}` | `MatchesResponse{matches}` |
| `FindIndex` | `InputRequest{input}` | `MatchIndexesResponse{matches}` |
| `Suggest` | `InputRequest{input}` | `MatchesResponse{matches}` |
| `SuggestIndex` | `InputRequest{input}` | `MatchIndexesResponse{matches}` |
| `Info` | `EmptyRequest` | `InfoResponse{keywords, nodes}` |
| `Flush` | `EmptyRequest` | `StatusResponse{status}` |

All eight are unary; full method names are `/acor.server.v1.Acor/<RPC>`.

Two shapes differ from HTTP:

- **Counts are `int64`.** `CountResponse.count`, `InfoResponse.keywords`, and
  `InfoResponse.nodes` are `int64` on the wire, where the Go and JSON APIs use `int`.
- **Match offsets are wrapped.** proto3 maps cannot hold a repeated value, so
  `MatchIndexesResponse.matches` is `map<string, Positions>`:

  ```proto
  message Positions {
    repeated int64 positions = 1;
  }
  ```

  A Go client reads `resp.GetMatches()["redis"].GetPositions()`.

Positions are rune offsets, and `SuggestIndex` always answers `[0]` because it matches the
input as a prefix. Both behaviors come from the collection rather than the transport, so
they are identical on both surfaces — [the HTTP page works through them with
examples](../http-api/).

## Errors

Every error from the collection becomes `codes.Internal` with the error's own text as the
message — the same flattening as the HTTP blanket `500`. No RPC returns `InvalidArgument`,
`NotFound`, or `FailedPrecondition`; a write to a read-only V1 collection is `Internal`.
Telling a caller mistake from a Redis outage means matching on message text, which is not
part of any promise.

## Deadlines do not cancel the work

**A client deadline or disconnect ends the RPC, not the Redis operation behind it.**
`Service` declares no context on any method (`server/grpc.go` calls
`s.service.Add(req.GetKeyword())`), and `(*AhoCorasick).Add` runs against the collection's
own long-lived context. The request context is accepted by each adapter and discarded.

So a client that gives up on `Add`, `Remove`, or `Flush` and sees `DeadlineExceeded` has
**not** prevented the write:

- Do not treat a timeout as "it did not happen". Re-read with `Info` or `Find` before
  retrying anything that mutates.
- Retries can double-apply. `Add` and `Remove` are idempotent by keyword; a retried `Flush`
  is a second flush.
- A short deadline sheds no server load.

The [HTTP API](../http-api/) behaves identically.

## Constructors

```go
server.NewGRPCServer(service, opts...)                            // bare
server.NewGRPCServerWithObservability(ctx, service, obs, opts...) // + observability
```

Both accept any `grpc.ServerOption`, including `grpc.Creds` for TLS, and both leave
`Serve`/`Stop` to you. `Observability` bundles four pillars, and **any field may be nil to
skip it**:

| Field | Wires in | Notes |
| ----- | -------- | ----- |
| `Tracer` | `otelgrpc` stats handler | Configure with `tracing.NewTracer` |
| `Metrics` | `grpc_server_*` Prometheus interceptor | gRPC has no `/metrics` route — expose `promhttp.Handler()` on a separate listener |
| `Logger` | zerolog unary interceptor | JSON request logs |
| `Health` | the standard `grpc.health.v1` service | See below |

Metric names and tracing configuration are in
[Operations → Monitoring](../../operations/monitoring/).

## Health checking

Passing `Health` registers the standard `grpc.health.v1.Health` service, so
`grpc_health_probe` and Kubernetes gRPC probes work with no extra code. Four things to know
before setting a probe interval:

- **Status is polled, not computed per request.** A gRPC probe reads whatever the last poll
  left behind, so probing more often than every 5 seconds buys no resolution.
- **The 5-second tick bounds neither staleness nor load.** The poller calls
  `checker.Check()` inline on one long-lived `time.Ticker`, which keeps ticking while a
  check is blocked and buffers one tick — so a check that overruns the interval is followed
  *immediately* by the next. A checker taking 30 seconds makes the status at least 30
  seconds stale **and** polls back-to-back for as long as it stays slow, hitting Redis
  hardest when Redis is already the slow part. A checker that hangs outright cannot act on
  context cancellation either, so shutdown stops marking the server `NOT_SERVING`. Give
  every checker its own deadline — [Running a Server](../running/) uses 2 seconds.
- **A nil `Health` field means no health service at all**, so a probe fails with
  `UNIMPLEMENTED`, not `SERVING` (`server/grpc.go:77`). For a service that always answers
  `SERVING`, pass an empty `health.NewChecker()`; the nil-*checker*-means-`SERVING` rule
  lives inside `RegisterGRPCHealthServer` and is reachable only by calling that yourself.
- **The `ctx` passed to `NewGRPCServerWithObservability` bounds the poller.** Cancelling it
  marks the server `NOT_SERVING`, which is what lets a load balancer drain connections
  before `GracefulStop` finishes.

Both the overall server (`""`) and `acor.server.v1.Acor` are tracked, and they carry the
same status.

## Generating a client

There is no `buf.yaml` or `protoc` configuration in this repository.

- **Go clients** import the generated package and skip codegen entirely:

  ```go
  import acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"

  client := acorv1.NewAcorClient(conn)
  ```

- **Other languages** run their own `protoc`/`buf` against `acor.proto`. It has no imports
  beyond proto3, so the single file is the whole input — but `--<lang>_out` alone generates
  only the **message** types. The service stub needs that language's gRPC plugin as a
  second output flag:

  ```sh
  # Python: --python_out gives acor_pb2.py only; the stub needs grpcio-tools.
  python -m grpc_tools.protoc -I. --python_out=. --grpc_python_out=. acor.proto

  # C++/Java/etc. take the plugin the same way:
  protoc -I. --cpp_out=. --grpc_out=. --plugin=protoc-gen-grpc=$(which grpc_cpp_plugin) acor.proto
  ```
