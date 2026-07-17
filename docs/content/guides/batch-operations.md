---
title: "Batch Operations"
weight: 1
---

# Batch Operations

ACOR supports batch operations for better performance when working with multiple keywords.

## Overview

Batch operations reduce network round-trips by grouping multiple operations together.

## Adding Multiple Keywords

<!-- doccheck -->
```go
result, err := ac.AddMany([]string{"he", "her", "him", "his"}, &acor.BatchOptions{
    Mode: acor.BatchModeTransactional,
})
if err != nil {
    panic(err)
}

fmt.Printf("Added: %d, Failed: %d, Skipped: %d\n",
    len(result.Added), len(result.Failed), len(result.Skipped))
```

## Batch Modes

### Transactional

Rolls back all changes if any error occurs:

```go
result, err := ac.AddMany(keywords, &acor.BatchOptions{
    Mode: acor.BatchModeTransactional,
})
```

### BestEffort

Continues on errors and returns partial results:

```go
result, err := ac.AddMany(keywords, &acor.BatchOptions{
    Mode: acor.BatchModeBestEffort,
})
```

## Removing Multiple Keywords

```go
result, err := ac.RemoveMany([]string{"he", "her"})
if err != nil {
    panic(err)
}
fmt.Printf("Removed: %d\n", len(result.Removed))
```

Use `RemoveManyWithOptions` to control the batch mode:

```go
result, err := ac.RemoveManyWithOptions([]string{"he", "her"}, &acor.BatchOptions{
    Mode: acor.BatchModeTransactional,
})
```

## Finding Matches in Multiple Texts

<!-- doccheck -->
```go
texts := []string{"he is him", "this is hers", "hello world"}
results, err := ac.FindMany(texts)
if err != nil {
    panic(err)
}

for text, matches := range results {
    fmt.Printf("Text %q: %v\n", text, matches)
}
```

## Batch Result Structure

```go
type BatchResult struct {
    Added   []string       // Keywords successfully added
    Removed []string       // Keywords successfully removed
    Failed  []KeywordError // Keywords that could not be processed, with their errors
    Skipped []string       // Keywords skipped (e.g. duplicates in the input)
}

type KeywordError struct {
    Keyword string // The keyword that caused the error
    Error   error  // The error that occurred while processing it
}
```

Inspect failures in `BatchModeBestEffort`:

```go
for _, ke := range result.Failed {
    fmt.Printf("%q failed: %v\n", ke.Keyword, ke.Error)
}
```

## Performance Tips

1. Use batch sizes between 100-1000 keywords
2. Use `BatchModeTransactional` when data consistency is critical
3. Use `BatchModeBestEffort` when partial success is acceptable

## Next Steps

- [Parallel Matching](parallel-matching/) - Process large texts efficiently
- [API Reference](../reference/api/) - Complete API documentation
