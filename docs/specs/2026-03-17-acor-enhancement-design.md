# ACOR Project Enhancement Design

**Date:** 2026-03-17
**Author:** Design Session
**Status:** Draft

## Overview

Enhance ACOR (Aho-Corasick automation working On Redis) with batch operations, parallel matching, and streaming API capabilities.

## Goals

1. Add batch operations for efficient bulk keyword management
2. Implement parallel matching for improved performance on large texts
3. Add streaming API for memory-efficient processing of large inputs
4. Maintain backward compatibility with existing API

## Non-Goals

- Breaking changes to existing API
- New external dependencies
- Redis Cluster-specific optimizations

## File Structure

```
pkg/acor/
├── acor.go           # Core AhoCorasick struct and existing methods
├── acor_test.go      # Existing tests
├── batch.go          # Batch operations (new)
├── batch_test.go     # Batch tests (new)
├── parallel.go       # Parallel matching (new)
├── parallel_test.go  # Parallel tests (new)
├── stream.go         # Streaming API - Phase 2 (new)
├── stream_test.go    # Stream tests - Phase 2 (new)
├── options.go        # Shared options types (new)
└── errors.go         # Error definitions (new)
```

## Batch Operations

### Types

```go
type BatchMode int

const (
    BatchModeBestEffort BatchMode = iota  // Continue on errors
    BatchModeTransactional                 // Rollback all on any error
)

type BatchOptions struct {
    Mode BatchMode
}

type BatchResult struct {
    Added    []string       // Successfully added keywords
    Failed   []KeywordError // Failed keywords with reasons
    Skipped  []string       // Duplicates that were skipped
}

type KeywordError struct {
    Keyword string
    Error   error
}
```

### API

```go
func (ac *AhoCorasick) AddMany(keywords []string, opts *BatchOptions) (*BatchResult, error)
func (ac *AhoCorasick) RemoveMany(keywords []string) (*BatchResult, error)
func (ac *AhoCorasick) FindMany(texts []string) (map[string][]string, error)
```

### Behavior

- `AddMany` with `BestEffort`: Adds all valid keywords, returns what succeeded/failed
- `AddMany` with `Transactional`: Uses Redis pipeline, rolls back if any fails
- `RemoveMany`: Best-effort removal, returns what was actually removed
- `FindMany`: Searches multiple texts in one call, returns map[text]matches

## Parallel Matching

### Types

```go
type ChunkBoundary int

const (
    ChunkBoundaryWord ChunkBoundary = iota    // Split at word boundaries
    ChunkBoundarySentence                      // Split at sentence boundaries (. ! ?)
    ChunkBoundaryLine                          // Split at newlines
)

type ParallelOptions struct {
    Workers       int           // Number of goroutines (default: runtime.NumCPU())
    ChunkSize     int           // Target chunk size in runes (default: 1000)
    Boundary      ChunkBoundary // How to split chunks
    Overlap       int           // Overlap size for edge matches (default: max keyword length)
}
```

### API

```go
func (ac *AhoCorasick) FindParallel(text string, opts *ParallelOptions) ([]string, error)
func (ac *AhoCorasick) FindIndexParallel(text string, opts *ParallelOptions) (map[string][]int, error)
```

### Algorithm

1. **Auto-chunk**: Split text at specified boundaries (word/sentence/line)
2. **Overlap**: Add overlap between chunks to catch keywords spanning boundaries
3. **Worker pool**: Distribute chunks to goroutines
4. **Merge**: Deduplicate and merge results with corrected indices
5. **Context support**: Allow cancellation

### Edge Case Handling

- Keywords spanning chunk boundaries: Handled by overlap region
- Empty chunks: Skipped
- Single chunk: Falls back to sequential Find

## Streaming API

### Types

```go
type StreamOptions struct {
    BufferSize    int          // Read buffer size in bytes (default: 4096)
    MatchCallback func(Match)  // Called for each match (optional)
}

type Match struct {
    Keyword    string
    StartIndex int
    EndIndex   int
    Text       string // Surrounding context (optional)
}
```

### API

```go
func (ac *AhoCorasick) FindStream(reader io.Reader, opts *StreamOptions) ([]Match, error)
func (ac *AhoCorasick) FindStreamAsync(ctx context.Context, reader io.Reader, opts *StreamOptions) <-chan Match
```

### Behavior

- `FindStream`: Blocking read from io.Reader, returns all matches when complete
- `FindStreamAsync`: Non-blocking, sends matches to channel as found
- Sliding window buffer maintains automaton state across buffer boundaries
- Callback option for real-time processing without collecting all matches

### Memory Efficiency

- Fixed buffer size regardless of input size
- Only stores matches, not entire input
- Suitable for files/streams larger than memory

## Error Handling

### Types

```go
var (
    ErrEmptyKeyword       = errors.New("keyword cannot be empty")
    ErrInvalidChunkSize   = errors.New("chunk size must be positive")
    ErrInvalidWorkerCount = errors.New("worker count must be positive")
    ErrNoBoundariesFound  = errors.New("could not find suitable chunk boundaries")
    ErrStreamInterrupted  = errors.New("stream processing was interrupted")
)
```

### Error Wrapping

All errors wrap underlying causes where applicable:
- Redis errors: Wrapped with context about which operation failed
- Validation errors: Returned directly with clear message
- Context cancellation: Returns ctx.Err()

## Implementation Phases

### Phase 1: Core Enhancements

1. Create `options.go` and `errors.go`
2. Implement `batch.go` with `AddMany`, `RemoveMany`, `FindMany`
3. Implement `parallel.go` with `FindParallel`, `FindIndexParallel`
4. Add comprehensive tests

### Phase 2: Streaming & gRPC

1. Implement `stream.go` with `FindStream`, `FindStreamAsync`
2. Add gRPC service definitions in `pkg/server/`
3. Update `cmd/acor/` for gRPC server mode

## Dependencies

No new external dependencies required:
- Uses existing `go-redis/redis/v8`
- Standard library only for parallel/streaming

## Testing Strategy

- Unit tests for each new function
- Integration tests with miniredis (already in use)
- Benchmark tests for parallel performance
- Edge case tests for chunk boundaries and overlaps

## Backward Compatibility

- All new APIs are additions, no modifications to existing APIs
- Existing `Add`, `Remove`, `Find`, `FindIndex` methods unchanged
- Default options provide sensible behavior without configuration
