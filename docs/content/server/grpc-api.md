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

## Errors

Every error from the collection becomes `codes.Internal` with the error's own text as the
message. This is the same flattening the HTTP API does with its blanket `500`: a caller
mistake and a Redis outage arrive as the same code, and telling them apart means matching on
message text, which is not part of any promise.

No RPC returns `InvalidArgument`, `NotFound`, or `FailedPrecondition` — a write to a V1
read-only collection is `Internal`, not `FailedPrecondition`.

## Generating a client

There is no `buf.yaml` or `protoc` configuration in this repository. Two options:

- **Go clients** import the generated package directly and skip codegen entirely:

  ```go
  import acorv1 "github.com/skyoo2003/acor/server/proto/acor/v1"

  client := acorv1.NewAcorClient(conn)
  ```

- **Other languages** run their own `protoc`/`buf` against `acor.proto`. It has no imports
  beyond proto3 itself, so a bare `protoc --<lang>_out` on the single file is enough.

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

- **Status is polled, not computed per request**, every 5 seconds. An HTTP `/readyz` probe
  runs your checkers on each request; a gRPC health probe reads the last poll. Expect up to
  five seconds of staleness, and do not set a probe period shorter than that expecting
  finer resolution.
- **Both the overall server (`""`) and `acor.server.v1.Acor` are tracked**, and they carry
  the same status.
- **A nil checker reports `SERVING`** — an unconfigured health service is indistinguishable
  from a healthy one.
- **The `ctx` you pass to `NewGRPCServerWithObservability` bounds the poller.** Cancel it
  and the server is marked `NOT_SERVING`, which is what lets a load balancer drain
  connections before `GracefulStop` finishes.

## Navigation

← [HTTP API](../http-api/) | [Extending](../../extending/) →
