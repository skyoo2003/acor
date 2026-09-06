---
title: "Bounded search, masking and replacement"
description: "Original byte and rune positions with explicit work and output limits."
---

# Bounded search, masking and replacement

`Scan`, `MaskText`, and `ReplaceText` exist on both `AhoCorasick` and
`VersionedCollection` and all take a context. The existing `Find`/`FindMatches` APIs keep
their unlimited result and position contracts. A V3 call holds one serving engine for the
whole scan or rewrite, even if a refresh finishes concurrently.

## Scan

`Scan(ctx, text, *ScanOptions)` returns `SourceMatch` entries carrying the normalized
`Keyword`, the original `Text` substring, and half-open `Start`/`End` rune and
`ByteStart`/`ByteEnd` byte offsets into the **original** input.

Case folding can change byte lengths — `İSTANBUL` matches `istanbul`, and its byte span
still selects the original spelling. Output text is never normalized. Invalid UTF-8 is
decoded as `RuneError` for matching, while the reported byte slices keep those exact
original bytes.

| Option | Default | At the limit |
|---|---:|---|
| `MaxInputBytes` | 1 MiB | `ErrInputLimit`, before the engine is loaded or scanned |
| `MaxMatches` | 1,000 | Keeps at most this many; sets `Truncated` when another eligible match appears |
| `MaxCandidates` | 100,000 | `ErrScanWorkLimit` on the next raw automaton match |

Zero-valued limits select the defaults; negative limits are rejected. Construct
`ScanOptions` and `RewriteOptions` with named fields.

`Kind` defaults to `MatchKindOverlapping`. `MatchKindLeftmostLongest` takes the leftmost
available start, then the longest keyword there, and discards overlaps — implemented by
keeping the longest candidate at each pending start until the engine's longest keyword
makes the decision safe, not by collecting and sorting all raw matches. Scratch memory is
bounded by input length plus the pending-start window and retained results; it is not
constant.

`WholeWord` and `WordRune` carry the same normalized-rune semantics as `MatchOptions`:
letters, digits, combining marks, and underscore are word characters by default, and
scripts without spaces usually need an application-specific `WordRune`. Candidates are
counted *before* whole-word and overlap filtering, so a dense rejected-match workload
cannot slip past the work budget. Custom `WordRune` code runs in the caller's goroutine.

**Errors and `Truncated` are different signals.** Input exhaustion, candidate exhaustion,
and cancellation all return an error and no result; `Truncated` concerns only the eligible
result count. Checking for a clean input means handling both. The context is checked
during input indexing, traversal, and match handling.

## Masking and replacement

`ReplaceText(ctx, text, replacement, *RewriteOptions)` inserts the literal replacement for
each selected non-overlapping leftmost-longest match. No regular expressions, no capture
expansion, no re-searching the replacement. An empty replacement deletes matched spans.

`MaskText(ctx, text, maskRune, *RewriteOptions)` writes one mask rune per matched original
rune, preserving rune count but not necessarily byte count. Any valid Unicode rune is
accepted, including NUL; invalid runes are rejected.

Both leave unmatched original bytes untouched. `RewriteOptions` shares the three scan
limits and adds `MaxOutputBytes`, defaulting to 4 MiB. Overlap selection is always
leftmost-longest. Exceeding `MaxMatches` gives `ErrMatchLimit`, exceeding the output bound
gives `ErrOutputLimit`, and **every error returns no `RewriteResult`** — a caller cannot
accidentally consume a half-masked document. Output size is checked before the buffer is
allocated.

`RewriteResult` holds `Text` plus the `SourceMatch` entries used. Their offsets always
refer to the **input**, even when replacement changes the output length. Match `Text`
substrings can retain the original input string in memory.

<!-- doccheck -->
```go
package main

import (
    "context"
    "log"

    "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
    ctx := context.Background()
    dictionary, err := acor.OpenVersioned(ctx, &acor.VersionedOptions{
        Redis: acor.AhoCorasickArgs{Addr: "localhost:6379", Name: "text-v3"},
    })
    if err != nil { log.Fatal(err) }
    defer dictionary.Close()

    snapshot, err := dictionary.Snapshot(ctx)
    if err != nil { log.Fatal(err) }
    expected := snapshot.Version()
    snapshot.Close(ctx)
    write, err := dictionary.Replace(ctx, expected, []string{"한국", "한국어", "istanbul"})
    if err != nil { log.Fatal(err) }
    if err := dictionary.WaitForVersion(ctx, write.Version); err != nil { log.Fatal(err) }

    found, err := dictionary.Scan(ctx, "한국어 İSTANBUL", &acor.ScanOptions{
        Kind: acor.MatchKindLeftmostLongest, MaxMatches: 10,
    })
    if err != nil { log.Fatal(err) }
    if found.Truncated { log.Fatal("incomplete result") }
    for _, match := range found.Matches {
        log.Printf("%q at input bytes [%d,%d)", match.Text, match.ByteStart, match.ByteEnd)
    }
    masked, err := dictionary.MaskText(ctx, "한국어 İSTANBUL", '*', nil)
    if err != nil { log.Fatal(err) }
    log.Print(masked.Text) // *** ********
}
```

Parity, resource-bound, and cancellation evidence:
[R2/R3 report](../r2-r3-performance/).
