---
title: "Installation"
weight: 1
---

# Installation

## Prerequisites

- **Go**: Version 1.25 or later
- **Redis**: Version 3.0 or later, **or Valkey**: Version 7.2 or later

ACOR uses the standard RESP protocol via [go-redis v9](https://github.com/redis/go-redis) and works with any Redis- or Valkey-compatible server. RESP3 is negotiated on connect and falls back to RESP2 automatically on servers that predate the `HELLO` command.

## Install the Package

```bash
go get github.com/skyoo2003/acor/pkg/acor@latest
```

## Verify Installation

Create a test file to verify ACOR is installed correctly:

```go
package main

import (
    "fmt"
    "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
    args := &acor.AhoCorasickArgs{
        Addr: "localhost:6379",
        Name: "test",
    }

    ac, err := acor.Create(args)
    if err != nil {
        panic(err)
    }
    defer ac.Close()

    fmt.Println("ACOR installed successfully!")
}
```

## CLI Installation

Install the command-line tool:

```bash
go install github.com/skyoo2003/acor/cmd/acor@latest
```

Verify the CLI:

```bash
acor --help
acor version
```

`acor version` needs no Redis and prints the version stamped at release build
time (`dev` for a locally built binary).

CLI options must appear before the command. Batch commands accept keywords as
arguments, or `-` as the only argument to read one keyword per line from stdin:

```bash
acor -addr localhost:6379 -batch-mode transactional add-many foo bar "hello world"
printf 'foo\nbar\n' | acor -addr localhost:6379 remove-many -
```

`best-effort` is the default and reports per-keyword failures in JSON while
returning success; `transactional` fails the command if the whole batch cannot
be committed.

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

Parallel matching accepts a text argument, or `-` to read the complete text
from stdin. `word`, `sentence`, and `line` chunk boundaries are available:

```bash
acor -addr localhost:6379 -workers 8 -chunk-size 10000 \
  -boundary line -overlap 100 find-parallel - < large.log

acor -addr localhost:6379 -workers 8 \
  find-index-parallel - < document.txt
```

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

## Next Steps

- [Quick Start](../quick-start/) - Build your first application
- [Guides](../../guides/) - Learn about batch operations and parallel matching
