---
title: "API Reference"
weight: 1
---

# API Reference

Core API documentation for ACOR. See
[pkg.go.dev](https://pkg.go.dev/github.com/skyoo2003/acor/pkg/acor) for the
complete generated reference.

## Core Types

### AhoCorasickArgs

Configuration for creating an AhoCorasick instance.

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

### AhoCorasick

Main type for pattern matching operations.

```go
ac, err := acor.Create(&acor.AhoCorasickArgs{...})
defer ac.Close()
```

`CreateContext` is the same constructor with a context bounding the setup I/O
(schema check and initialization write, initial keyword load):

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

The context bounds construction only. Canceling it afterwards does not close the
instance or stop its invalidation listener — use `Close` for that, and the
`*Context` methods for per-operation cancellation. The Pub/Sub subscribe is part
of that listener, so it runs on the instance's own context rather than this one.

## Core Methods

### Add

Add a single keyword to the collection.

```go
count, err := ac.Add("keyword")
```

### AddMany

Add multiple keywords in a batch.

```go
result, err := ac.AddMany([]string{"a", "b", "c"}, nil)
// or with options:
result, err := ac.AddMany([]string{"a", "b", "c"}, &acor.BatchOptions{
    Mode: acor.BatchModeTransactional,
})
```

### Remove

Remove a single keyword from the collection.

```go
count, err := ac.Remove("keyword")
```

### RemoveMany

Remove multiple keywords in a batch.

<!-- doccheck -->
```go
result, err := ac.RemoveMany([]string{"a", "b"}, nil)
// Pass options when transactional behavior is required.
result, err = ac.RemoveMany([]string{"c", "d"}, &acor.BatchOptions{
    Mode: acor.BatchModeTransactional,
})
_ = result
_ = err
```

### Find

Find all matching keywords in text.

```go
matches, err := ac.Find("sample text")
// Returns: []string{"match1", "match2", ...}
```

### FindIndex

Find matches with their start positions.

```go
positions, err := ac.FindIndex("sample text")
// Returns: map[string][]int{"keyword": {startPos, ...}, ...}
```

### FindMatches

Return every occurrence in scan order with its keyword and half-open rune span
`[Start, End)`. The default includes overlapping matches; use
`MatchKindLeftmostLongest` for non-overlapping tokenization or replacement.

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

`WholeWord` uses letters, digits, combining marks, and underscores as word
runes. Set `WordRune` when those defaults do not fit the input script.

### Contains

Report whether any keyword occurs, stopping at the first match.

```go
found, err := ac.Contains("sample text")
```

### FindStream

Scan an `io.Reader` without buffering the whole input. Matches include
overlaps, retain rune offsets across reads, and arrive in scan order. Returning
`false` from the callback stops the scan.

<!-- doccheck -->
```go
err := ac.FindStream(strings.NewReader("sample text"), func(match acor.Match) bool {
    _ = match
    return true
})
_ = err
```

Streaming does not apply whole-word or leftmost-longest filtering because
those modes require buffering. Use `FindMatches` for bounded strings that need
those options.

### FindMany

Find matches in multiple texts.

```go
matches, err := ac.FindMany([]string{"text1", "text2"})
// Returns: map[string][]string{"text1": {"kw", ...}, ...} (keyed by input text)
```

### FindParallel

Find matches using parallel processing.

```go
matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
    Workers:   4,
    Boundary:  acor.ChunkBoundaryWord,
})
```

Keywords longer than `ParallelOptions.Overlap` can be missed at chunk
boundaries. Set `Overlap` to at least the longest expected keyword.

### FindIndexParallel

Find start positions using the same parallel chunking options.

```go
positions, err := ac.FindIndexParallel(largeText, acor.DefaultParallelOptions())
```

### Info

Get collection statistics.

```go
info, err := ac.Info()
// Returns: &AhoCorasickInfo{Keywords: N, Nodes: M, Preset: ..., MemoryBytes: ..., TrieDepth: ...}
```

### CacheStats

Get local cache statistics. Unlike `Info`, this performs no Redis I/O, so it is cheap
enough to scrape on a timer.

```go
stats := ac.CacheStats()
// Returns: CacheStats{Hits: N, Misses: M, Rebuilds: R, RebuildDuration: ..., LastInvalidationLag: ...}
```

The counters are per instance and per process — scrape every instance in a fleet. See
[Monitoring](../../operations/monitoring/) for how to read them, including why
`Rebuilds` does not equal `Misses` and why `LastInvalidationLag` carries clock skew.

### Flush

Clear all data from the collection.

```go
err := ac.Flush()
```

### Close

Close the Redis connection.

```go
err := ac.Close()
```

### AhoCorasickInfo

Statistics about an Aho-Corasick instance.

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

### CacheStats (type)

A snapshot of one instance's local cache activity. Returned by `CacheStats()`, never
constructed by callers — fields may be added inside `v1`.

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

### Preset

Architecture presets for the preset-optimized Redis engine.

```go
const (
    PresetNone            Preset = iota // Zero value (unset) — falls through to original V1/V2 mode
    PresetSpeed                         // Full DFA + flat array — max speed, higher memory
    PresetBalanced                      // Double-Array Trie + Banded DFA — best speed-to-memory ratio
    PresetMemoryEfficient               // Map-based + Bloom filter — min memory, slower search
)
```

## Redis-Backed Engine with Presets

Redis-backed Aho-Corasick that combines Redis persistence with a local preset-optimized automaton. Writes go to Redis atomically (V2 Lua scripts with optimistic locking); reads hit the local engine with no Redis I/O. Created via the unified `Create` API with `Preset` set.

```go
ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr:          "localhost:6379",
    Name:          "my-collection",
    Preset:        acor.PresetBalanced,
    CaseSensitive: false,
})
defer ac.Close()
```

### AhoCorasickArgs (Preset field)

The `AhoCorasickArgs` struct includes a `Preset` field for the engine mode:

```go
type AhoCorasickArgs struct {
    // ... standard Redis connection fields ...
    Preset         Preset // Architecture preset: PresetSpeed, PresetBalanced, PresetMemoryEfficient
    // ... other fields ...
}
```

### Preset-Optimized Redis Methods

```go
// Create
ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr:   "localhost:6379",
    Name:   "my-collection",
    Preset: acor.PresetBalanced,
})

// Add/Remove
added, err := ac.Add("keyword")      // (int, error)
removed, err := ac.Remove("keyword") // (int, error)

// Find (0 RTT on hot path — reads from local engine)
matches, err := ac.Find("text")       // ([]string, error)
positions, err := ac.FindIndex("text") // (map[string][]int, error)
spans, err := ac.FindMatches("text", nil) // ([]Match, error)
found, err := ac.Contains("text")          // (bool, error)

// Info
info, err := ac.Info()   // (*AhoCorasickInfo, error)

// Flush
err := ac.Flush()

// Close
err := ac.Close()
```

## Context Variants

Operations that may perform Redis I/O also accept an explicit
`context.Context`: `AddContext`, `RemoveContext`, `FindContext`,
`FindIndexContext`, `FindMatchesContext`, `ContainsContext`,
`FindStreamContext`, `FlushContext`, `InfoContext`, `SuggestContext`,
`SuggestIndexContext`, `AddManyContext`, `RemoveManyContext`,
`FindManyContext`, `FindParallelContext`, and `FindIndexParallelContext`.

```go
matches, err := ac.FindMatchesContext(ctx, text, nil)
```

## Suggest Methods

### Suggest

Get prefix suggestions.

```go
suggestions, err := ac.Suggest("pre")
```

### SuggestIndex

Get suggestions with positions.

```go
positions, err := ac.SuggestIndex("pre")
```

## Batch Operations

### BatchOptions

```go
type BatchOptions struct {
    Mode BatchMode // BestEffort (default) or Transactional
}
```

### BatchResult

```go
type BatchResult struct {
    Added   []string       // Successfully added keywords
    Removed []string       // Successfully removed keywords
    Failed  []KeywordError // Keywords that failed with their errors
    Skipped []string       // Duplicate adds or absent removes
}
```

### KeywordError

```go
type KeywordError struct {
    Keyword string
    Error   error
}
```

## Parallel Options

### ParallelOptions

```go
type ParallelOptions struct {
    Workers     int           // Concurrent goroutines (default: runtime.NumCPU())
    ChunkSize   int           // Target chunk size in characters (required; no fallback)
    Boundary    ChunkBoundary // How chunks are split (default: ChunkBoundaryWord)
    Overlap     int           // Overlap characters between chunks (unset means zero)
    AutoOverlap bool          // Extend each chunk by the dictionary's longest keyword (default: false)
}
```

### DefaultParallelOptions

Returns parallel options with sensible defaults:

```go
opts := acor.DefaultParallelOptions()
matches, err := ac.FindParallel(text, opts)
```

### ChunkBoundary

```go
const (
    ChunkBoundaryWord     ChunkBoundary = iota // Split at whitespace (default)
    ChunkBoundarySentence                       // Split at sentence boundaries (. ! ?)
    ChunkBoundaryLine                           // Split at newlines
)
```

## Parallel Boundary Protection and Preset Refresh

`ParallelOptions.AutoOverlap` (default `false`) enables dictionary-aware right
extensions for each base chunk. It protects keywords longer than `ChunkSize`
without additional Redis reads. Results use the existing parallel order and rune
positions. See [parallel matching](../../guides/parallel-matching/).

`CacheStats.PresetReloadFailures` and `CacheStats.PresetPollFailures` are cumulative
`uint64` counters. Shared reload failures count once per job; cancellations are
excluded. They remain zero in other modes. See [Preset refresh](../../guides/redis-backed-engine/#invalidation-safety).

## Versioned dictionaries

`OpenVersioned(ctx, *VersionedOptions)` opens a separate V3 collection. Its
`VersionedCollection` API provides leased snapshots, paginated list/diff, expected-version
replace/add/remove, asynchronous engine refresh, operation receipts, V2 copying
and explicit pruning. See [the V3 API guide](../versioned/) for contracts and a
compilable example; V3 uses the existing module version and search semantics.

## Bounded source-position APIs

`Scan`, `MaskText`, and `ReplaceText` are available on AhoCorasick and
VersionedCollection with explicit contexts. See [bounded text processing](../text-processing/)
for ScanOptions, RewriteOptions, original byte/rune spans and error contracts.
