---
title: "HTTP API"
weight: 2
---

# HTTP API

`server.NewHTTPHandler(service)` returns an `http.Handler` serving nine routes. Every
response the handler itself produces is JSON with `Content-Type: application/json`; the
exceptions are the two `ServeMux`-level responses noted under
[Not every response is JSON](#not-every-response-is-json).

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
| `POST` | `/v1/suggest-index` | `{"input":"..."}` | `{"matches":{"kw":[0]}}` — always `[0]`, see below |
| `GET` | `/v1/info` | — | `{"keywords":3,"nodes":7}` |
| `POST` | `/v1/flush` | — | `{"status":"ok"}` |

`count` is how many keywords the operation actually changed, so a second `add` of the same
keyword answers `{"count":0}`.

### Offsets are rune offsets, and the two `*-index` routes do not mean the same thing

`/v1/find-index` returns, per keyword, the positions where it matched. Those positions are
counted in **runes, not bytes**:

```sh
curl -sX POST localhost:8080/v1/find-index -d '{"input":"한글 레디스 and redis"}'
# {"matches":{"redis":[11],"레디스":[3]}}
```

`레디스` starts at rune 3 but byte 7; `redis` starts at rune 11 but byte 21. The response
gives 3 and 11. A client that slices the original string by these numbers must count
runes — Go `[]rune`, Python `str` indices, Rust `chars()`. JavaScript needs care in both
directions, because its string indices are UTF-16 code units: `[...s]` gives code points,
and anything outside the BMP counts as two units under `.slice()`.

`/v1/suggest-index` looks like the same shape but is not. `Suggest` matches the input as a
*prefix* of each keyword, so every match starts at the beginning by construction and the
implementation assigns exactly `[0]` to every suggestion. It never reports a second
position:

```sh
curl -sX POST localhost:8080/v1/suggest-index -d '{"input":"red"}'
# {"matches":{"redis":[0]}}
```

Treat it as "these keywords start with your input", not as a position list.

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
| `413` | The **first** JSON value in the body exceeds 1 MiB | `{"error":"request body must not be larger than 1048576 bytes"}` |
| `500` | Any error from the underlying collection | `{"error":"<the error's own text>"}` |
| `404` | No such path | **`text/plain`**, body `404 page not found` |
| `301` | The path needs canonicalizing (`/v1//info`) | **`text/html`**, Go's `Moved Permanently` page |

Three of these deserve more than a table row.

### `413` is not guaranteed for every oversized body

The size cap is applied by `http.MaxBytesReader`, but the handler only translates
`*http.MaxBytesError` into a `413` on the **first** decode. It then does a second decode to
reject trailing content, and that branch reports only the generic single-value error. So
whether an oversized body is a `413` or a `400` depends on *where* it crosses the line:

| Body | Status |
| ---- | ------ |
| A single JSON value larger than 1 MiB | `413` |
| A small JSON value followed by padding that pushes the total past 1 MiB | `400 request body must contain only a single JSON value` |

Both are rejected and neither reaches the collection, so this is a reporting inconsistency
rather than a hole in the cap. Do not write a client that branches on `413` to detect
"too large".

Tracked as [#226](https://github.com/skyoo2003/acor/issues/226); this section goes away when
both branches report `413`.

### Every collection error is a `500`

There is no error taxonomy. The handler passes any non-nil error from the collection
straight to a `500`, so a client mistake and a Redis outage are indistinguishable by status
code. Writing to a V1 collection — which the core rejects with `ErrV1ReadOnly`, a caller
error — comes back as `500 {"error":"V1 collections are read-only; migrate with MigrateV1ToV2"}`, not as a `4xx`.

If your client needs to tell "retry this" from "fix your request", it has to match on the
message text, and message text is not part of any promise. Treat `5xx` here as
*something went wrong*, not as *the server is unhealthy*.

For the latter, use `/readyz` — but note that **`/readyz` is not part of this handler**.
`NewHTTPHandler` and `NewHTTPServer` register only the routes in the table above, so
requesting `/readyz` from either gets the `404`. The route exists only if you mount
`health.RegisterHTTPHandlers` on an outer mux yourself, as
[Running a Server](../running/) does.

### Not every response is JSON

Two cases are answered by Go's `http.ServeMux` before ACOR's handler sees them, and neither
is JSON:

- **`404 page not found`** — `text/plain`, for a path that matches nothing.
- **`301 Moved Permanently`** — `text/html`, for a path that needs canonicalizing.
  `/v1//info`, `/v1/./info`, and `/v1/../v1/info` all redirect to `/v1/info`.

A client that unconditionally parses responses as JSON breaks on both. The redirect is the
worse of the two: a client that follows redirects automatically may downgrade the `POST`
to a `GET` and then receive a `405`, turning a doubled slash into a confusing method error.
Normalize paths before sending.

## Disconnecting does not cancel the work

**A client timeout or a dropped connection ends the request, not the Redis operation behind
it.** Each handler passes `r.Context()` into its adapter and the adapter discards it
(`server/server.go:96` and its siblings take `_ context.Context`); `Service` declares no
context on any method, and `(*AhoCorasick).Add` runs against the collection's own
long-lived context rather than the caller's.

So a client that gives up on `/v1/add`, `/v1/remove`, or `/v1/flush` has **not** prevented
the write. It may already have landed, or land shortly after. `/v1/flush` deletes every key
in the collection, and hanging up does not call it off.

- Do not read a timeout as "it did not happen". Re-read with `/v1/info` or `/v1/find`
  before retrying anything that mutates.
- Retrying on timeout can double-apply. `add` and `remove` are idempotent by keyword, so a
  repeat is harmless; a retried `flush` is a second flush.
- A short client timeout sheds no server load. The Redis work continues at full cost after
  the client is gone.

The [gRPC API](../grpc-api/) behaves identically — same `Service` interface, same discard.

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
