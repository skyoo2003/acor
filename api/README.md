# api/

Generated records of the frozen public surface of `pkg/acor`. **Neither file is
hand-edited.**

| File | What it is | Written by |
|------|-----------|------------|
| `v1.txt` | The covered surface, one symbol per line — functions, methods, struct fields, interface methods, and struct tags | `tools/apisnap` |
| `v1-audit.txt` | One verdict per line of `v1.txt`, as `entry ⇥ verdict ⇥ note`, recording whether that entry's godoc was checked against the code | A human, gated by `tools/apisnap` |

Regenerate and check both with:

```
make api-check
```

That rewrites `v1.txt` in place and fails on the diff, so regenerating *is*
running the check — there is no separate `-update` target. It also fails if an
entry of `v1.txt` has no line in `v1-audit.txt`, or carries a verdict outside
`ok | fixed | risk | unaudited`, or claims a non-`unaudited` verdict without
citing a `file:line`.

A deleted line in `v1.txt` is a breaking change. The policy these files enforce —
what `v1` covers, what it does not, and what counts as breaking — is
[`docs/content/reference/compatibility.md`](../docs/content/reference/compatibility.md).
That page is the rule; these files are the evidence.

## What does not belong here

Protocol definitions live with the module that owns them, not in a shared tree.
The gRPC service is defined in [`server/proto/`](../server/proto/) because
`server/` is a separate Go module and `make proto` is rooted there; moving it
here would split one module's sources across two trees.

The core library speaks no wire protocol — it is a Go API backed by Redis, with
no HTTP or JSON interface of its own — so no OpenAPI spec or JSON schema
describes it. If one ever describes the server, it belongs under `server/`
alongside the `.proto` it documents.
