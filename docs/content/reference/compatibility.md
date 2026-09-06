---
title: "Compatibility"
weight: 2
---

# Compatibility

Code that imports `github.com/skyoo2003/acor/pkg/acor` keeps compiling, and keeps behaving
as documented, across every `v1.x.y` release. Nothing is removed from that surface inside
`v1`. Taking something back requires a `v2` import path, and no `v2` is scheduled.

`v1.5.0` is the first supported `v1` release and the baseline the promise is measured
from. `v1.0.0`–`v1.4.0` are retracted and were never covered — see
[Retracted versions](https://github.com/skyoo2003/acor/blob/main/RELEASE.md#retracted-versions).

## Upgrading from `v0.11.x` is the one exception

`v1.5.0` removes deprecated members and closes paths `v0.11.x` still allowed, so that one
upgrade can require changes to your code:

- **`PresetUltimate` is removed.** It aliased `PresetBalanced`; rename it.
- **`InMemoryInfo` is removed.** No exported function accepted or returned it.
- **V1 collections are read-only.** `Add` and `Remove` return `ErrV1ReadOnly`. Reads,
  `Suggest`, `Info`, and `MigrateV1ToV2` still work, so an existing V1 collection can be
  read and converted, but it takes no new keywords. `Flush` also still works — and still
  deletes every key. Read-only refuses keyword writes; it does not make the collection
  indestructible.

From `v0.10.x` or earlier, `v0.11.0`'s own breaking changes apply first — the `RemoveMany`
signature, the removal of `RemoveManyWithOptions`, and the removal of the exported V1 key
constants. Upgrade through `v0.11.x` and follow the
[changelog](https://github.com/skyoo2003/acor/blob/main/CHANGELOG.md); this page does not
restate them.

Every upgrade *after* `v1.5.0` is covered by everything below. Each of those three changes
would be a promise violation in `v1.6.0`; `v1.5.0` is the only release that could make
them.

## What is covered

| Surface | Covered by `v1` |
| ------- | --------------- |
| Exported identifiers of `pkg/acor` | ✅ |
| Documented behavior of those identifiers | ✅ |
| Sentinel error identity (`errors.Is`) | ✅ |
| On-Redis V2 data format | ✅ additions only |

Not covered:

| Surface | Why |
| ------- | --- |
| The `acor` CLI | Flags, output, and exit codes can change in any release. Call the library if you need a stable contract |
| `acor/server` | A separate, experimental module with no tags of its own; the core's version numbers say nothing about it |
| `internal/...` | Not importable, free to change |
| The `benchmarks` module | A measurement harness, not an API |
| Documentation wording | Pages get rewritten; what they describe is pinned by this page, not by their phrasing |
| The V1 schema layout | Deprecated and not evolved further — see [Deprecation](#deprecation) |

## Conditions on your code

The promise holds for code that follows all three. None is enforceable by the compiler.

### Construct option structs with field names

`AhoCorasickArgs`, `MatchOptions`, `BatchOptions`, `ParallelOptions`, and
`MigrationOptions` gain fields in minor releases. A keyed literal survives that:

<!-- doccheck -->
```go
args := &acor.AhoCorasickArgs{
    Addr:   "localhost:6379",
    Name:   "sample",
    Preset: acor.PresetBalanced,
}
_ = args
```

An unkeyed literal breaks the moment any field is added, and is not covered:

```go
// Not covered: positional fields break when a field is added.
opts := acor.MatchOptions{acor.MatchKindLeftmostLongest, true, nil}
```

`go vet`'s `composites` check reports unkeyed literals of another package's structs, so
this condition is verifiable in your own build.

The structs ACOR *returns* run the other way. `AhoCorasickInfo`, `MigrationResult`,
`BatchResult`, and `CacheStats` are built by ACOR and read by you, so a new field cannot
break a reader — and they do gain fields inside `v1`. The condition is only that you not
construct or whole-value compare one: assert on the fields you care about, and a later
release adding a sixth counter stays invisible.

Their JSON field names are covered too. `api/v1.txt` records struct tags, so renaming
`json:"status"` is a breaking change even though it moves no Go signature. That applies to
marshaling a returned struct yourself; the `acor` command's own `--json` output is a CLI
detail and stays uncovered.

### Do not expect the exported interface to grow

`Logger` is the one exported interface implementable from outside the module, and no
method is added to it inside `v1` — that would break every existing implementation.

`KVStorage`, `StringMapResult`, `Subscription`, and `Pipeliner` were exported through
`v1.4.0` and are not part of the `v1.5.0` surface. Nothing public accepted or returned
one, so no caller could supply an implementation; and freezing them would have capped the
pluggable-storage work they existed for, since this very rule forbids adding a method
later. They are unexported now so that feature can pick its own shape.

### Do not dot-import the package

`import . "github.com/skyoo2003/acor/pkg/acor"` puts every exported name into your file's
scope, so a name added in a minor release can collide with one of your declarations and
stop the file compiling. A normal import cannot: additions land behind the `acor.`
qualifier.

## The on-Redis data format

Several instances share one dictionary, so a rolling deploy runs two ACOR versions against
the same keys. Inside `v1`, changes to the V2 format are **additive only**:

- Key names, hash tags, and the names and meanings of existing fields do not change.
- New fields may be added to the `{name}:trie` hash or under a new key. A version that
  does not recognize a field there ignores it — that hash is read field by field, by name.
- Nothing is added to the `{name}:outputs` hash. Its field names are automaton states and
  every value is read as match data, so it has no room for metadata.

A mixed-version fleet therefore keeps working in both directions for the whole `v1` line,
and a rolling deploy needs no coordinated restart.

Two consequences worth planning for:

- `Flush` rewrites `{name}:trie` in full, so a `Flush` from an older instance drops fields
  a newer one added. The next write from an upgraded instance restores them.
- A feature depending on a newly added field does not take effect until every instance
  writing to the collection is upgraded. Expect it after the rollout, not during it.

## What counts as a breaking change

A patch release (`x.y.Z`) carries bug fixes and no surface change; a minor release
(`x.Y.0`) adds without breaking; a breaking change requires a new major version.

Removing or renaming an exported identifier, changing a signature, and narrowing a return
type are the obvious cases, and tooling catches them. These count too, and tooling catches
none of them:

- **Adding a field to an option struct**, for callers using unkeyed literals — hence the
  condition above.
- **No longer returning a sentinel error** for the situation that returns it today.
  `errors.Is(err, ErrCacheWithPreset)` continuing to hold is the promise; the variable
  merely continuing to exist is not enough.
- **Changing documented behavior** — match ordering, `MatchKind` semantics, `FindSet`'s
  first-match order, `FindParallel`'s deduplication contract.
- **Changing the meaning of an existing V2 field.**

Undocumented detail is not covered: sort order among equally-ranked matches, the exact
wording of an error string, allocation counts, and round-trip counts can change in any
release.

## How the promise is enforced

`api/v1.txt` is the covered surface, one symbol per line — functions, methods, struct
fields, interface methods. CI regenerates it and fails if the result differs from what is
committed, so a PR changing the public API has to change that file in the same diff: an
addition appends a line, a removal **deletes** one, in front of a reviewer. Nothing stops
a removal from being merged; what is gone is merging one unnoticed.

CI also fails if `retract [v1.0.0, v1.4.0]` leaves `go.mod`, because dropping it would
silently make the retracted range resolvable again.

Two clauses are not covered by that check:

- **Documented behavior.** No tool compares match ordering or `MatchKind` semantics
  *across versions* — the claim lives in prose. What is checked is that somebody looked:
  `api/v1-audit.txt` carries one verdict per entry of `api/v1.txt`, and CI fails when an
  entry has none. A verdict of `unaudited` is legal but counted, and the tally prints on
  every run. A non-`unaudited` verdict must cite the `file:line` the behavior was read at.

  Every entry of the `v1.5.0` surface was read against its code, and 38 said something the
  code did not do. Those sentences were rewritten before the freeze, so `v1` promises the
  corrected wording, not what shipped in `v1.4.0`. `v1.5.1` changed no entry of that
  surface — `git diff v1.5.0 v1.5.1 -- api/v1.txt` is empty, and the release touched only
  `LICENSE`, `server/LICENSE`, and the changelog.
- **Cross-version Redis interop.** Tests pin key names and hash field names, so a *rename*
  fails CI. They do not run an older ACOR against a newer one's data, so additive-only is
  verified as far as naming and no further.

## Deprecation

A deprecated identifier keeps working for the rest of `v1`. It carries a `Deprecated:`
line naming its replacement, gains no new capability, and is removed no earlier than `v2`.

Deprecated today: `SchemaV1`, read-only, whose read path is removed no earlier than `v2`.

## `v2`

A `v2` would ship as `github.com/skyoo2003/acor/v2`, imported as
`github.com/skyoo2003/acor/v2/pkg/acor`. `v1` stays importable and unchanged at its own
path, so publishing `v2` breaks nothing by itself. There is no schedule.

Security patches follow the
[security policy](https://github.com/skyoo2003/acor/blob/main/SECURITY.md).
