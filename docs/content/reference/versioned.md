---
title: "Versioned dictionaries (V3)"
description: "Large dictionary snapshots, atomic changes, and background search refresh."
---

# Versioned dictionaries (V3)

V3 is an opt-in storage format in the same Go module. Open it with `OpenVersioned` and a
**new** collection name. `Create`, the V1/V2 keys, and their error and update contracts
are untouched. V3 has no `Suggest` and no mutation/search server API. The server module
provides a read-only status surface through `NewVersionedHTTPHandler`.

`VersionedCollection.Status()` is a local observation with no Redis I/O. `ActiveVersion` is the
last committed version observed from Redis; `ServingVersion` is the version currently used
for searches. During refresh, `Building` is true. `LastError`, `LastRefreshFailure`, and
`RefreshFailures` describe failed background refreshes, while `LastRefreshSuccess` records
the most recent installed engine. `ActiveLeases` counts snapshots and writers owned by the
process. These fields are diagnostics, not a fleet-wide health check; use `WaitForVersion`
when a write requires read-after-write behavior.

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
        ShardCount: 4, // New collections only; an existing layout is retained.
        ShardConcurrency: 2, // Bound concurrent shard download/build/search work.
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

Every I/O method takes a context. `Status`, `Snapshot.Version`, and `Snapshot.Count` read
local state only. `Close` on the collection stops its background work; `Close(ctx)` on a
snapshot releases its lease. A failed initial load fails the open.

## Reading

`Snapshot.List(ctx, cursor, limit)` pages through keywords, ordered by bucket number then
lexically. Start with an empty cursor and stop when `NextCursor` is empty. **Keep the same
snapshot for a whole traversal** — a cursor is bound to its generation.

`Snapshot.Diff(ctx, target)` returns sorted `Added`, `Removed`, and `Retained` without
writing anything, materializing the comparison in memory. `Count` is the normalized,
deduplicated keyword count.

Search methods — `Find`, `FindIndex`, `FindMatches`, `FindSet`, `Contains`, `FindStream`,
`FindBatch`, `FindParallel`, `FindIndexParallel` — all take explicit contexts. Streaming
keeps state across reader boundaries, and the existing parallel options apply, including
`AutoOverlap` for boundary protection. V3 reuses the V1/V2 rune positions, case folding,
overlap, and word-boundary semantics. One search — batch, parallel, or streaming — always
uses one engine.

## Writing

`Replace`, `Add`, `Remove`, `AddMany`, and `RemoveMany` all require an expected `Version`.

- **Versions are opaque equality tokens.** Never treat them as ordered numbers or strings.
- Concurrent writers holding the same expected version cannot overwrite each other; the
  loser gets `errors.Is(err, acor.ErrConcurrencyConflict)`.
- Batch changes are atomic. `Replace` accepts an empty target.
- Reapplying an identical dictionary keeps the version and publishes no invalidation.
- Keywords are trimmed, and lowercased unless `CaseSensitive` was set at creation.
  Duplicates after normalization are dropped; a blank keyword or invalid UTF-8 fails the
  entire input. The stored case policy must match every subsequent open.

### Commit is not yet visibility

A successful write means the Redis commit landed. The calling instance and every other
instance may still be searching the previous engine. `WaitForVersion` waits for that
commit or a later one and honors cancellation. `Status` exposes the observed active
version, the serving version, build start and duration, and the most recent error.
`DeltaSearch` and `DeltaKeywords` remain in the status and option types for source
compatibility. V3 currently serves one immutable complete generation for every version, regardless
of `VersionedOptions.DeltaSearch`; both fields report false and zero. This avoids the
experimental two-automaton search path, whose million-keyword measurements showed
search p95 and peak RSS regressions. The historical [delta-search validation report](../versioned-delta-validation/)
records those measurements.

| Refresh behavior | |
|---|---|
| Discovery | Polling every 30 s by default, accelerated by Pub/Sub |
| Coalescing | A fixed 20 ms debounce window (`RefreshDebounce`) merges bursts; incoming events do not extend it |
| In-flight build | Finishes, then the worker loads the newest observed target |
| Failure | The old serving engine is preserved |

## Storage layout

SHA-256's first 12 bits select one of 4,096 fixed buckets. Each bucket is sorted,
deduplicated, and split into JSON string-array chunks of at most 1 MiB including escaping
and delimiters; an oversized single keyword gets its own chunk. Content-addressed
immutable chunks, generation manifests, metadata, and the active pointer live under
separate keys.

`ShardCount` zero or one creates legacy V3: all storage uses the original name-digest
hash tag. Explicit powers of two from 2 through 256 create sharded layout version 2.
The same 4,096 bucket IDs route by `bucketID % shardCount`. Each shard's immutable
chunks and manifest use its own hash tag; a small global manifest orders those shard
IDs, and the global metadata, active pointer, leases, and receipts keep the original tag.
Separate tags allow Cluster placement across slots, but distinct shards can collide in
a slot or share a node. Every Lua operation uses keys from one tag. Live multi-node
Cluster failover and resharding have not yet been qualified for this release.

Connection options reuse the existing
standalone, Sentinel, Cluster, and Ring clients; only the connection fields and `Name`
from `VersionedOptions.Redis` are used.

Delta writes download and prepare affected buckets only; a full replacement compares every
bucket and reuses the unchanged ones. Local engine refresh reuses verified immutable
keyword slices for unchanged buckets and downloads only changed ones, and the previous
cache stays usable when a candidate fails. Sharded refresh also reuses unchanged shard
engines, builds only changed shards, and atomically installs the complete generation
after all required shards validate. A failed candidate preserves the previous complete
serving generation. Search merges all local shards with the existing match ordering;
Redis is never in the V3 search path. `ShardConcurrency` bounds shard work; zero uses
the smaller of `GOMAXPROCS` and the shard count.

Every serving replica needs RAM for the full searchable dictionary. During refresh,
the old engines, retained keyword slices, downloaded chunks, and new changed-shard
engines coexist. Full replacement and `Reshard` can temporarily require a complete
second engine set, plus manifest/input preparation memory. `Snapshot.Diff` and `Replace`
materialize their inputs; pruning materializes retention references. The default preset is
`MemoryEfficient`; `Speed` and `Balanced` are selectable. No constant-memory or latency
guarantee is made.

### Choosing a memory profile

Start with `MemoryEfficient` and a fixed shard profile such as 1, 4, or 16, then measure
your dictionary distribution, text lengths, update sizes, refresh lag, peak process RSS,
and GC pauses on the deployment host. Common prefixes, diverse prefixes, and Korean
keywords have different engine costs. More shards can duplicate prefixes and increase
search merge work. `ShardConcurrency` limits simultaneous work, but does not remove the
full-index memory requirement. A Go `GOMEMLIMIT` is a soft runtime target, not a cap on
RSS or protection from a too-large dictionary. Capacity-test the largest replacement
and layout migration before applying that profile to production.

### Changing a stored layout

Opening an existing collection always uses its persisted layout, even if a different
valid `ShardCount` is supplied. `Reshard(ctx, expected, count)` explicitly migrates the
current dictionary; count must be a power of two from 1 through 256 (zero is invalid).
It prepares the target layout under a writer lease, then commits one global pointer
using the normal expected-version conflict check. Keywords, normalization, and search
semantics are preserved. Changing layout advances the version even for an empty
dictionary; selecting the current count is a no-op with an operation receipt.

<!-- doccheck -->
```go
dictionary, err := acor.OpenVersioned(ctx, &acor.VersionedOptions{
    Redis: acor.AhoCorasickArgs{Addr: "localhost:6379", Name: "filter-v3"},
})
if err != nil { return }
defer dictionary.Close()
snapshot, err := dictionary.Snapshot(ctx)
if err != nil { return }
expected := snapshot.Version()
if err := snapshot.Close(ctx); err != nil { return }
result, err := dictionary.Reshard(ctx, expected, 16)
if err != nil { return } // ErrCommitUnknown needs ResolveOperation; see below.
if err := dictionary.WaitForVersion(ctx, result.Version); err != nil { return }
status := dictionary.Status()
log.Printf("serving=%s layout=%d shards=%d", status.ServingVersion, status.LayoutVersion, status.ShardCount)
```

Existing leased snapshots and pagination cursors keep reading their original layout.
Pruning retains their transitive shard-manifest and chunk references. Reverting the
layout uses a new `Reshard` with a freshly captured expected version, including count 1
to return to legacy storage; it preserves all keywords committed in the meantime.
It needs the same capacity and availability checks as the forward migration. Old
binaries do not understand layout version 2: deploy compatible readers and writers
before creating or migrating sharded collections.

The final Lua commit checks the expected pointer, the preparation lease, and the
maintenance lock, stores a receipt, and swaps the pointer. It does not parse the manifest
or iterate keywords. Prepared data is unreachable until committed. Durability and failover
still depend on your Redis/Valkey persistence configuration.

### Ambiguous commits

On `ErrCommitUnknown`, keep the returned `WriteResult.OperationID` and call
`ResolveOperation(ctx, id)`. A found receipt is authoritative success. **A missing receipt
is not proof that an in-flight request cannot still commit** — do not blindly reapply an
ambiguous write. Receipts and committed-version markers are retained indefinitely,
including no-op receipts, so they grow with write count.

For a failed refresh, inspect `LastError` and `FailedShard`, restore the affected Redis
shard's availability or correct the source data through a valid write, and let the
worker retry. The previous complete engine continues serving; a restart must load and
validate the active generation successfully. Do not delete immutable keys to repair
a failed build or manually swap the active pointer. Recovery of missing committed
data requires your Redis/Valkey backup and persistence procedure. A lost preparation
response cannot publish a partial layout; an ambiguous final commit requires receipt
resolution. Keep the operation ID across process restarts.

## Leases and pruning

Snapshots, builds, and preparation writers hold server-time leases: five minutes, renewed
every minute by default. Lease checks fail explicitly after expiration, and renewal cannot
resurrect an expired handle — so close snapshots promptly. A local search needs no lease
once its engine is built.

`Prune(ctx)` retains the active generation, anything prepared or committed within 24
hours, and anything protected by a valid reader lease. It deletes unreferenced chunks and
unretained manifests in batches of at most 64, and collects abandoned preparation data.
Receipts are never deleted, and there is no automatic collector; large retention sets are
enumerated in memory.

For sharded layouts, references include each retained global generation's shard
manifests and their storage coordinates, including snapshots from earlier layouts.
Cleanup also discovers abandoned target namespaces from failed migrations. Global
maintenance authority fences each occupied shard before bounded deletion; an expired
maintenance owner cannot continue deleting. Redis availability and snapshot leases
still determine whether collection can proceed.

Pruning takes a monotonically increasing maintenance fence, and only when no valid writer
lease remains. While it holds, new writers and snapshots get `ErrMaintenance` and searches
continue. Every deletion checks and extends lock ownership atomically; the lock expires
after 30 seconds if its process dies, and an expired pruner cannot delete once a successor
takes ownership. Preparation is fenced too, so an expired writer cannot create
unregistered data during cleanup.

Builds check cancellation throughout trie insertion, failure-link construction, and table
filling. `Close` cancels the active build; cancellation discards the candidate without
publishing it or leaving a detached goroutine. Ordinary new commits do not cancel an
active build, which is what stops continuous writes from starving refresh.

## Cutting over from V2

1. **Use a different name.** Migrate V1 to V2 through the existing migration first.
   `CopyV2(ctx, sourceName, expected, nil)` reads the V2 version and keywords in one
   `HMGET` and replaces the V3 target, reporting source version, normalized count, and the
   SHA-256 checksum of the sorted JSON keyword array.
2. **Rehearse.** Copy, then compare count, checksum, and representative search results —
   case-sensitive, Korean, and overlapping matches. Set the V3 case policy to the source
   application's policy; V2 does not persist it.
3. **Cut over.** Stop V2 writes for the final copy, verify source version, count,
   checksum, and search results, then point the application at the new V3 name. There is
   no automatic dual write and no simultaneous fleet-wide engine switch.
4. **Rollback is not a name change.** Once V3 writes begin, switching back to V2 loses
   them. Export and apply the V3 changes to V2 first, and keep V2 untouched until the
   migration is accepted.

## CLI

Global connection and name flags come **before** `dictionary`; the subcommand's own flags
follow it. Diff and replace read one JSON string array from stdin. Empty replacements and
empty `copy-v2` sources need `--allow-empty`, checked before the target is replaced.
Results are JSON. `list` returns one page per invocation, and a concurrent generation
change rejects an older page cursor — hold a `Snapshot` in library code for a long export.

```sh
acor -name filter-v3 dictionary status
acor -name filter-v3 dictionary list --limit 1000
printf '["한국","hello"]' | acor -name filter-v3 dictionary diff
printf '["한국","hello"]' | acor -name filter-v3 dictionary replace --expected-version TOKEN
printf '[]' | acor -name filter-v3 dictionary replace --expected-version TOKEN --allow-empty
acor -name filter-v3 dictionary copy-v2 --source filter-v2 --expected-version TOKEN
acor -name filter-v3 dictionary prune
```

## Measurements

[R1 validation and resource costs](../versioned-performance/) are the archived baseline.
R2 added incremental download reuse, bounded coalescing, cancellable builds, and more
compact sparse-engine nodes; R3 added bounded source-position search and atomic
masking/replacement ([bounded text processing](../text-processing/),
[R2/R3 verification](../r2-r3-performance/)). None of these reports is a performance
guarantee.
