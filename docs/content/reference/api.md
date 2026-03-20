---
title: "API Reference"
weight: 1
---

# API Reference

Complete API documentation for ACOR.

## Core Types

### AhoCorasickArgs

Configuration for creating an AhoCorasick instance.

```go
type AhoCorasickArgs struct {
    Addr       string            // Standalone Redis address
    Addrs      []string          // Sentinel or Cluster addresses
    RingAddrs  map[string]string // Ring shard addresses
    MasterName string            // Sentinel master name
    Password   string            // Redis password
    DB         int               // Redis database number
    Name       string            // Collection name
}
```

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
result, err := ac.AddMany([]string{"a", "b", "c"}, &acor.BatchOptions{
    Mode: acor.Transactional,
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
    Workers:       4,
    ChunkBoundary: acor.ChunkWord,
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
    Mode BatchMode // Transactional or BestEffort
}
```

### BatchResult

```go
type BatchResult struct {
    Success int
    Failed  int
    Errors  []BatchError
}
```

## Parallel Options

### ParallelOptions

```go
type ParallelOptions struct {
    Workers       int
    ChunkSize     int
    ChunkBoundary ChunkBoundaryType
}
```

### ChunkBoundaryType

```go
const (
    ChunkWord     ChunkBoundaryType = iota
    ChunkLine
    ChunkSentence
)
```
