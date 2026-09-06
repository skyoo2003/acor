---
title: "API Reference"
weight: 1
---

# API Reference

Contracts and behavior for `pkg/acor`. Generated signatures live on
[pkg.go.dev](https://pkg.go.dev/github.com/skyoo2003/acor/pkg/acor).

## Creating a collection

```go
ac, err := acor.Create(&acor.AhoCorasickArgs{...})
defer ac.Close()
```

`CreateContext` is the same constructor with a context bounding the setup I/O — the
schema check, the initialization write, and the initial keyword load:

<!-- doccheck -->
```go
setupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
defer cancel()
instance, err := acor.CreateContext(setupCtx, &acor.AhoCorasickArgs{
    Addr: "localhost:6379",
    Name: "default",
})
_ = instance
_ = err
```

That context bounds construction only. Cancelling it later neither closes the instance
nor stops its invalidation listener — use `Close` for that and the `*Context` methods for
per-operation cancellation. The Pub/Sub subscribe belongs to the listener, so it runs on
the instance's own context.

### AhoCorasickArgs

<!-- AUTO-GENERATED:types:start -->
```go
type AhoCorasickArgs struct {
    Addr                            string            // Standalone Redis address (conflicts with Addrs)
    Addrs                           []string          // Sentinel or Cluster addresses (one entry still means cluster)
    RingAddrs                       map[string]string // Ring shard addresses
    MasterName                      string            // Sentinel master name
    Password                        string            // Redis password
    DB                              int               // Redis database number (default: 0; rejected with Addrs)
    DialTimeout                     time.Duration     // Connection timeout (zero: go-redis default)
    ReadTimeout                     time.Duration     // Socket read timeout (zero: go-redis default)
    WriteTimeout                    time.Duration     // Socket write timeout (zero: go-redis default)
    MaxRetries                      int               // Command retries (zero: go-redis default; -1: disabled)
    PoolSize                        int               // Connections per server (zero: go-redis default)
    Name                            string            // Collection name (required)
    Debug                           bool              // Send the default logger to stdout (ignored when Logger is set)
    Logger                          Logger            // Custom logger (nil disables logging)
    SchemaVersion                   int               // 0 or 2: V2 (default, optimized); 1: V1 (deprecated)
    EnableCache                     bool              // Local caching for Find/FindIndex (V2 only, not with Preset)
    SelfInvalidationCleanupInterval uint64            // Cleanup frequency for self-invalidation map (default: 128)
    CaseSensitive                   bool              // Enable case-sensitive matching (default: false)
    RollbackTimeout                 time.Duration     // V1 flush/rollback timeout, not the caller's ctx (default: 10s)
    Preset                          Preset            // Architecture preset (default: PresetNone)
    InvalidationPollInterval        time.Duration     // Preset version polling (zero: disabled)
}
```
<!-- AUTO-GENERATED:types:end -->

Setting `Preset` switches reads to a local automaton — see
[Preset-Optimized Engine](../../guides/preset-engine/). Topology fields are covered in
[Redis topologies](../../getting-started/quick-start/#redis-topologies).

## Writing

| Method | Returns | Notes |
| ------ | ------- | ----- |
| `Add(keyword)` | `(int, error)` | 1 if the collection changed, 0 if it was already there |
| `Remove(keyword)` | `(int, error)` | Same convention |
| `AddMany(keywords, *BatchOptions)` | `(*BatchResult, error)` | One transaction; `nil` options mean best-effort |
| `RemoveMany(keywords, *BatchOptions)` | `(*BatchResult, error)` | Same |
| `Flush()` | `error` | Deletes every key in the collection |
| `Close()` | `error` | Closes the connection and stops the invalidation listener |

<!-- doccheck -->
```go
result, err := ac.RemoveMany([]string{"a", "b"}, nil)
// Pass options when the whole batch must land or fail together.
result, err = ac.RemoveMany([]string{"c", "d"}, &acor.BatchOptions{
    Mode: acor.BatchModeTransactional,
})
_ = result
_ = err
```

See [Batch Operations](../../guides/batch-operations/) for the modes and result shape.

## Matching

| Method | Returns | Shape |
| ------ | ------- | ----- |
| `Find(text)` | `[]string` | Which keywords occur |
| `FindIndex(text)` | `map[string][]int` | Keyword to its rune start positions |
| `FindSet(text)` | `[]string` | Each keyword once, in first-match order |
| `FindMatches(text, *MatchOptions)` | `[]Match` | Every occurrence with its rune span |
| `Contains(text)` | `bool` | Stops at the first match |
| `FindStream(reader, callback)` | `error` | Scans an `io.Reader` without buffering it |
| `FindMany(texts)` | `map[string][]string` | Keyed by input text |
| `FindParallel(text, *ParallelOptions)` | `[]string` | Chunked across workers |
| `FindIndexParallel(text, *ParallelOptions)` | `map[string][]int` | Same chunking, with positions |

All positions are **rune** offsets, not byte offsets.

### FindMatches

Every occurrence in scan order with its half-open rune span `[Start, End)`. The default
includes overlaps; `MatchKindLeftmostLongest` produces non-overlapping spans suitable for
tokenizing or replacing.

<!-- doccheck -->
```go
matches, err := ac.FindMatches("classic class", &acor.MatchOptions{
    Kind:      acor.MatchKindLeftmostLongest,
    WholeWord: true,
})
_ = matches
_ = err
```

```go
type Match struct {
    Keyword string
    Start   int // Rune offset, inclusive
    End     int // Rune offset, exclusive
}

type MatchOptions struct {
    Kind      MatchKind
    WholeWord bool
    WordRune  func(rune) bool // Optional whole-word predicate
}

const (
    MatchKindOverlapping MatchKind = iota // Default
    MatchKindLeftmostLongest
)
```

`WholeWord` treats letters, digits, combining marks, and underscores as word runes. Set
`WordRune` for scripts those defaults do not fit — in CJK or Thai text every adjacent
character counts as a word rune, so nearly every match is dropped as mid-word.

### FindStream

Matches keep rune offsets across reads and arrive in scan order; returning `false` from
the callback stops the scan.

<!-- doccheck -->
```go
err := ac.FindStream(strings.NewReader("sample text"), func(match acor.Match) bool {
    _ = match
    return true
})
_ = err
```

Streaming always overlaps: whole-word and leftmost-longest need buffering, so use
`FindMatches` on a bounded string when you need them.

### Parallel

```go
type ParallelOptions struct {
    Workers     int           // Concurrent goroutines (default: runtime.NumCPU())
    ChunkSize   int           // Target chunk size in runes (required; no fallback)
    Boundary    ChunkBoundary // How chunks are split (default: ChunkBoundaryWord)
    Overlap     int           // Overlap runes between chunks (unset means zero)
    AutoOverlap bool          // Extend each chunk by the dictionary's longest keyword (default: false)
}

const (
    ChunkBoundaryWord     ChunkBoundary = iota // Split at whitespace (default)
    ChunkBoundarySentence                      // Split at . ! ?
    ChunkBoundaryLine                          // Split at newlines
)
```

`DefaultParallelOptions()` returns a usable starting point. Without `AutoOverlap`, a
keyword longer than `Overlap` can be missed at a chunk boundary; with it, chunks extend
by the dictionary's longest keyword at no extra Redis cost. Details:
[Parallel Matching](../../guides/parallel-matching/).

## Suggest

`Suggest(prefix)` returns keywords starting with the prefix; `SuggestIndex(prefix)`
returns the same keys mapped to `[0]`, since a prefix match always starts at the
beginning. Both require Redis and are unavailable in `Preset` mode
(`ErrSuggestRequiresRedis`).

## Batch types

```go
type BatchOptions struct {
    Mode BatchMode // BatchModeBestEffort (default) or BatchModeTransactional
}

type BatchResult struct {
    Added   []string       // Successfully added keywords
    Removed []string       // Successfully removed keywords
    Failed  []KeywordError // Keywords that failed, with their errors
    Skipped []string       // Duplicate adds or absent removes
}

type KeywordError struct {
    Keyword string
    Error   error
}
```

## Statistics

`Info()` reads Redis; `CacheStats()` does not, so it is cheap to scrape on a timer.

<!-- AUTO-GENERATED:types:start -->
```go
type AhoCorasickInfo struct {
    Keywords    int    // Number of keywords
    Nodes       int    // Number of trie nodes (states)
    Preset      Preset // Architecture preset (internal default sentinel in original mode)
    MemoryBytes int64  // Estimated memory usage in bytes (zero in original mode)
    TrieDepth   int    // Maximum trie depth (zero in original mode)
}
```
<!-- AUTO-GENERATED:types:end -->

```go
type CacheStats struct {
    PresetReloadFailures uint64        // Failed shared reload jobs, once per job (Preset only; cancellation excluded)
    PresetPollFailures   uint64        // Failed version polls (Preset with polling enabled only)
    Hits                 uint64        // Reads served without rebuilding the automaton
    Misses               uint64        // Reads that waited for a rebuild
    Rebuilds             uint64        // Automaton builds (starts at 1 in Preset mode)
    RebuildDuration      time.Duration // Cumulative build time, excluding Redis I/O
    LastInvalidationLag  time.Duration // Last peer invalidation delay (Preset/EnableCache only; carries clock skew)
}
```

`CacheStats` is returned by ACOR and never constructed by callers, so fields may be added
inside `v1`. Counters are per instance and per process — scrape every instance in a fleet.
How to read them, including why `Rebuilds` does not equal `Misses`:
[Monitoring](../../operations/monitoring/).

## Presets

```go
const (
    PresetNone            Preset = iota // Zero value — original V1/V2 mode
    PresetSpeed                         // Full DFA + flat array — max speed, higher memory
    PresetBalanced                      // Double-Array Trie + Banded DFA — best speed-to-memory ratio
    PresetMemoryEfficient               // Map-based + Bloom filter — min memory, slower search
)
```

## Context variants

Every method that may touch Redis has a `*Context` twin taking an explicit
`context.Context`: `AddContext`, `RemoveContext`, `FindContext`, `FindIndexContext`,
`FindSetContext`, `FindMatchesContext`, `ContainsContext`, `FindStreamContext`,
`FlushContext`, `InfoContext`, `SuggestContext`, `SuggestIndexContext`,
`AddManyContext`, `RemoveManyContext`, `FindManyContext`, `FindParallelContext`, and
`FindIndexParallelContext`.

```go
matches, err := ac.FindMatchesContext(ctx, text, nil)
```

## Beyond V1/V2

- **`OpenVersioned(ctx, *VersionedOptions)`** opens a separate V3 collection with leased
  snapshots, paginated list/diff, expected-version writes, background engine refresh, and
  operation receipts. See [Versioned dictionaries](../versioned/).
- **`Scan`, `MaskText`, `ReplaceText`** report original byte and rune spans under explicit
  work limits, on both `AhoCorasick` and `VersionedCollection`. See
  [bounded text processing](../text-processing/).
