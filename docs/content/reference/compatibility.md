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

**Upgrading from `v0.11.x` is a one-time exception.** `v1.5.0` removes deprecated members
and closes paths that `v0.11.x` still allowed, so that upgrade can require changes to your
code. From `v0.10.x` or earlier, `v0.11.0`'s own breaking changes apply first — the
`RemoveMany` signature, the removal of `RemoveManyWithOptions`, and the removal of the
exported V1 key constants — so upgrade through `v0.11.x` and follow the
[changelog](https://github.com/skyoo2003/acor/blob/main/CHANGELOG.md); this page does not
restate them.

From `v0.11.x`, specifically:

- `PresetUltimate` is removed. It was an alias for `PresetBalanced`; rename it.
- `InMemoryInfo` is removed. No exported function accepted or returned it.
- **V1 collections are read-only.** `Add` and `Remove` return `ErrV1ReadOnly`. Reads,
  `Suggest`, `Info`, and `MigrateV1ToV2` still work, so an existing V1 collection can be
  read and converted, but it takes no new keywords. Migrate to V2. `Flush` also still
  works — and still deletes every key in the collection. Read-only means keyword writes
  are refused, not that the collection cannot be destroyed.

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
- **`acor/server`.** A separate, experimental module. It publishes no tags of its own, so
  it can only be required by pseudo-version, and the core's version numbers say nothing
  about it.
- **`internal/...`.** Not importable, and free to change in any release.
- **The `benchmarks` module.** A measurement harness, not an API.
- **Documentation wording.** Pages get rewritten. What they describe is pinned by this
  page, not by their phrasing.
- **The V1 schema layout.** Deprecated, and not evolved further. See
  [Deprecation](#deprecation).

## Conditions on your code

The promise holds for code that follows all three. None of them is something the compiler
can enforce on your behalf.

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
opts := acor.MatchOptions{acor.MatchKindLeftmostLongest, true, nil}
```

That literal compiles against `v1.5.0` and stops compiling the moment `MatchOptions` gains
a fourth field. `go vet`'s `composites` check reports unkeyed literals of another package's
structs, so this condition is verifiable in your own build.

The structs ACOR *returns* run the other way. `AhoCorasickInfo`, `MigrationResult`,
`BatchResult`, and `CacheStats` are built by ACOR and read by you, so a new field cannot
break a reader — and they do gain fields inside `v1`. The condition on them is only that
you not construct or whole-value compare one: assert on the fields you care about, and a
later release adding a sixth counter stays invisible to your code.

Their JSON field names are covered too. `api/v1.txt` records struct tags, so renaming
`json:"status"` is a breaking change even though it moves no Go signature. That applies
to marshaling a returned struct yourself; the `acor` command's own `--json` output is a
CLI detail and stays uncovered.

### Do not expect the exported interface to grow

One exported interface can be implemented from outside the module: `Logger`. No method
is added to it inside `v1` — doing so would break every existing implementation.

`KVStorage`, `StringMapResult`, `Subscription`, and `Pipeliner` were exported through
`v1.4.0` and are not part of the `v1.5.0` surface. Nothing public ever accepted or
returned one, so no caller could supply an implementation; and freezing them would have
capped the pluggable-storage work they existed for, since this very rule forbids adding
a method to them later. They are unexported now so that feature can choose its own
shape, which `v1` permits as an addition.

### Do not dot-import the package

`import . "github.com/skyoo2003/acor/pkg/acor"` puts every exported name into your file's
scope, so a name added in a minor release can collide with one of your own declarations and
stop the file compiling. A normal import cannot: additions land behind the `acor.`
qualifier, where they collide with nothing.

## The on-Redis data format

ACOR exists so that several instances share one dictionary, which means a rolling deploy
runs two ACOR versions against the same Redis keys at the same time. The Go API promise
says nothing about that case. This section does.

Inside `v1`, changes to the V2 format are **additive only**:

- Key names, hash tags, and the names and meanings of existing fields do not change.
- New fields may be added to the `{name}:trie` hash, or under a new key. An ACOR version
  that does not recognize a field there ignores it: that hash is read by looking its
  fields up by name.
- Nothing is added to the `{name}:outputs` hash. Its field names are automaton states and
  every value in it is read as match data, so it has no room for metadata.
- A mixed-version fleet therefore keeps working in both directions for the whole `v1`
  line, and a rolling deploy needs no coordinated restart.

`Flush` rewrites `{name}:trie` in full, so a `Flush` issued by an older instance drops
fields a newer one added. The next write from an upgraded instance restores them.

The cost lands on new features rather than on availability: a feature that depends on a
newly added field does not take effect until every instance writing to the collection is
upgraded. Expect it to appear after the rollout finishes, not during it.

## What counts as a breaking change

A patch release (`x.y.Z`) carries bug fixes and no surface change; a minor release
(`x.Y.0`) adds to the surface without breaking it; a breaking change requires a new major
version.

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

## How the promise is enforced

Most of this page is checked by machine rather than by a reviewer noticing.

`api/v1.txt` in the repository is the covered surface, one symbol per line —
functions, methods, struct fields, and interface methods. CI regenerates it and fails
if the result differs from what is committed. A pull request that changes the public
API therefore has to change that file in the same diff: an addition appends a line, and
a removal **deletes** one, in front of a reviewer. Nothing stops a removal from being
merged; what is gone is the possibility of merging one unnoticed.

Two clauses on this page are not covered by that, and both are stated here rather than
left to be discovered:

- **Documented behavior.** Match ordering, `MatchKind` semantics, `FindSet`'s
  first-match order — no tool compares these *across versions*, and none can: the
  claim lives in prose. What is checked is that somebody looked. `api/v1-audit.txt`
  carries one verdict per entry of `api/v1.txt`, and CI fails when an entry has none,
  so a *missing* verdict is a build failure rather than an omission nobody notices.
  A verdict of `unaudited` is legal, so an unreviewed entry does not fail the build —
  it is counted instead, and the tally prints on every run. Whether each verdict is
  *right*, and whether its cited `file:line` still points where it did, is review.

  Every entry of the `v1.5.0` surface has been read against the code it describes,
  and 38 of them said something the code did not do. Those sentences were rewritten
  before the freeze, so what `v1` promises is the corrected wording, not the wording
  that shipped in `v1.4.0`. A non-`unaudited` verdict has to cite the `file:line`
  the behavior was read at, which is what stops a future pass from marking a line
  reviewed without reviewing it.

  Those verdicts still describe the code as shipped. `v1.5.1` changed no entry of
  the surface they were measured against: `git diff v1.5.0 v1.5.1 -- api/v1.txt` is
  empty, and the release touched only `LICENSE`, `server/LICENSE`, and the changelog —
  no Go file at all.
- **Cross-version Redis interop.** Tests pin the key names and hash field names, so a
  *rename* fails CI. They do not run an older ACOR against a newer one's data, so
  additive-only is verified as far as naming and no further.

Retractions are checked too: CI fails if `retract [v1.0.0, v1.4.0]` leaves `go.mod`,
because a release without it silently makes the retracted range resolvable again.

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
which defines which lines receive them.
