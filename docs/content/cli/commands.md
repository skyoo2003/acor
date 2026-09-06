---
title: "Commands"
weight: 1
---

# Commands

`acor --help` prints the command list and every flag with its default. This page covers the
behavior those one-line flag descriptions cannot carry: the ordering rule, what each batch
mode does on failure, how the matching commands differ from one another, and when the local
cache is worth its memory.

## Options come before the command

CLI options must appear before the command. Batch commands accept keywords as
arguments, or `-` as the only argument to read one keyword per line from stdin:

```bash
acor -addr localhost:6379 -batch-mode transactional add-many foo bar "hello world"
printf 'foo\nbar\n' | acor -addr localhost:6379 remove-many -
```

`best-effort` is the default and reports per-keyword failures in JSON while
returning success; `transactional` fails the command if the whole batch cannot
be committed.

## Matching

Beyond `find` and `find-index`, the matching commands cover the set, span, and
presence shapes of the same scan:

```bash
acor -addr localhost:6379 find-set "he is him"
acor -addr localhost:6379 contains "he is him"
acor -addr localhost:6379 -match-kind leftmost-longest -whole-word \
  find-matches "he is him"
```

`find-set` reports each keyword once, `contains` stops at the first match, and
`find-matches` reports each occurrence with its rune span in scan order.
`-match-kind` and `-whole-word` apply only to `find-matches`.

`-whole-word` assumes a script that separates words with spaces or punctuation.
In scripts written without inter-word boundaries (CJK, Thai, …) every adjacent
character counts as a word character, so nearly every match is treated as
mid-word and dropped — scan such text without `-whole-word`, or use the library's
`MatchOptions.WordRune` to supply your own boundary rule.

## Parallel matching

Parallel matching accepts a text argument, or `-` to read the complete text
from stdin. `word`, `sentence`, and `line` chunk boundaries are available:

```bash
acor -addr localhost:6379 -workers 8 -chunk-size 10000 \
  -boundary line -overlap 100 find-parallel - < large.log

acor -addr localhost:6379 -workers 8 \
  find-index-parallel - < document.txt
```

## Local cache and presets

Use `-cache` with the normal V2 engine, or select a Redis-backed local preset
engine. Preset mode already keeps a local engine, so `-cache` and `-preset`
cannot be combined:

```bash
acor -addr localhost:6379 -cache find-parallel - < document.txt

acor -addr localhost:6379 -preset balanced \
  -invalidation-poll-interval 30s find-parallel - < document.txt
```

Available presets are `speed`, `balanced`, and `memory-efficient`; `none` is
the compatibility-preserving default. Preset mode requires an explicit Redis
address and does not support `suggest`, `suggest-index`, or migration commands.
The local cache is most useful for parallel matching, where every chunk shares
one CLI process; a one-shot `find` invocation has no later lookup to reuse it.

The trade-offs behind each preset are in
[Guides → Preset-Optimized Engine](../../guides/preset-engine/).

## Navigation

← [CLI](../) | [Extending](../../extending/) →

## Versioned dictionaries

Use `acor -name new-v3-name dictionary list|diff|replace|status|copy-v2|prune`.
Replacement and copying require `--expected-version`; empty replacements and
empty V2 sources require `--allow-empty`. Diff and replace read a JSON string
array from stdin. See the [V3 guide](../../reference/versioned/) for pagination,
case policy, cutover and command examples.
