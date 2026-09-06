---
title: "Parallel Matching"
weight: 2
---

# Parallel Matching

`FindParallel` and `FindIndexParallel` split one text into chunks and scan them
concurrently. The automaton is loaded once per call, so the Redis cost is the same as a
serial `Find` however many chunks result.

<!-- doccheck -->
```go
matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
    Workers:     4,
    ChunkSize:   1000, // runes per base chunk; must be positive
    AutoOverlap: true,
    Boundary:    acor.ChunkBoundaryWord,
})
if err != nil {
    panic(err)
}
_ = matches
```

## Boundaries

Only `Boundary` changes between these; everything else stays as above.

| Boundary | Splits at | Suited to |
| -------- | --------- | --------- |
| `ChunkBoundaryWord` (default) | Whitespace | Natural language |
| `ChunkBoundaryLine` | Newlines | Log files |
| `ChunkBoundarySentence` | `.` `!` `?` | Documents |

Boundary choice alone does not protect a keyword that straddles a split. `AutoOverlap`
does.

## Boundary protection

`AutoOverlap: true` extends each base chunk to the right by
`max(Overlap, longest keyword rune length - 1)`, using the longest keyword in the engine
already loaded for this call — no extra Redis read, and every worker shares one snapshot.
Matches starting inside an extension belong to the next chunk and are not double-counted.
This also catches keywords longer than `ChunkSize`, including Korean and emoji.

`AutoOverlap` defaults to `false`, which keeps the legacy behavior: an `Overlap` shorter
than your longest keyword silently misses boundary matches, and a keyword longer than
`ChunkSize` may fit in no chunk at all.

## Result order

`FindParallel` reports each keyword once, in chunk order and then first-match order
within a chunk — which need not equal serial scan order. `FindIndexParallel` returns
sorted, unique rune positions in the original text. Empty input performs no Redis read.

## When it pays

Worth it above roughly 100 KB of text, with cores to spare. Below ~10 KB, or for a
single expected match, the chunking overhead outweighs it. Start `Workers` at
`runtime.NumCPU()`.
