---
title: "Bounded search, masking and replacement"
description: "Original byte and rune positions with explicit work and output limits."
---

R3 adds `Scan`, `MaskText`, and `ReplaceText` to both AhoCorasick and
VersionedCollection. All take a context. Existing Find/FindMatches APIs retain
their existing unlimited result and position contracts. V3 calls hold one serving
engine for the entire scan or rewrite, even if a refresh finishes concurrently.

## Original positions and bounded search

`Scan(ctx, text, *ScanOptions)` returns SourceMatch entries containing the normalized
Keyword, the original Text substring, and half-open Start/End rune and
ByteStart/ByteEnd byte offsets into the original input. Unicode case folding can
change byte lengths: `İSTANBUL` matches `istanbul`, but its byte span still selects
the original spelling. No normalization of the original output text occurs.
Invalid UTF-8 input bytes are decoded as RuneError for matching, while reported
byte slices retain those exact original bytes.

Zero-valued limits select these defaults; negative limits are rejected. Construct
ScanOptions and RewriteOptions using named fields for forward compatibility.

| Option | Default | Behavior at limit |
|---|---:|---|
| MaxInputBytes | 1 MiB | ErrInputLimit before loading/scanning the engine |
| MaxMatches | 1,000 | At most this many entries; Truncated when another eligible match is observed |
| MaxCandidates | 100,000 | ErrScanWorkLimit when another raw automaton match is encountered |

Kind defaults to MatchKindOverlapping. MatchKindLeftmostLongest selects the
leftmost available start, then the longest keyword there, and discards overlaps.
The implementation keeps the longest candidate at each pending start until the
engine's longest keyword makes the decision safe. It does not collect all raw
matches and sort them. Scratch memory is bounded by input length plus the pending
start window and retained result count; it is not constant memory. Input indexing
uses rune and byte-offset arrays. A low result limit alone does not replace the
input-byte or candidate-work limit.

WholeWord and WordRune have the same normalized-rune boundary semantics as
MatchOptions. Letters, digits, combining marks and underscore are word characters
by default. Scripts without spaces may require an application-specific WordRune.
Candidate counting happens before whole-word and overlap filtering, so a dense
rejected-match workload cannot bypass the work budget. Custom WordRune code runs
in the caller's goroutine; its own resource use is the caller's responsibility.

Input or candidate exhaustion and cancellation return an error with no result.
Truncated concerns the eligible result count only. A caller checking safety or
completeness must handle both errors and Truncated; neither means a clean input.
The context is checked during input indexing, traversal and match handling.

## Atomic masking and literal replacement

`ReplaceText(ctx, text, replacement, *RewriteOptions)` inserts the literal
replacement for each selected non-overlapping leftmost-longest match. It does not
interpret regular expressions, expand captures, or search replacement text again.
An empty replacement deletes matched spans.

`MaskText(ctx, text, maskRune, *RewriteOptions)` writes one mask rune for every
matched original rune. The result preserves rune count, not necessarily byte count.
Any valid Unicode rune, including NUL, is accepted. Invalid runes are rejected.
Both APIs leave unmatched original bytes untouched.

RewriteOptions shares the three scan limits above and adds MaxOutputBytes, which
defaults to 4 MiB. WholeWord and WordRune are also available. Overlap selection is
always leftmost-longest. Exceeding MaxMatches produces ErrMatchLimit; exceeding the
output bound produces ErrOutputLimit. Every error returns no RewriteResult, so a
caller cannot accidentally consume a partially masked document. All output sizes
are checked before allocating the output buffer.

RewriteResult contains Text and the SourceMatch entries used for the rewrite.
Their offsets always refer to the **input**, even when replacement changes output
length. Match Text substrings can retain the original input string in memory.

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

See the [R2/R3 report](../r2-r3-performance/) for parity, resource-bound and
cancellation evidence and the same-environment R1 comparison.
