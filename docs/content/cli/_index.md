---
title: "CLI"
weight: 6
---

# CLI

`acor` is the third way into the same collection: one binary, nineteen commands, every one of
them a shell over the library. It is the entry point for the things a program should not have
to be written for — seeding a dictionary, checking what is in one, running a migration,
grepping a log against keywords that live in Redis.

> **The CLI is not covered by the `v1` compatibility promise.** Quoting
> [Compatibility](../reference/compatibility/#what-is-not-covered) directly:
>
> **The CLI.** Flags, output format, and exit codes of the `acor` command can change in
> any release. If you need a stable contract, call the library rather than parsing CLI
> output.
>
> The library (`pkg/acor`) is covered; this is the surface that is not.

## Install

```bash
go install github.com/skyoo2003/acor/cmd/acor@latest
```

Full instructions, including verifying the install, are on
[Getting Started → Installation](../getting-started/installation/#cli-installation).

## The nineteen commands

| Group | Commands |
| ----- | -------- |
| Write | `add`, `add-many`, `remove`, `remove-many`, `flush` |
| Match | `find`, `find-index`, `find-set`, `find-matches`, `contains`, `find-parallel`, `find-index-parallel` |
| Suggest | `suggest`, `suggest-index` |
| Inspect | `info`, `schema-version`, `version` |
| Migrate | `migrate`, `migrate-rollback` |

Flags are deliberately not tabulated here. `acor --help` prints the command list followed by
the flag set's own defaults, so each flag's description lives exactly once — next to where the
flag is registered — and cannot drift out of step with a copy on this page.

`acor version` is the one command that needs no Redis. It prints the version stamped in at
release build time, or `dev` for a binary you built yourself.

## Sections

- [Commands](commands/) - Option ordering, batch modes, the four matching shapes, parallel chunking, and when the local cache earns its memory

## Navigation

← [Server](../server/) | [Extending](../extending/) →
