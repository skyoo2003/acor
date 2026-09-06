---
title: "Batch Operations"
weight: 1
---

# Batch Operations

`AddMany` and `RemoveMany` commit a whole batch in one transaction — two round trips
regardless of batch size, against one per keyword for a loop over `Add`.

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

`nil` options mean best-effort, which is also what `BatchOptions` with no `Mode` selects.

## Modes

| Mode | On a per-keyword failure |
| ---- | ------------------------ |
| `BatchModeBestEffort` (default) | Commits the rest; failures land in `result.Failed`, and the call still returns success |
| `BatchModeTransactional` | Rolls the whole batch back and returns an error |

Inspect what best-effort dropped:

```go
for _, ke := range result.Failed {
    fmt.Printf("%q failed: %v\n", ke.Keyword, ke.Error)
}
```

`result.Skipped` holds duplicate adds and absent removes — not errors. Field-by-field
shapes for `BatchResult` and `KeywordError` are in the
[API reference](../../reference/api/#batchresult).

## Scanning many texts

`FindMany` loads the automaton once and scans every text against that one snapshot, so it
costs the same round trip as a single `Find`. The result is keyed by the input text.

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

## Sizing

100–1,000 keywords per batch is the useful range: below it the transaction overhead
dominates, above it the single Lua commit grows large enough to block Redis noticeably.
Measured costs are on the [benchmarks page](../../reference/benchmarks/#bulk-load-addmany).
