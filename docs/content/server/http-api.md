---
title: "HTTP API"
weight: 2
---

# HTTP API

`server.NewHTTPHandler(service)` returns an `http.Handler` serving nine routes. Every
response the handler itself produces is JSON; the two exceptions come from `ServeMux`
below. The `main` that mounts it is [Running a Server](../running/).

> The `acor/server` module is [experimental and separately versioned](../) — these paths
> and shapes can change in any release.

## Endpoints

| Method | Path | Request | Success response |
| ------ | ---- | ------- | ---------------- |
| `GET` | `/healthz` | — | `{"status":"ok"}` |
| `POST` | `/v1/add` | `{"keyword":"..."}` | `{"count":1}` |
| `POST` | `/v1/remove` | `{"keyword":"..."}` | `{"count":1}` |
| `POST` | `/v1/find` | `{"input":"..."}` | `{"matches":["..."]}` |
| `POST` | `/v1/find-index` | `{"input":"..."}` | `{"matches":{"kw":[0,12]}}` |
| `POST` | `/v1/suggest` | `{"input":"..."}` | `{"matches":["..."]}` |
| `POST` | `/v1/suggest-index` | `{"input":"..."}` | `{"matches":{"kw":[0]}}` — always `[0]` |
| `GET` | `/v1/info` | — | `{"keywords":3,"nodes":7}` |
| `POST` | `/v1/flush` | — | `{"status":"ok"}` |

`count` is how many keywords the operation actually changed, so a second `add` of the same
keyword answers `{"count":0}`. `/v1/flush` takes no body, does not read one if you send it,
and deletes every key in the collection. The method column is enforced: anything else is
`405`.

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

### The two `*-index` routes do not mean the same thing

`/v1/find-index` returns, per keyword, the positions where it matched — counted in
**runes, not bytes**:

```sh
curl -sX POST localhost:8080/v1/find-index -d '{"input":"한글 레디스 and redis"}'
# {"matches":{"redis":[11],"레디스":[3]}}
```

`레디스` starts at rune 3 but byte 7; `redis` at rune 11 but byte 21. A client slicing the
original string by these numbers must count runes — Go `[]rune`, Python `str` indices,
Rust `chars()`. JavaScript needs care in both directions, since its string indices are
UTF-16 code units: `[...s]` gives code points, and anything outside the BMP counts as two
units under `.slice()`.

`/v1/suggest-index` looks like the same shape but is not. `Suggest` matches the input as a
*prefix*, so every match starts at the beginning by construction and the implementation
assigns exactly `[0]` to every suggestion. Read it as "these keywords start with your
input", not as a position list.

## Errors

Every failure the handler produces is `{"error":"<message>"}` with
`Content-Type: application/json`.

| Status | When | Body |
| ------ | ---- | ---- |
| `400` | Body is not valid JSON | `{"error":"unexpected EOF"}` — from `encoding/json`, not a stable string |
| `400` | Body holds more than one JSON value | `{"error":"request body must contain only a single JSON value"}` |
| `405` | Wrong method for the path | `{"error":"method not allowed"}` |
| `413` | Reading the body reaches the 1 MiB cap | `{"error":"request body must not be larger than 1048576 bytes"}` |
| `500` | Any error from the underlying collection | `{"error":"<the error's own text>"}` |
| `404` | No such path | **`text/plain`**, `404 page not found` |
| `301` | Path needs canonicalizing (`/v1//info`) | **`text/html`**, Go's `Moved Permanently` page |

The body is read as it is decoded, so whichever fault surfaces first is the one you get: an
oversized body that is *also* malformed comes back as the `400`, because the decoder
reaches the bad byte before the reader reaches the cap.

**Every collection error is a `500`.** There is no error taxonomy — a client mistake and a
Redis outage are indistinguishable by status code. Writing to a V1 collection, which the
core rejects with `ErrV1ReadOnly`, arrives as
`500 {"error":"V1 collections are read-only; migrate with MigrateV1ToV2"}`, not a `4xx`.
Telling "retry this" from "fix your request" means matching on message text, and message
text is not part of any promise.

**`/readyz` is not part of this handler.** `NewHTTPHandler` and `NewHTTPServer` register
only the routes above, so `/readyz` returns the `404`. It exists only if you mount
`health.RegisterHTTPHandlers` on an outer mux, as [Running a Server](../running/) does.

**Not every response is JSON.** The `404` and the `301` are answered by Go's
`http.ServeMux` before ACOR's handler sees them. `/v1//info`, `/v1/./info`, and
`/v1/../v1/info` all redirect to `/v1/info` — and a client that follows redirects
automatically may downgrade the `POST` to a `GET` and then receive a `405`, turning a
doubled slash into a confusing method error. Normalize paths before sending.

## Disconnecting does not cancel the work

**A client timeout or dropped connection ends the request, not the Redis operation behind
it.** Each handler passes `r.Context()` into its adapter and the adapter discards it
(`server/server.go:96` and siblings take `_ context.Context`); `Service` declares no
context on any method, and `(*AhoCorasick).Add` runs against the collection's own
long-lived context.

So a client that gives up on `/v1/add`, `/v1/remove`, or `/v1/flush` has **not** prevented
the write:

- Do not read a timeout as "it did not happen". Re-read with `/v1/info` or `/v1/find`
  before retrying anything that mutates.
- Retrying can double-apply. `add` and `remove` are idempotent by keyword; a retried
  `flush` is a second flush.
- A short client timeout sheds no server load — the Redis work continues at full cost.

The [gRPC API](../grpc-api/) behaves identically: same `Service` interface, same discard.

## What the server does not check

- **Request `Content-Type` is ignored** — any value is accepted if the body parses as JSON.
- **Unknown JSON fields are ignored.** `{"keyword":"x","bogus":1}` succeeds.
- **Missing fields become the zero value.** `POST /v1/add` with `{}` is an `Add("")`.
- **There is no authentication.** `/v1/flush` is reachable by anyone who can reach the port.
