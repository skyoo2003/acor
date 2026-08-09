---
title: "HTTP API"
weight: 2
---

# HTTP API

`server.NewHTTPHandler(service)` returns an `http.Handler` serving nine routes. Every
response is JSON with `Content-Type: application/json`, with exactly one exception noted
under [Errors](#errors).

> **The `acor/server` module is experimental.** These paths and shapes are **not covered by
> the core module's compatibility promise** and can change in any release. See the
> [section overview](../) for what that means for your `go.mod`.

See [Running a Server](../running/) for the `main` that mounts this handler.

## Endpoints

| Method | Path | Request | Success response |
| ------ | ---- | ------- | ---------------- |
| `GET` | `/healthz` | — | `{"status":"ok"}` |
| `POST` | `/v1/add` | `{"keyword":"..."}` | `{"count":1}` |
| `POST` | `/v1/remove` | `{"keyword":"..."}` | `{"count":1}` |
| `POST` | `/v1/find` | `{"input":"..."}` | `{"matches":["..."]}` |
| `POST` | `/v1/find-index` | `{"input":"..."}` | `{"matches":{"kw":[0,12]}}` |
| `POST` | `/v1/suggest` | `{"input":"..."}` | `{"matches":["..."]}` |
| `POST` | `/v1/suggest-index` | `{"input":"..."}` | `{"matches":{"kw":[0,12]}}` |
| `GET` | `/v1/info` | — | `{"keywords":3,"nodes":7}` |
| `POST` | `/v1/flush` | — | `{"status":"ok"}` |

`count` is how many keywords the operation actually changed, so a second `add` of the same
keyword answers `{"count":0}`. In `*-index`, each value is the list of **start offsets** at
which that keyword matched.

`/v1/flush` takes no request body and does not read one if you send it. It deletes every
key in the collection.

The method column is enforced, not advisory: `/healthz` and `/v1/info` are `GET`-only and
everything else is `POST`-only. Any other method gets `405`.

## Errors

Every failure returns `{"error":"<message>"}` with `Content-Type: application/json`.

| Status | When | Body |
| ------ | ---- | ---- |
| `400` | The body is not valid JSON | `{"error":"unexpected EOF"}` — the message comes from `encoding/json` and is not a stable string |
| `400` | The body holds more than one JSON value | `{"error":"request body must contain only a single JSON value"}` |
| `405` | Wrong method for the path | `{"error":"method not allowed"}` |
| `413` | Body larger than 1 MiB | `{"error":"request body must not be larger than 1048576 bytes"}` |
| `500` | Any error from the underlying collection | `{"error":"<the error's own text>"}` |
| `404` | No such path | **`text/plain`**, body `404 page not found` |

Two of these deserve more than a table row.

### Every collection error is a `500`

There is no error taxonomy. The handler passes any non-nil error from the collection
straight to a `500`, so a client mistake and a Redis outage are indistinguishable by status
code. Writing to a V1 collection — which the core rejects with `ErrV1ReadOnly`, a caller
error — comes back as `500 {"error":"v1 collection is read-only"}`, not as a `4xx`.

If your client needs to tell "retry this" from "fix your request", it has to match on the
message text, and message text is not part of any promise. Treat `5xx` here as
*something went wrong*, not as *the server is unhealthy*, and use `/readyz` for the latter.

### The 404 is the only non-JSON response

An unmatched path is answered by Go's `http.ServeMux` default, not by ACOR, so it is
`text/plain` and its body is `404 page not found`. A client that unconditionally parses
responses as JSON will fail on a typo'd path in a way it will not fail on any other error.

## What the server does not check

- **Request `Content-Type` is ignored.** Any value is accepted as long as the body parses
  as JSON.
- **Unknown JSON fields are ignored.** `{"keyword":"x","bogus":1}` succeeds.
- **Missing fields become the zero value.** `POST /v1/add` with `{}` is an `Add("")`; the
  server does not reject it, and what happens next is whatever the collection does with an
  empty keyword.
- **There is no authentication or authorization.** `/v1/flush` is reachable by anyone who
  can reach the port.

## Example

```sh
curl -sX POST localhost:8080/v1/add        -d '{"keyword":"redis"}'
# {"count":1}

curl -sX POST localhost:8080/v1/find       -d '{"input":"redis-backed matching"}'
# {"matches":["redis"]}

curl -sX POST localhost:8080/v1/find-index -d '{"input":"redis and redis"}'
# {"matches":{"redis":[0,10]}}

curl -s     localhost:8080/v1/info
# {"keywords":1,"nodes":6}
```

## Navigation

← [Running a Server](../running/) | [gRPC API](../grpc-api/) →
