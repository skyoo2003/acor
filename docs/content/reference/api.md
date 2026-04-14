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

Find matches with their positions.

```go
positions, err := ac.FindIndex("sample text")
// Returns: map[string][]int{"keyword": {start, end}, ...}
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
// Returns: &AhoCorasickInfo{Keywords: N, Nodes: M}
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
