---
title: "Versioned dictionaries (V3)"
description: "Large dictionary snapshots, atomic changes, and background search refresh."
---

V3 is an opt-in storage format in the existing Go module. Use `OpenVersioned`
and a new collection name. Existing `Create`, V1/V2 keys, and their error and
update contracts remain unchanged. V3 does not provide Suggest or a server API.

<!-- doccheck -->
```go
package main

import (
    "context"
    "log"

    "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
    ctx := context.Background()
    dictionary, err := acor.OpenVersioned(ctx, &acor.VersionedOptions{
        Redis: acor.AhoCorasickArgs{Addr: "localhost:6379", Name: "filter-v3"},
    })
    if err != nil { log.Fatal(err) }
    defer dictionary.Close()

    snapshot, err := dictionary.Snapshot(ctx)
    if err != nil { log.Fatal(err) }
    expected := snapshot.Version()
    snapshot.Close(ctx)

    result, err := dictionary.Replace(ctx, expected, []string{"한국", "hello"})
    if err != nil { log.Fatal(err) }
    if err := dictionary.WaitForVersion(ctx, result.Version); err != nil { log.Fatal(err) }
    matches, err := dictionary.Find(ctx, "HELLO 한국")
    if err != nil { log.Fatal(err) }
    log.Print(matches)
}
```

## API contracts

All I/O methods take a context. `Status`, `Snapshot.Version`, and `Snapshot.Count`
read local state. `Close` on the collection stops its background work; snapshot
`Close(ctx)` releases its lease. Initial loading fails the open operation.

`Snapshot.List(ctx, cursor, limit)` returns up to a positive limit, ordered by
bucket number then lexical keyword order. Pass an empty cursor to start and stop
when `NextCursor` is empty. Keep the same snapshot for a complete traversal:
a cursor is bound to the generation. `Snapshot.Diff(ctx, target)` returns sorted
Added, Removed, and Retained entries without writing. Diff materializes the
comparison in memory. Count is the normalized, deduplicated keyword count.

`Replace`, `Add`, `Remove`, `AddMany`, and `RemoveMany` require an expected Version.
Treat versions as opaque equality tokens, never ordered numbers or strings.
Concurrent writers using the same expected version cannot overwrite each other;
use `errors.Is(err, acor.ErrConcurrencyConflict)`. Batch changes are atomic.
Replace accepts an empty target. Reapplying an identical dictionary keeps the
version and does not publish invalidation. Keywords are trimmed and, unless
CaseSensitive was set at creation, lowercased. Blank keywords and invalid UTF-8
fail the entire input. Duplicate normalized inputs are removed. The stored case
policy must match every subsequent open.

A successful write means the Redis commit completed. The calling instance and
other instances may still search the previous engine. Use `WaitForVersion` to
wait for that commit or a later one; it honors cancellation. Status exposes the
observed active version, serving version, build start/duration, and recent error.
A failed refresh preserves the old serving engine. Polling defaults to 30 seconds,
with Pub/Sub accelerating discovery. A fixed 20 ms debounce window (RefreshDebounce) merges bursts before each
background build. Incoming events do not extend the window. The worker finishes
its current build, then loads the newest observed target. Every search, including a batch,
parallel scan, or stream, uses one engine. V3 reuses existing rune positions,
case folding, overlap and word-boundary semantics.

`Find`, `FindIndex`, `FindMatches`, `FindSet`, `Contains`, `FindStream`,
`FindBatch`, `FindParallel`, and `FindIndexParallel` are available with explicit
contexts. Streaming keeps state across reader boundaries. The existing parallel
options still apply; use AutoOverlap for dictionary-length boundary protection.

## Storage and failure handling

SHA-256's first 12 bits select one of 4,096 fixed buckets. Each bucket is sorted,
deduplicated, and split into JSON string-array chunks of at most 1 MiB including
JSON escaping and delimiters. An oversized individual keyword gets its own chunk.
Content-addressed immutable chunks, generation manifests, metadata, and the active
pointer use separate keys. All keys share a name-digest Redis hash tag, hence one
Cluster slot. This does not distribute one collection across multiple nodes.
Connection options reuse existing standalone, Sentinel, Cluster and Ring clients.
Only connection fields and Name from VersionedOptions.Redis are used.

Delta writes download and prepare affected buckets only; full replacements compare
all buckets and reuse unchanged ones. Local engine refresh reuses verified immutable keyword slices for unchanged
buckets and downloads changed buckets only. The previous cache remains usable
when a candidate fails. Full local engine rebuilding still requires memory for
the old engine, retained keyword slices, and the new engine. The default
preset is MemoryEfficient; Speed and Balanced are also selectable. No constant
memory or unmeasured latency guarantee is made.

The final Lua commit checks the expected pointer, preparation lease and maintenance
lock, stores a receipt, and changes the pointer. It does not parse the manifest or
iterate keywords. Prepared data is unreachable until committed. Storage durability
and failover behavior still depend on the Redis/Valkey persistence configuration.

On `ErrCommitUnknown`, retain the returned WriteResult.OperationID and use
`ResolveOperation(ctx, id)`. A found receipt is authoritative success. A missing
receipt is **not** proof that an in-flight request cannot commit later. Do not
blindly reapply ambiguous writes. Operation receipts and committed-version markers
are retained indefinitely, including no-op receipts, so they grow with write count.

## Leases and explicit pruning

Snapshots, builds and preparation writers use server-time leases (five minutes,
renewed every minute by default). Lease checks fail explicitly after expiration;
renewal cannot resurrect an expired handle. Close snapshots promptly. Existing
local searches need no Redis lease once their engine is built.

`Prune(ctx)` retains the active generation, generations prepared or committed
within 24 hours, and generations protected by valid reader leases. It deletes
unreferenced chunks and unretained manifests in batches of at most 64. It also
collects abandoned preparation data. Receipts are not deleted.

Prune acquires a monotonically increasing maintenance fence only when no valid
writer lease remains. New writers and snapshots temporarily return ErrMaintenance;
searches continue. Each deletion checks and extends lock ownership atomically.
The maintenance lock expires after 30 seconds if its process dies. An expired
pruner cannot delete after a successor takes ownership. Preparation itself is
fenced so an expired writer cannot create unregistered data during cleanup.
Large retention sets are enumerated in memory; there is no automatic collector.
R2 builds check cancellation throughout trie insertion, failure-link construction,
and table filling. Close cancels the active build; initialization honors its own
context. Cancellation discards a candidate without publishing it or leaving a
detached builder goroutine. Individual allocation, string length and sort
operations complete before the next checkpoint. Ordinary new commits do not
cancel an active build, avoiding starvation under continuous writes.

## V2 cutover

1. Use a **different** V3 name. Migrate V1 to V2 through the existing migration
   first. `CopyV2(ctx, sourceName, expected, nil)` reads the V2 version and keywords in
   one HMGET and replaces the V3 target. It reports source version, normalized
   count and SHA-256 checksum of the sorted JSON keyword array.
2. For rehearsal, copy and compare count, checksum and representative search
   results, including case-sensitive, Korean and overlapping matches. Set the
   V3 case policy to the source application's policy (V2 does not persist it).
3. Stop V2 writes for the final copy. Verify its source version, count, checksum
   and search results, then change application configuration to the new V3 name.
   There is no automatic dual write or simultaneous fleet engine switch.
4. After V3 writes begin, switching configuration back to V2 loses those changes.
   Export and apply the V3 changes back to V2 before rollback; a simple name change
   is insufficient. Keep V2 untouched until the migration has been accepted.

## CLI

Global connection/name flags precede `dictionary`; its flags follow the subcommand.
Input to diff/replace is one JSON string array on stdin. Empty copy-v2 sources
also require `--allow-empty`; the guard runs before replacing the target. Status and other results
are JSON. List is one page per invocation; concurrent generation changes reject a
previous page cursor. Library callers should retain a Snapshot for a long export.

```sh
acor -name filter-v3 dictionary status
acor -name filter-v3 dictionary list --limit 1000
printf '["한국","hello"]' | acor -name filter-v3 dictionary diff
printf '["한국","hello"]' | acor -name filter-v3 dictionary replace --expected-version TOKEN
printf '[]' | acor -name filter-v3 dictionary replace --expected-version TOKEN --allow-empty
acor -name filter-v3 dictionary copy-v2 --source filter-v2 --expected-version TOKEN
acor -name filter-v3 dictionary prune
```

R1 validation and measured resource costs are recorded in
[the reproducible V3 performance report](../versioned-performance/). R2 adds incremental download reuse, bounded coalescing, cancellable builds, and
more compact sparse-engine nodes. R3 adds bounded source-position search and
atomic masking/replacement; see [bounded text processing](../text-processing/) and
[the R2/R3 verification report](../r2-r3-performance/). R1 measurements remain
archived as the baseline; none of these reports are performance guarantees.
