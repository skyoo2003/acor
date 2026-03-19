---
title: "Batch Operations"
weight: 1
---

# Batch Operations

ACOR supports batch operations for better performance when working with multiple keywords.

## Overview

Batch operations reduce network round-trips by grouping multiple operations together.

## Adding Multiple Keywords

```go
result, err := ac.AddMany([]string{"he", "her", "him", "his"}, &acor.BatchOptions{
    Mode: acor.Transactional,
})
if err != nil {
    panic(err)
}

fmt.Printf("Added: %d, Failed: %d\n", result.Success, result.Failed)
```

## Batch Modes

### Transactional

Rolls back all changes if any error occurs:

```go
result, err := ac.AddMany(keywords, &acor.BatchOptions{
    Mode: acor.Transactional,
})
```

### BestEffort

Continues on errors, returns partial results:

```go
result, err := ac.AddMany(keywords, &acor.BatchOptions{
    Mode: acor.BestEffort,
})
```

## Removing Multiple Keywords

```go
result, err := ac.RemoveMany([]string{"he", "her"})
if err != nil {
    panic(err)
}
```

## Finding Matches in Multiple Texts

```go
texts := []string{"he is him", "this is hers", "hello world"}
matches, err := ac.FindMany(texts)
if err != nil {
    panic(err)
}

for i, m := range matches {
    fmt.Printf("Text %d: %v\n", i, m)
}
```

## Batch Result Structure

```go
type BatchResult struct {
    Success int          // Number of successful operations
    Failed  int          // Number of failed operations
    Errors  []BatchError // Individual errors (BestEffort mode only)
}
```

## Performance Tips

1. Use batch sizes between 100-1000 keywords
2. Use `Transactional` mode when data consistency is critical
3. Use `BestEffort` mode when partial success is acceptable
