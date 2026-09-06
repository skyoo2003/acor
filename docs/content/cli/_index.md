---
title: "CLI"
weight: 6
---

# CLI

`acor` is the third way into the same collection: one binary, every command a shell over
the library. It is for the things a program should not have to be written for — seeding a
dictionary, checking what is in one, running a migration, grepping a log against keywords
that live in Redis.

> **The CLI is not covered by the `v1` compatibility promise.** Flags, output format, and
> exit codes can change in any release. If you need a stable contract, call the library
> rather than parsing CLI output. See
> [Compatibility](../reference/compatibility/#what-is-covered).

```bash
go install github.com/skyoo2003/acor/cmd/acor@latest
```

## The commands

| Group | Commands |
| ----- | -------- |
| Write | `add`, `add-many`, `remove`, `remove-many`, `flush` |
| Match | `find`, `find-index`, `find-set`, `find-matches`, `contains`, `find-parallel`, `find-index-parallel` |
| Suggest | `suggest`, `suggest-index` |
| Inspect | `info`, `schema-version`, `version` |
| Migrate | `migrate`, `migrate-rollback` |
| Dictionary (V3) | `dictionary list\|diff\|replace\|status\|copy-v2\|prune` |

Flags are deliberately not tabulated here. `acor --help` prints the command list followed
by the flag set's own defaults, so each flag's description lives exactly once — next to
where the flag is registered — and cannot drift out of step with a copy on this page.

`acor version` is the one command needing no Redis. It prints the version stamped in at
release build time, or `dev` for a binary you built yourself.

[Commands](commands/) covers what those one-line flag descriptions cannot carry: option
ordering, batch modes, the matching shapes, parallel chunking, and when the local cache
earns its memory.
