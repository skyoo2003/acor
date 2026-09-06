---
title: "Commands"
weight: 1
---

# Commands

`acor --help` prints every command and flag with its default. This page covers what those
one-line descriptions cannot carry.

## Options come before the command

Batch commands take keywords as arguments, or `-` as the only argument to read one keyword
per line from stdin:

```bash
acor -addr localhost:6379 -batch-mode transactional add-many foo bar "hello world"
printf 'foo\nbar\n' | acor -addr localhost:6379 remove-many -
```

`best-effort` is the default: it reports per-keyword failures in JSON and still exits
successfully. `transactional` fails the command if the whole batch cannot be committed.

## Matching

```bash
acor -addr localhost:6379 find-set "he is him"
acor -addr localhost:6379 contains "he is him"
acor -addr localhost:6379 -match-kind leftmost-longest -whole-word \
  find-matches "he is him"
```

`find-set` reports each keyword once, `contains` stops at the first match, and
`find-matches` reports each occurrence with its rune span in scan order. `-match-kind` and
`-whole-word` apply to `find-matches` only.

`-whole-word` assumes a script that separates words with spaces or punctuation. In scripts
written without word boundaries (CJK, Thai, …) every adjacent character counts as a word
character, so nearly every match is treated as mid-word and dropped — scan such text
without `-whole-word`, or use the library's `MatchOptions.WordRune`.

## Parallel matching

Takes a text argument, or `-` to read the whole text from stdin:

```bash
acor -addr localhost:6379 -workers 8 -chunk-size 10000 \
  -boundary line -overlap 100 find-parallel - < large.log

acor -addr localhost:6379 -workers 8 \
  find-index-parallel - < document.txt
```

`-boundary` takes `word`, `sentence`, or `line`.

## Local cache and presets

`-cache` enables the local cache on the normal V2 engine; `-preset` selects a Redis-backed
local engine instead. Preset mode already keeps a local engine, so the two cannot be
combined:

```bash
acor -addr localhost:6379 -cache find-parallel - < document.txt

acor -addr localhost:6379 -preset balanced \
  -invalidation-poll-interval 30s find-parallel - < document.txt
```

Presets are `speed`, `balanced`, and `memory-efficient`; `none` is the
compatibility-preserving default. Preset mode requires an explicit Redis address and
supports neither `suggest`/`suggest-index` nor the migration commands.

The cache pays off for parallel matching, where every chunk shares one CLI process. A
one-shot `find` has no later lookup to reuse it. Preset trade-offs are in
[Guides → Preset-Optimized Engine](../../guides/preset-engine/).

## Versioned dictionaries

```bash
acor -name new-v3-name dictionary list|diff|replace|status|copy-v2|prune
```

Replacement and copying require `--expected-version`; empty replacements and empty V2
sources require `--allow-empty`. Diff and replace read a JSON string array from stdin.
Pagination, case policy, and cutover: [V3 guide](../../reference/versioned/#cli).
