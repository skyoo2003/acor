---
title: "Parallel Matching"
weight: 2
---

# Parallel Matching

For large texts, use parallel matching to leverage multiple goroutines.

## Overview

Parallel matching splits text into chunks and processes them concurrently, significantly improving performance for large inputs.

## Basic Usage

```go
matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
    Workers:       4,
    ChunkBoundary: acor.ChunkWord,
})
if err != nil {
    panic(err)
}
```

## Chunk Boundaries

Chunk boundaries ensure matches aren't split across chunks:

### ChunkWord (default)

Splits at word boundaries, ideal for natural language text:

```go
opts := &acor.ParallelOptions{
    Workers:       4,
    ChunkBoundary: acor.ChunkWord,
}
```

### ChunkLine

Splits at line breaks, ideal for log files:

```go
opts := &acor.ParallelOptions{
    Workers:       4,
    ChunkBoundary: acor.ChunkLine,
}
```

### ChunkSentence

Splits at sentence endings, ideal for document processing:

```go
opts := &acor.ParallelOptions{
    Workers:       4,
    ChunkBoundary: acor.ChunkSentence,
}
```

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
    ChunkSize:  10000, // 10KB chunks
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
