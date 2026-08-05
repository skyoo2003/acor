---
title: "Compatibility"
weight: 2
---

# Compatibility

Code that imports `github.com/skyoo2003/acor/pkg/acor` keeps compiling, and keeps
behaving as documented, across every `v1.x.y` release. Nothing is removed from that
surface inside `v1`. Taking something back requires a `v2` import path, and no `v2` is
scheduled.

`v1.5.0` is the first supported `v1` release, and the promise is measured from its
surface. `v1.0.0`-`v1.4.0` are retracted and were never covered — see
[Retracted versions](https://github.com/skyoo2003/acor/blob/main/RELEASE.md#retracted-versions)
for why the numbering starts there.

**Upgrading from `v0.x` is a one-time exception.** `v1.5.0` removes deprecated members and
closes paths that `v0.11.x` still allowed, so that upgrade can require changes to your
code. Specifically:

- `PresetUltimate` is removed. It was an alias for `PresetBalanced`; rename it.
- `InMemoryInfo` is removed. No exported function accepted or returned it.
- **V1 collections are read-only.** `Add` and `Remove` return `ErrV1ReadOnly`. Reads,
  `Suggest`, `Info`, `Flush`, and `MigrateV1ToV2` still work, so an existing V1
  collection can be read and converted, but it takes no new keywords. Migrate to V2.

Every upgrade *after* `v1.5.0`, within `v1`, is covered by everything below. Each of
those three would be a promise violation in `v1.6.0`; `v1.5.0` is the only release that
can make them.

## What is covered

| Surface                                  | Covered by `v1`   |
| ---------------------------------------- | ----------------- |
| Exported identifiers of `pkg/acor`       | ✅                |
| Documented behavior of those identifiers | ✅                |
| Sentinel error identity (`errors.Is`)    | ✅                |
| On-Redis V2 data format                  | ✅ additions only |
| Everything in the next section           | ❌                |

## What is not covered

- **The CLI.** Flags, output format, and exit codes of the `acor` command can change in
  any release. If you need a stable contract, call the library rather than parsing CLI
  output.
- **`acor/server`.** A separate, experimental module that publishes no tags of its own,
  versioned independently of the core.
- **`internal/...`.** Not importable, and free to change in any release.
- **The `benchmarks` module.** A measurement harness, not an API.
- **Documentation wording.** Pages get rewritten. What they describe is pinned by this
  page, not by their phrasing.
- **The V1 schema layout.** Deprecated, and not evolved further. See
  [Deprecation](#deprecation).

## Two conditions on your code

The promise holds for code that follows both. Neither is something the compiler can
enforce on your behalf.

### Construct option structs with field names

`AhoCorasickArgs`, `MatchOptions`, `BatchOptions`, `ParallelOptions`, and
`MigrationOptions` gain fields in minor releases. That is not a breaking change for a
keyed literal:

<!-- doccheck -->
```go
args := &acor.AhoCorasickArgs{
    Addr:   "localhost:6379",
    Name:   "sample",
    Preset: acor.PresetBalanced,
}
_ = args
```

An unkeyed literal breaks the moment any field is added, and is not covered by this
promise:

```go
// Not covered: positional fields break when a field is added.
args := &acor.AhoCorasickArgs{"localhost:6379", "sample"}
```

`go vet`'s `composites` check reports unkeyed literals of another package's structs, so
this condition is verifiable in your own build.

### Do not expect the exported interfaces to grow

Five exported interfaces can be implemented from outside the module: `Logger`,
`KVStorage`, `StringMapResult`, `Subscription`, and `Pipeliner`. No method is added to
any of them inside `v1` — doing so would break every existing implementation.

## The on-Redis data format

ACOR exists so that several instances share one dictionary, which means a rolling deploy
runs two ACOR versions against the same Redis keys at the same time. The Go API promise
says nothing about that case. This section does.

Inside `v1`, changes to the V2 format are **additive only**:

- Key names, hash tags, and the names and meanings of existing fields do not change.
- New fields may be added. An ACOR version that does not recognize a field ignores it.
- A mixed-version fleet therefore keeps working in both directions for the whole `v1`
  line, and a rolling deploy needs no coordinated restart.

The cost lands on new features rather than on availability: a feature that depends on a
newly added field does not take effect until every instance writing to the collection is
upgraded. Expect it to appear after the rollout finishes, not during it.

## What counts as a breaking change

Removing or renaming an exported identifier, changing a signature, and narrowing a return
type are the obvious cases, and tooling catches them. These count too, and tooling does
not catch any of them:

- **Adding a field to an option struct**, for callers using unkeyed literals — which is
  why the condition above exists.
- **No longer returning a sentinel error** for the situation that returns it today.
  `errors.Is(err, ErrCacheWithPreset)` continuing to hold is part of the promise; the
  variable merely continuing to exist is not enough.
- **Changing documented behavior** — match ordering, `MatchKind` semantics, `FindSet`'s
  first-match order, `FindParallel`'s deduplication contract.
- **Changing the meaning of an existing V2 field**, per the section above.

Undocumented detail is not covered. Sort order among equally-ranked matches, the exact
wording of an error string, allocation counts, and round-trip counts can change in any
release.

## Deprecation

A deprecated identifier keeps working for the rest of `v1`. It carries a `Deprecated:`
line naming its replacement, gains no new capability, and is removed no earlier than
`v2`.

Deprecated today: `SchemaV1`, which is read-only and whose read path is removed no
earlier than `v2`.

## `v2`

A `v2` would ship as the module `github.com/skyoo2003/acor/v2`, imported as
`github.com/skyoo2003/acor/v2/pkg/acor`. `v1` stays importable and unchanged at its own
path, so nothing breaks by the act of publishing `v2`. There is no schedule.

Security patches follow the [security policy](https://github.com/skyoo2003/acor/blob/main/SECURITY.md),
which covers the latest released minor line only.
