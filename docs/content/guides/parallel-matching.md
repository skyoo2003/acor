---
title: "Parallel Matching"
weight: 2
---

# Parallel Matching

For large texts, use parallel matching to scan with multiple goroutines.

## Overview

Parallel matching splits text into chunks and processes them concurrently, significantly improving performance for large inputs.

## Basic Usage

<!-- doccheck -->
```go
matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
    Workers:       4,
    ChunkSize:     1000,
    AutoOverlap:   true,
    Boundary: acor.ChunkBoundaryWord,
})
if err != nil {
    panic(err)
}
_ = matches
```

## Chunk Boundaries

Boundary selection chooses where base chunks end. Enable `AutoOverlap` to protect
matches across every boundary; boundary selection alone cannot guarantee this.

### ChunkBoundaryWord (default)

Splits at word boundaries, ideal for natural language text:

```go
opts := &acor.ParallelOptions{
    Workers:       4,
    ChunkSize:     1000,
    AutoOverlap:   true,
    Boundary: acor.ChunkBoundaryWord,
}
```

### ChunkBoundaryLine

Splits at line breaks, ideal for log files:

```go
opts := &acor.ParallelOptions{
    Workers:       4,
    ChunkSize:     1000,
    AutoOverlap:   true,
    Boundary: acor.ChunkBoundaryLine,
}
```

### ChunkBoundarySentence

Splits at sentence endings, ideal for document processing:

```go
opts := &acor.ParallelOptions{
    Workers:       4,
    ChunkSize:     1000,
    AutoOverlap:   true,
    Boundary: acor.ChunkBoundarySentence,
}
```

## Automatic Boundary Protection

`AutoOverlap: true` uses the longest keyword in the engine loaded for this call.
Each base chunk owns its starting positions and extends its right search range by
`max(Overlap, longest keyword rune length - 1)`. Matches starting in the extension
belong to the next base chunk and are excluded from the current chunk. This also
finds keywords longer than `ChunkSize`, including Korean and emoji keywords.
No extra Redis query is needed, and all workers use the same dictionary snapshot.

`FindParallel` returns each keyword once, in chunk order and then first-match scan
order within each chunk. That order need not equal serial scan order.
`FindIndexParallel` returns sorted unique rune positions in the original text.

The default `AutoOverlap` is `false`, preserving legacy overlapping chunks.
In that mode, an insufficient `Overlap` can miss boundary matches, and a keyword
longer than a chunk may not fit in any chunk. Options are copied before normalization.
`ChunkSize` must be positive; empty input performs no Redis reads.

## Performance Tuning

### Worker Count

Choose worker count based on CPU cores:

```go
workers := runtime.NumCPU()
```

For I/O-bound workloads, consider higher counts:

```go
workers := runtime.NumCPU() * 2
```

### Chunk Size

Control chunk size with the `ChunkSize` option:

```go
opts := &acor.ParallelOptions{
    Workers:    4,
    ChunkSize:  10000, // 10,000 runes per base chunk
}
```

## When to Use Parallel Matching

- Text size > 100KB
- Many pattern matches expected
- CPU cores available for parallel work

## When to Avoid

- Small texts (< 10KB)
- Single-match scenarios
- Resource-constrained environments

## Next Steps

- [Batch Operations](../batch-operations/) - Optimize bulk keyword operations
- [API Reference](../../reference/api/) - Complete API documentation
