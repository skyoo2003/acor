---
title: "gRPC API"
weight: 3
---

# gRPC API

The service is `acor.server.v1.Acor`, defined in
[`server/proto/acor/v1/acor.proto`](https://github.com/skyoo2003/acor/blob/main/server/proto/acor/v1/acor.proto).
It mirrors the [HTTP API](../http-api/) one for one.

> **The `acor/server` module is experimental.** This service definition is **not covered by
> the core module's compatibility promise** and can change in any release. See the
> [section overview](../) for what that means for your `go.mod`.

See [Running a Server](../running/) for the `main` that serves it.

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

All eight are unary. Full method names are `/acor.server.v1.Acor/<RPC>`.

### Two shapes differ from HTTP

**Counts are `int64`.** `CountResponse.count`, `InfoResponse.keywords`, and
`InfoResponse.nodes` are `int64` on the wire, where the Go API and the JSON API use `int`.

**Match offsets are wrapped.** proto3 maps cannot hold a repeated value, so
`MatchIndexesResponse.matches` is `map<string, Positions>` rather than the JSON API's
`map[string][]int`:

```proto
message Positions {
  repeated int64 positions = 1;
}

message MatchIndexesResponse {
  map<string, Positions> matches = 1;
}
```

A Go client reads `resp.GetMatches()["redis"].GetPositions()`.

**Positions are rune offsets, not byte offsets**, and `SuggestIndex` always answers `[0]`
because it matches the input as a prefix rather than searching for it. Both behaviors come
from the collection, not the transport, so they are identical on both surfaces —
[the HTTP page works through them with examples](../http-api/).

## Errors

Every error from the collection becomes `codes.Internal` with the error's own text as the
message. This is the same flattening the HTTP API does with its blanket `500`: a caller
mistake and a Redis outage arrive as the same code, and telling them apart means matching on
message text, which is not part of any promise.

No RPC returns `InvalidArgument`, `NotFound`, or `FailedPrecondition` — a write to a V1
read-only collection is `Internal`, not `FailedPrecondition`.

## Deadlines do not cancel the work

**A client deadline or a disconnect ends the RPC, not the Redis operation behind it.**
`Service` declares no context on any method (`server/grpc.go` calls
`s.service.Add(req.GetKeyword())`), and `(*AhoCorasick).Add` runs against the collection's
own long-lived context, not the caller's. The request context is accepted by each adapter
and discarded.

The consequence to plan for: a client that gives up on `Add`, `Remove`, or `Flush` and sees
`DeadlineExceeded` or `Canceled` has **not** prevented the write. It may already have
landed, or land shortly after. `Flush` deletes every key in the collection, and abandoning
the call does not call it off.

- Do not treat a timeout as "it did not happen". Re-read with `Info` or `Find` before
  retrying anything that mutates.
- Client-side retries on timeout can double-apply. `Add` and `Remove` are idempotent by
  keyword, so a repeat is harmless there; a retried `Flush` is a second flush.
- Setting a short deadline does not shed load on the server. The Redis work continues at
  full cost after the client is gone.

The [HTTP API](../http-api/) behaves identically — same `Service` interface, same discard.

## Generating a client

There is no `buf.yaml` or `protoc` configuration in this repository. Two options:

- **Go clients** import the generated package directly and skip codegen entirely:

  ```go
  import acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"

  client := acorv1.NewAcorClient(conn)
  ```

- **Other languages** run their own `protoc`/`buf` against `acor.proto`. It has no imports
  beyond proto3 itself, so the single file is the whole input — but `--<lang>_out` alone
  generates only the **message** types. The service stub needs that language's gRPC plugin
  as a second output flag:

  ```sh
  # Python: --python_out gives acor_pb2.py only; the stub needs grpcio-tools.
  python -m grpc_tools.protoc -I. --python_out=. --grpc_python_out=. acor.proto

  # C++/Java/etc. take the plugin the same way:
  protoc -I. --cpp_out=. --grpc_out=. --plugin=protoc-gen-grpc=$(which grpc_cpp_plugin) acor.proto
  ```

  Omit the gRPC flag and you get the eight request/response messages with nothing able to
  call the eight RPCs.

## Constructors

```go
server.NewGRPCServer(service, opts...)                            // bare
server.NewGRPCServerWithObservability(ctx, service, obs, opts...) // + observability
```

Both accept any `grpc.ServerOption`, including `grpc.Creds` for TLS, and both leave
`Serve`/`Stop` to you.

`Observability` bundles four pillars, and **any field may be nil to skip that one**:

| Field | Wires in | Notes |
| ----- | -------- | ----- |
| `Tracer` | `otelgrpc` stats handler | Configure with `tracing.NewTracer` |
| `Metrics` | `grpc_server_*` Prometheus interceptor | gRPC has no `/metrics` route — expose `promhttp.Handler()` on a separate HTTP listener |
| `Logger` | zerolog unary interceptor | JSON request logs |
| `Health` | the standard `grpc.health.v1` service | See below |

Metric names and tracing configuration live in
[Operations → Monitoring](../../operations/monitoring/); this page does not restate them.

## Health checking

Passing `Health` registers the standard `grpc.health.v1.Health` service, so
`grpc_health_probe` and Kubernetes gRPC probes work without extra code. Behavior worth
knowing before you set a probe interval:

- **Status is polled, not computed per request.** An HTTP `/readyz` probe runs your checkers
  on each request; a gRPC health probe reads whatever the last poll left behind. Probing
  more often than every 5 seconds buys no extra resolution.
- **The 5-second tick bounds neither staleness nor load.** The poller calls
  `checker.Check()` inline on a single long-lived `time.Ticker`. That ticker keeps ticking
  while a check is blocked, and its channel buffers one tick, so a check that overruns the
  interval is followed *immediately* by the next one rather than after a fresh 5-second
  gap. A checker that takes 30 seconds therefore makes the status at least 30 seconds stale
  **and** polls back-to-back for as long as it stays slow — the opposite of the breather
  the interval suggests, and it lands on Redis hardest when Redis is already the slow part.
  A checker that hangs outright is worse: the blocked goroutine cannot act on context
  cancellation either, so shutdown stops marking the server `NOT_SERVING`. Give every
  checker its own deadline; the examples in [Running a Server](../running/) use a 2-second
  `context.WithTimeout` for exactly this.
- **Both the overall server (`""`) and `acor.server.v1.Acor` are tracked**, and they carry
  the same status.
- **A nil `Health` field means no health service at all.** The constructor only registers
  `grpc.health.v1` when the field is set (`server/grpc.go:77`), so leaving it nil makes a
  probe fail with `UNIMPLEMENTED`, not `SERVING`. To get a health service that always
  answers `SERVING`, pass an empty `health.NewChecker()` rather than nil — the
  nil-*checker*-means-`SERVING` rule lives inside `RegisterGRPCHealthServer` and is only
  reachable by calling that function yourself.
- **The `ctx` you pass to `NewGRPCServerWithObservability` bounds the poller.** Cancel it
  and the server is marked `NOT_SERVING`, which is what lets a load balancer drain
  connections before `GracefulStop` finishes.

## Navigation

← [HTTP API](../http-api/) | [CLI](../../cli/) →
