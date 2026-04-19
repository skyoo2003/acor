---
title: "API Reference"
weight: 1
---

# API Reference

Complete API documentation for ACOR.

## Core Types

### AhoCorasickArgs

Configuration for creating an AhoCorasick instance.

<!-- AUTO-GENERATED:types:start -->
```go
type AhoCorasickArgs struct {
    Addr                            string            // Standalone Redis address
    Addrs                           []string          // Sentinel or Cluster addresses
    RingAddrs                       map[string]string // Ring shard addresses
    MasterName                      string            // Sentinel master name
    Password                        string            // Redis password
    DB                              int               // Redis database number (0-15, default: 0)
    Name                            string            // Collection name (required)
    Debug                           bool              // Enable debug logging to stdout
    Logger                          Logger            // Custom logger (nil disables logging)
    SchemaVersion                   int               // 0 or 2: V2 (default, optimized); 1: V1 (legacy)
    EnableCache                     bool              // Enable local in-memory caching for Find/FindIndex
    SelfInvalidationCleanupInterval uint64            // Cleanup frequency for self-invalidation map (default: 128)
    CaseSensitive                   bool              // Enable case-sensitive matching (default: false)
    RollbackTimeout                 time.Duration     // V1 rollback timeout (default: 10s)
    InMemory                        bool              // Pure in-memory mode, no Redis (default: false)
    Preset                           Preset            // Architecture preset (default: PresetNone)
}
```
<!-- AUTO-GENERATED:types:end -->

### AhoCorasick

Main type for pattern matching operations.

```go
ac, err := acor.Create(&acor.AhoCorasickArgs{...})
defer ac.Close()
```

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

```go
result, err := ac.RemoveMany([]string{"a", "b"})
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

### FindMany

Find matches in multiple texts.

```go
matches, err := ac.FindMany([]string{"text1", "text2"})
// Returns: [][]string
```

### FindParallel

Find matches using parallel processing.

```go
matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
    Workers:   4,
    Boundary:  acor.ChunkBoundaryWord,
})
```

### Info

Get collection statistics.

```go
info, err := ac.Info()
// Returns: &AhoCorasickInfo{Keywords: N, Nodes: M, Preset: ..., MemoryBytes: ..., TrieDepth: ...}
```

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

## In-Memory Engine

Pure in-memory Aho-Corasick engine with selectable architecture presets. No Redis required. Created via the unified `Create` API with `InMemory: true`.

```go
ac, err := acor.Create(&acor.AhoCorasickArgs{
    InMemory: true,
    Name:     "my-collection",
    Preset:   acor.PresetBalanced,
})
```

### AhoCorasickInfo

Statistics about an Aho-Corasick instance.

<!-- AUTO-GENERATED:types:start -->
```go
type AhoCorasickInfo struct {
    Keywords    int    // Number of keywords
    Nodes       int    // Number of trie nodes (states)
    Preset      Preset // Architecture preset (zero in original mode)
    MemoryBytes int64  // Estimated memory usage in bytes (zero in original mode)
    TrieDepth   int    // Maximum trie depth (zero in original mode)
}
```
<!-- AUTO-GENERATED:types:end -->

### Preset

Architecture presets for the in-memory and preset-optimized Redis engines.

```go
const (
    PresetNone            Preset = iota // Zero value (unset) — falls through to original V1/V2 mode
    PresetSpeed                         // Full DFA + flat array — max speed, higher memory
    PresetBalanced                      // Double-Array Trie + Banded DFA — best speed-to-memory ratio
    PresetMemoryEfficient               // Map-based + Bloom filter — min memory, slower search
    PresetUltimate                      // SIMD + Double-Array + Banded DFA — max throughput
    PresetDefault         Preset = -1   // Internal sentinel; not user-selectable
)
```

### In-Memory Methods

```go
// Create
ac, err := acor.Create(&acor.AhoCorasickArgs{
    InMemory: true,
    Name:     "my-collection",
    Preset:   acor.PresetBalanced,
})

// Add/Remove
count, err := ac.Add("keyword")      // (int, error) — returns 0 or 1
count, err := ac.Remove("keyword")   // (int, error) — returns 0 or 1

// Find
matches, err := ac.Find("text")              // ([]string, error)
positions, err := ac.FindIndex("text")       // (map[string][]int, error)

// Info
info, err := ac.Info()              // (*AhoCorasickInfo, error)

// Flush
err := ac.Flush()
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

### AhoCorasickArgs (Preset and InMemory fields)

The `AhoCorasickArgs` struct includes two fields that control engine mode:

```go
type AhoCorasickArgs struct {
    // ... standard Redis connection fields ...
    InMemory       bool   // Pure in-memory mode, no Redis (default: false)
    Preset         Preset // Architecture preset: PresetSpeed, PresetBalanced, PresetMemoryEfficient, PresetUltimate
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

// Info
info, err := ac.Info()   // (*AhoCorasickInfo, error)

// Flush
err := ac.Flush()

// Close
err := ac.Close()
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
    Skipped []string       // Keywords that were skipped (e.g., duplicates)
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
    Workers   int           // Concurrent goroutines (default: runtime.NumCPU())
    ChunkSize int           // Target chunk size in characters (default: 1000)
    Boundary  ChunkBoundary // How chunks are split (default: ChunkBoundaryWord)
    Overlap   int           // Overlap characters between chunks (default: 50)
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
