# Changelog
All notable changes to this project will be documented in this file.

## [v1.5.1](https://github.com/skyoo2003/acor/releases/tag/v1.5.1) - 2026-08-09

### Fixed

* Restored the verbatim Apache-2.0 text in `LICENSE`, so pkg.go.dev detects the license instead of reporting `License: UNKNOWN` and hiding the package documentation behind "Documentation not displayed due to license restrictions". The committed text had drifted from the canonical wording in three places — `submitted to the Licensor`, `received by the Licensor`, and `excluding any notices` — and the `APPENDIX` section had been dropped. pkg.go.dev matches the file against a fixed pattern with `github.com/google/licensecheck`, so a single substituted word breaks the match and the coverage falls under the 75% threshold the site requires. `server/` is a separate module whose zip does not carry the parent directory's files, so it gets its own copy as a regular file: the module zip builder skips symlinks, and a link would have left that module unlicensed. Per-version license data is immutable, so v1.5.0 stays `UNKNOWN`; this takes effect from the next tag. ([#217](https://github.com/skyoo2003/acor/issues/217))
## [v1.5.0](https://github.com/skyoo2003/acor/releases/tag/v1.5.0) - 2026-08-09

### Added

* `AhoCorasick.CacheStats()` reports the local cache hit rate, rebuild cost, and the lag of the last peer invalidation, with no Redis I/O. Metrics were previously exposed only by the experimental `server/` module, so a library user could observe the cache only by inferring it from latency. Counters are per instance and per process. `CacheStats` is only returned, never constructed by callers, so it may gain fields within v1. ([#202](https://github.com/skyoo2003/acor/issues/202))
* CI verifies that the `retract [v1.0.0, v1.4.0]` block is still present in `go.mod`, so a release cannot silently un-retract the withdrawn version range. ([#203](https://github.com/skyoo2003/acor/issues/203))
* CI checks the public API surface of `pkg/acor` against a committed snapshot (`api/v1.txt`), so a breaking change must be recorded in the diff that introduces it. `make api-check` runs the same check locally. ([#203](https://github.com/skyoo2003/acor/issues/203))
* `NOTICE` now lists the copyright notice and full license text of every third-party module linked into the `acor` binary, and ships in the release archives and container images. `make license-check` regenerates it from the module graph and CI verifies the result, so a dependency change cannot silently drop an attribution. ([#199](https://github.com/skyoo2003/acor/issues/199))

### Changed

* Every public type that was an alias into an internal package is now declared in `pkg/acor` itself: `KVStorage`, `Z`, `Pipeliner`, `Subscription`, `StringMapResult`, `PubSubMessage`, `Preset`, and `Match`. The `internal/storage` package is gone. Method sets and struct fields are unchanged and Go satisfies interfaces structurally, so custom `KVStorage` backends and `Match` consumers need no changes. Internal refactors can no longer change the public API by accident. ([#201](https://github.com/skyoo2003/acor/issues/201))
* V1 collections are now read-only. `Add` and `Remove` return the new `ErrV1ReadOnly` sentinel; `Find`, `FindIndex`, `Suggest`, `Info`, `Flush`, and `MigrateV1ToV2` are unchanged, so an existing V1 collection can still be read and converted in place. Creating a collection with `SchemaVersion: SchemaV1` yields one that can never gain a keyword. Batch calls report the refusal per keyword in `BatchResult.Failed`, as they do for any per-keyword failure. This removes the cost of keeping two write paths correct; the V1 read path remains for the whole v1 line and is removed no earlier than v2. ([#201](https://github.com/skyoo2003/acor/issues/201))
* FindSet dedups by keyword id instead of by string: each engine state now carries a 4-byte pattern id with the keyword strings interned once, and the unique-set collector is a bitset test per state and per reported keyword — switching to hash maps sized by hits once the dictionary passes a million table slots, so a million-pattern query allocates 384 B instead of 262 KB. At 1000 keywords over match-dense text this makes FindSet 1.4-3.2x faster and cuts per-query allocations from 178 KB/44 to 35 KB/10; a suffix-nested dictionary over repeated text drops from 2 ms to 0.4 ms (Speed) and from 578 ms to 0.7 ms (MemoryEfficient). Sparse text is unchanged. No public API change. ([#215](https://github.com/skyoo2003/acor/issues/215))

### Deprecated

* Retracted `v1.0.0`-`v1.4.0`. Their tags were deleted long ago, but the module proxy caches versions permanently, so `go get github.com/skyoo2003/acor` still resolved to `v1.4.0` instead of the supported line. This release is the first supported v1, which is why the numbering jumps from v0.11.x. Existing pins to a retracted version keep building and are reported by `go list -m -u`. ([#198](https://github.com/skyoo2003/acor/issues/198))

### Removed

* Removed `acor.InMemoryInfo`, a field-for-field duplicate of `AhoCorasickInfo` that no exported function accepted or returned. ([#201](https://github.com/skyoo2003/acor/issues/201))
* Removed `PresetUltimate`. It had been a deprecated alias for `PresetBalanced` since v0.11.0, so switching to `PresetBalanced` is a rename with identical behavior. ([#201](https://github.com/skyoo2003/acor/issues/201))
* Unexported the storage abstraction — `KVStorage`, `Pipeliner`, `Subscription`, `StringMapResult`, `PubSubMessage`, and `Z` — removing 43 of the 223 entries on the frozen v1 surface. No exported function accepted or returned one, so the interfaces could be named but never supplied. Freezing them would also have capped the pluggable-backend work they were exported for: the compatibility policy forbids adding a method to an exported interface within v1, and `KVStorage` has 23. Unexported, their shape stays open until that feature settles it, and publishing an interface later is an addition v1 allows. No behavior changed and the Redis backend is unaffected; for testing without Redis, use miniredis as the ACOR test suite does. ([#209](https://github.com/skyoo2003/acor/issues/209))

### Fixed

* `FindParallel`, `FindIndexParallel`, and `FindMany` now cost one Redis round trip per call instead of one per chunk or per text. Every chunk went through `Find`, which reloads the automaton, so a 63-chunk text issued 63 `HGETALL` calls against the full outputs hash: roughly 1000 concurrent reads for a 1 MB input at the default chunk size, where a serial `Find` costs one at any size. The automaton is now loaded once per call and every chunk is scanned against that snapshot, so all chunks also see a consistent dictionary. `CacheStats` records one read per call accordingly. ([#206](https://github.com/skyoo2003/acor/issues/206))
* Corrected thirteen godoc claims on the frozen v1 surface that did not match the code. `ParallelOptions.ChunkSize` documented a fallback to `DefaultChunkSize`, but a caller-built struct that leaves it unset gets `ErrInvalidChunkSize`. `ParallelOptions.Overlap` documented `DefaultOverlap` the same way but stays at zero, silently missing keywords that straddle a chunk boundary; only `DefaultParallelOptions()` supplies either default. `BatchOptions.Mode` and `BatchModeBestEffort` described the mode as defaulting "if nil", but `BatchMode` is an int: unset means the zero value, and a nil `*BatchOptions` is a separate case. `BatchResult.Skipped` also covers keywords a batch found already present, not just duplicates within the input. `AhoCorasick.Debug()` writes through the configured `Logger` rather than stdout, so a default instance produces no output. `AhoCorasick.Info()` does not return the schema version; `SchemaVersion()` does. `Add`/`Remove` treat an empty keyword as a no-op returning `(0, nil)`. `ErrEmptyKeyword` is returned only by the batch forms. `ErrConcurrencyConflict` surfaces only after the internal retry with backoff is exhausted, so an immediate caller-side retry is wrong. `CacheStats.Misses` counts a failed Redis fetch as a miss for Preset and cached V2, but not for default V2 or V1. The `KVStorage` godoc offered mock implementations for testing, but nothing on the public surface accepts one. No signature, sentinel identity, or behavior changed; `api/v1.txt` is unchanged. ([#208](https://github.com/skyoo2003/acor/issues/208))
* Completed the godoc audit of the frozen v1 surface: all 180 entries are now checked against the code, and this pass corrected the remaining claims. Topology: `Addr` is not "ignored if Addrs is set" — setting both returns `ErrRedisConflictingTopology`; and a single-element `Addrs` already selects cluster mode, not "multiple entries", so a one-address setup with a `DB` fails with `ErrRedisClusterDB` where the same address in `Addr` would not. Schema: V2 was documented as three Redis keys, but only `MigrateV1ToV2` writes `{name}:nodes`, so a natively built collection has two and a fresh one has one. Migration: `MigrationResult.RolledBack` is never set and is always false; `KeysBefore` is an estimate (`Prefixes + Keywords + 2`) that reported 17 for a collection holding 13; `KeysAfter` is a constant, not a count; `DurationMs` stays 0 on the error paths; `Progress` reports 4/5 on a dry run and never reaches `done == total`; and `DryRun` still takes the migration lock. `RollbackToV1` documented the keywords lost but not that the collection becomes read-only afterwards, making it one-way. `FlushContext` does not propagate cancellation on V1 — it runs on a fresh context bounded by `RollbackTimeout` — and `RollbackTimeout` documented only a path `Add` can no longer reach. Newly documented: `Suggest`/`SuggestIndex` return `ErrSuggestRequiresRedis` in Preset mode, `FindMany` discards earlier results on the first error, `AddMany`/`RemoveMany` screen duplicates on the normalized keyword (so "Foo" and "foo" are one), and `AhoCorasickArgs.Debug` is ignored when a `Logger` is also set. No signature, sentinel identity, or behavior changed; `api/v1.txt` is unchanged. ([#211](https://github.com/skyoo2003/acor/issues/211))
* Fixed a lost update in Preset mode. A single `Add` or `Remove` rebuilt the local automaton from an incrementally maintained keyword set instead of the snapshot it had just committed, then cleared the stale flag and advanced the local version, so a keyword written by a peer disappeared from local `Find` results and was never refetched because the instance looked up to date. Single writes now apply the committed snapshot, as batch writes already did. ([#207](https://github.com/skyoo2003/acor/issues/207))
* `Create` now rejects `RingAddrs` combined with `Addrs` instead of silently building a ring client and dropping `Addrs`. The guard tested the merged address list for more than one entry, encoding the same "cluster needs multiple entries" assumption the godoc audit corrected: a one-element `Addrs` beside `RingAddrs` slipped through and connected the caller to ring shards they had not asked for, while a two-element list already returned `ErrRedisConflictingTopology`. It now tests `Addrs` the way `selectsCluster` does, so both cases are rejected alike. `Addr` beside `RingAddrs` is still accepted with `Addr` ignored, as documented. ([#211](https://github.com/skyoo2003/acor/issues/211))

### Documentation

* Documented the v1 compatibility promise: the exported API of `pkg/acor`, its documented behavior, sentinel error identity, and additive-only changes to the on-Redis V2 format are covered for every `v1.x.y` release, so mixed-version fleets stay safe during a rolling deploy. The CLI contract, the experimental `acor/server` module, and `internal/...` are excluded. Callers must construct option structs with field names and must not dot-import the package, and the five exported interfaces gain no methods within v1. ([#200](https://github.com/skyoo2003/acor/issues/200))
* Rewrote the monitoring page to separate the core library from the experimental `server/` module, and documented how to read the new cache statistics, including why `Rebuilds` does not equal `Misses` and why `LastInvalidationLag` includes clock skew. ([#202](https://github.com/skyoo2003/acor/issues/202))

## [v0.11.0](https://github.com/skyoo2003/acor/releases/tag/v0.11.0) - 2026-08-05

### Added

* Homebrew installation via `brew install skyoo2003/tap/acor`, published to the tap on each release ([#181](https://github.com/skyoo2003/acor/issues/181))
* `FindSet`/`FindSetContext` return each matched keyword once in first-match order. `Find` remains unchanged and reports every occurrence ([#188](https://github.com/skyoo2003/acor/issues/188))
* `FindMatchesAppend` appends matches to a caller-supplied buffer, allowing the result slice to be reused across scans ([#188](https://github.com/skyoo2003/acor/issues/188))
* Expose batch operations, parallel matching, Redis-backed engine presets, and local caching through the `acor` CLI ([#195](https://github.com/skyoo2003/acor/issues/195))
* `CreateContext` constructs an instance with a context bounding the setup I/O (schema check, initial keyword load, Pub/Sub subscribe). The context does not bound the instance's lifetime, so canceling it cannot silently stop the invalidation listener ([#196](https://github.com/skyoo2003/acor/issues/196))
* CLI commands `version`, `find-set`, `find-matches`, and `contains`, with `-match-kind` and `-whole-word` for `find-matches`. `acor version` reports the version stamped into release builds and needs no Redis ([#196](https://github.com/skyoo2003/acor/issues/196))

### Changed

* Improved `FindMatches` and `Contains` performance by scanning input strings directly and preallocating a small result buffer. `FindStream` retains its streaming path for `io.Reader` inputs ([#188](https://github.com/skyoo2003/acor/issues/188))
* Improved leftmost-longest `FindMatches` sorting and selection to reduce time and allocations. `Find` and `FindSet` also avoid allocating results when no keywords match ([#188](https://github.com/skyoo2003/acor/issues/188))
* Flattened and packed the DFA transition tables to reduce per-character lookup overhead across presets. `Find`, `FindSet`, and `Contains` also scan raw bytes for ASCII-only dictionaries, improving common matching paths ([#187](https://github.com/skyoo2003/acor/issues/187))
* `PresetUltimate` is now a deprecated alias for `PresetBalanced`. Its Bloom pre-filter was removed because it was 1.7-1.8x slower and prevented the ASCII byte-scan fast path. Existing code remains compatible and now uses the faster engine, while `PresetBalanced` also benefits from lower scan overhead. `PresetMemoryEfficient` retains its Bloom pre-filter. ([#186](https://github.com/skyoo2003/acor/issues/186))
* Every preset now stores each keyword once and follows output links for suffix matches. This keeps output storage linear for suffix-nested dictionaries while preserving match results and order ([#187](https://github.com/skyoo2003/acor/issues/187))
* Improved `Contains` by scanning directly and stopping at the first match, and optimized `FindSet` deduplication to reduce scan time and allocations ([#188](https://github.com/skyoo2003/acor/issues/188))
* `RemoveMany` now takes `*BatchOptions` like `AddMany`, and `RemoveManyWithOptions` is removed. Replace `RemoveMany(kw)` with `RemoveMany(kw, nil)` and `RemoveManyWithOptions(kw, opts)` with `RemoveMany(kw, opts)` ([#196](https://github.com/skyoo2003/acor/issues/196))

### Deprecated

* `SchemaV1` is deprecated. V1 collections stay readable and writable and `MigrateV1ToV2` converts them in place, but V1 gains no new features and support for it may be removed in a future major version ([#196](https://github.com/skyoo2003/acor/issues/196))

### Removed

* The exported V1 key-format constants `KeywordKey`, `PrefixKey`, `SuffixKey`, `OutputKey`, and `NodeKey`. They never produced the keys ACOR writes (a real key is hash-tagged, `{name}:keyword`) and nothing read them ([#196](https://github.com/skyoo2003/acor/issues/196))

### Fixed

* Uncached V2 `Find()` no longer rebuilds the match engine on every call and reads only the outputs hash. At 1000 keywords: 1,163,098 -> 221,253 ns/op and 11,704 -> 2,063 allocs/op, taking uncached V2 from ~9x slower than V1 to ~1.7x. Freshness is unchanged - the read still happens on every call ([#183](https://github.com/skyoo2003/acor/issues/183))
* `AddMany` and `RemoveMany` now process V2 and preset batches in one pass and commit them with a single atomic transaction, avoiding per-keyword trie rewrites. Adding 1,000 keywords is about 400x faster with 970x fewer allocations, while server round trips remain fixed at two regardless of batch size. `computeOutputs` also reuses string suffixes to reduce allocations. V1 behavior is unchanged ([#185](https://github.com/skyoo2003/acor/issues/185))
* `Info().TrieDepth` now reports the correct value for `PresetSpeed` and is recomputed on every rebuild ([#187](https://github.com/skyoo2003/acor/issues/187))
* `AddMany` and `RemoveMany` now detect case-insensitive duplicates using normalized keywords in V2 and preset collections. Inputs such as `["Foo", "foo"]` now report one item as Added and the duplicate as Skipped, matching V1 and single-keyword operations ([#185](https://github.com/skyoo2003/acor/issues/185))
* `MigrateV1ToV2` and `RollbackToV1` no longer panic with a nil pointer dereference when called on a Preset-mode instance. They return the new `ErrMigrationRequiresRedis` instead, matching the check the CLI already performed ([#196](https://github.com/skyoo2003/acor/issues/196))
* `Create` now rejects `EnableCache` combined with a `Preset` (`ErrCacheWithPreset`) instead of silently ignoring the cache setting. Preset mode already serves reads from a local engine ([#196](https://github.com/skyoo2003/acor/issues/196))
* `Create(nil)` now returns the new `ErrNilArgs` instead of panicking with a nil pointer dereference. `CreateContext` shares the guard ([#196](https://github.com/skyoo2003/acor/issues/196))
* The `acor` CLI now rejects `suggest` and `suggest-index` under `-preset` with the usage exit code, instead of connecting, loading the whole dictionary, and then failing with `ErrSuggestRequiresRedis`. `acor version` also no longer ignores stray arguments and inapplicable flags ([#196](https://github.com/skyoo2003/acor/issues/196))

### Documentation

* Replaced the published performance claims with measured values and a reproducible benchmarks page. Both V1 and V2 `Find()` cost 1 round trip, and the "50-60x faster Find()" belongs to `EnableCache`/`Preset`, not the V2 schema. Round-trip counts are now enforced by tests on every CI run ([#182](https://github.com/skyoo2003/acor/issues/182))
* Corrected the security policy, which listed supported v1.x/v2.x lines that were never released, and labeled the `acor/server` module experimental and separately versioned. Documented the repository's module layout and why the library import path keeps its `pkg/` segment ([#196](https://github.com/skyoo2003/acor/issues/196))

## [v0.10.1](https://github.com/skyoo2003/acor/releases/tag/v0.10.1) - 2026-07-30

### Changed

* The V2 trie hash no longer stores a `suffixes` field. It held every prefix reversed, was rewritten on each add and remove, and was never read by any matcher — only the V1 schema's suffix sorted set is used, for its own rebuild walk. Existing collections are unaffected: a leftover `suffixes` field is ignored on read and disappears on the next `Flush`, so old and new binaries can run against the same collection during a rolling upgrade.

The two V2 Lua scripts were also merged into one, differing only by a flag for whether the outputs hash is cleared first. ([#170](https://github.com/skyoo2003/acor/issues/170))
* V1 `Find` and `FindIndex` now scan the keyword set with the same in-memory automaton the other modes use instead of walking the trie in Redis one character at a time: a single `SMEMBERS` per call rather than two to three commands per rune, with identical results (pinned in `v1_engine_parity_test.go`). The automaton is reused while the set is unchanged, but the set is re-read on every call, so another instance's write is still picked up immediately. ([#173](https://github.com/skyoo2003/acor/issues/173))
* `logging.NewLogger` now accepts every level `zerolog.ParseLevel` understands — `trace`, `fatal`, `panic` and `disabled` in addition to `debug`/`info`/`warn`/`error`. Those extra names previously fell through to `info`. Unrecognized and empty values still default to `info`.

The `acor` CLI help now renders its option list from the flag definitions instead of a hand-maintained copy, so the two can no longer disagree. The `-dry-run` and `-keep-old-keys` entries are labelled `migrate:` rather than sitting under a separate heading. ([#174](https://github.com/skyoo2003/acor/issues/174))

### Fixed

* `FindParallelContext` now deduplicates its results for short texts, matching `FindParallel` and the documented contract that each keyword appears at most once. Previously a text that fit in a single chunk took a shortcut that returned every occurrence, so the result shape depended on the input length. ([#172](https://github.com/skyoo2003/acor/issues/172))
* The `Preset` modes now honor a canceled or expired context in `FindContext`, `FindIndexContext` and their parallel variants. They match against a local in-memory engine, so once the engine was warm no Redis call was made and the context was never consulted: a call with an already-canceled context ran the full scan and returned complete results with a nil error. They now return the context error, matching the V2 schema mode. ([#172](https://github.com/skyoo2003/acor/issues/172))
* An `Addrs` list of only blank entries no longer connects to localhost. go-redis substitutes `127.0.0.1:6379` / `127.0.0.1:26379` for an empty address list; this now returns the new `ErrRedisAddrs`.

`ErrRedisClusterDB` is now returned whenever cluster mode is selected. The check required more than one address, so a single-entry `Addrs` with `DB` set had its `DB` silently dropped to 0.

`logging.NewLogger` range-checks the level from `zerolog.ParseLevel`, which also parses numbers: `"99"` produced a logger that emitted nothing, and now falls back to `info`.

The `acor` CLI rejects `-dry-run` and `-keep-old-keys` outside `migrate` instead of ignoring them, so `acor -dry-run flush` can no longer look like a preview while deleting. ([#174](https://github.com/skyoo2003/acor/issues/174))

## [v0.10.0](https://github.com/skyoo2003/acor/releases/tag/v0.10.0) - 2026-07-25

### Added

* Add FindMatches, Contains, and FindStream matching APIs (plus Context variants). FindMatches returns matches in scan order with rune-offset spans and takes MatchKind (overlapping default, or non-overlapping leftmost-longest) and WholeWord options; Contains is an early-exit containment gate; FindStream scans an io.Reader without buffering the whole input. Available in preset, V2, and V1 modes with matching semantics identical to Find/FindIndex. ([#158](https://github.com/skyoo2003/acor/issues/158))
* Add AhoCorasickArgs.InvalidationPollInterval: an opt-in background safety net for Preset mode that reloads when the collection's stored version changes, bounding staleness from a dropped best-effort Pub/Sub invalidation. Add MatchOptions.WordRune to override whole-word boundary detection for scripts the default misclassifies (e.g. CJK). The Preset commit path also retries the invalidation publish a few times on transient failure. ([#158](https://github.com/skyoo2003/acor/issues/158))
* Expose Redis connection-resilience knobs on AhoCorasickArgs: DialTimeout, ReadTimeout, WriteTimeout, MaxRetries, and PoolSize. They pass straight through to go-redis across all topologies (standalone, cluster, sentinel, ring); a zero value keeps the existing go-redis default, so behavior is unchanged unless set. ([#161](https://github.com/skyoo2003/acor/issues/161))

### Changed

* Coalesce the preset-mode local automaton rebuild and pub/sub invalidation across AddMany/RemoveMany, so a batch of N writes triggers one rebuild instead of one per keyword (O(N^2) to O(N) bulk-load). The Redis-backed engine now swaps in a freshly built automaton on each rebuild, making local match snapshots immutable and scannable lock-free. ([#158](https://github.com/skyoo2003/acor/issues/158))
* Pinned golangci-lint to the CI version via `go run` in the Makefile and relaxed the `goconst` config to upstream defaults, so local `make lint` no longer diverges from CI. Repeated string literals (Redis field names, CLI commands, JSON keys) were extracted into named constants — no functional changes. ([#160](https://github.com/skyoo2003/acor/issues/160))

### Removed

* Removed the unused exported errors ErrNoBoundariesFound and ErrStreamInterrupted, which were declared but never returned by any operation. ([#159](https://github.com/skyoo2003/acor/issues/159))

### Fixed

* RemoveMany and RemoveManyWithOptions no longer report keywords that were not present as removed: an absent keyword is now recorded in BatchResult.Skipped instead of BatchResult.Removed, matching AddMany and single Remove. In Preset mode this also prevents a no-op RemoveMany from triggering an unnecessary cluster-wide cache reload. ([#158](https://github.com/skyoo2003/acor/issues/158))
* The internal match engine no longer panics on an invalid (negative) rune supplied to Engine.Stream; such runes are treated as not in the alphabet, matching any other out-of-alphabet character. ([#159](https://github.com/skyoo2003/acor/issues/159))
* FindParallel/FindIndexParallel now clamp a negative ParallelOptions.Overlap to 0. A negative overlap previously pushed each chunk's start past the previous boundary, dropping the runes in between and silently losing any match there. ([#159](https://github.com/skyoo2003/acor/issues/159))
* FindParallel now returns a deduplicated result set regardless of text size. The single-chunk fast path previously returned Find's per-occurrence multiplicity, contradicting the documented dedup contract that the multi-chunk path already followed. ([#159](https://github.com/skyoo2003/acor/issues/159))

### Documentation

* Documented on FindParallel and FindIndexParallel that a keyword longer than opts.Overlap straddling a chunk boundary can be missed, and to set Overlap to at least the longest expected keyword length. ([#159](https://github.com/skyoo2003/acor/issues/159))

## [v0.9.0](https://github.com/skyoo2003/acor/releases/tag/v0.9.0) - 2026-07-20

### Added

* First-class Valkey support: documented Valkey/Redis compatibility, an opt-in real-server integration suite (ACOR_INTEGRATION_ADDR), and a CI job validating against both redis and valkey service containers ([#150](https://github.com/skyoo2003/acor/issues/150))
* Standard-library gRPC observability via NewGRPCServerWithObservability: OpenTelemetry tracing (otelgrpc stats handler), Prometheus grpc_server_* metrics, zerolog request logging, and a live grpc.health.v1 health service backed by a background readiness poller ([#153](https://github.com/skyoo2003/acor/issues/153))

### Changed

* Migrate the Redis client from go-redis/v8 to go-redis/v9 (RESP3 by default, with automatic RESP2 fallback for servers without HELLO). No public API changes ([#150](https://github.com/skyoo2003/acor/issues/150))
* Speed up V2 cached Find/FindIndex by building a local Aho-Corasick automaton once per cache load instead of recomputing the failure function per character. Match latency is now constant with dictionary size (4-12x faster, ~300x fewer allocations on large dictionaries) ([#150](https://github.com/skyoo2003/acor/issues/150))
* Halve resident V2 cache memory by no longer retaining a full clone of the outputs map alongside the built Aho-Corasick automaton; the cache now keeps only the engine it matches against ([#150](https://github.com/skyoo2003/acor/issues/150))
* Speed up in-memory Aho-Corasick matching (Speed/Balanced/Ultimate presets and the V2 cached path) with an ASCII direct-index fast path and rune-code threading through the double-array-trie failure traversal, removing a map lookup on nearly every character. Default Balanced Find/FindIndex is ~2.2-2.4x faster (Speed up to 3x) with no change to match results ([#152](https://github.com/skyoo2003/acor/issues/152))
* **BREAKING**: Rebuild the gRPC adapter on standard protobuf (previously a hand-rolled JSON-over-gRPC codec). The acor.server.v1.Acor service is now defined in server/proto/acor/v1/acor.proto and served via server.NewGRPCServer / NewGRPCServerWithObservability; regenerate Go stubs with make proto. Existing gRPC clients must regenerate from the .proto ([#153](https://github.com/skyoo2003/acor/issues/153))
* **BREAKING**: Rename gRPC Prometheus metrics from acor_grpc_requests_total / acor_grpc_request_duration_seconds to the standard grpc_server_* series; update dashboards and alerts ([#153](https://github.com/skyoo2003/acor/issues/153))
* gRPC request logs now use go-grpc-middleware field names (grpc.method, grpc.code, grpc.time_ms; message "finished call") instead of the previous "request completed" line with method / status / latency_ms; update log-based alerts ([#153](https://github.com/skyoo2003/acor/issues/153))
* Reorganize internals: extract the Aho-Corasick engines into internal/engine and storage into internal/storage, leaving the public API in pkg/acor unchanged ([#151](https://github.com/skyoo2003/acor/issues/151))

### Removed

* **BREAKING**: Unexport PresetDefault; it was an internal, non-user-selectable sentinel identical to PresetNone. Use acor.PresetNone instead ([#153](https://github.com/skyoo2003/acor/issues/153))

### Fixed

* Fix a failure-link construction bug in the Speed and MemoryEfficient in-memory engines that double-applied the goto transition. Keyword sets with nested suffixes (e.g. {a, aa, aaa}) could make MemoryEfficient Find loop forever and cause Speed to silently drop matches; Balanced and Ultimate were unaffected ([#152](https://github.com/skyoo2003/acor/issues/152))
* Fix the Speed in-memory engine (PresetSpeed) silently dropping matches when a keyword's failure link points to a state inserted later in the trie. The full DFA transition table was filled in state-id order, so a fail-fallback row could be copied before it was populated (e.g. keywords {aab, b} against "aabb" dropped the trailing "b"); it is now filled in breadth-first order. Balanced, MemoryEfficient, and Ultimate were unaffected ([#152](https://github.com/skyoo2003/acor/issues/152))

### Documentation

* Add a CI guard that compiles the documented Go examples to keep them from drifting ([#149](https://github.com/skyoo2003/acor/issues/149))
* Fix broken Go code examples in the documentation ([#147](https://github.com/skyoo2003/acor/issues/147))
* Fix stale descriptions and cross-cutting drift across the documentation ([#148](https://github.com/skyoo2003/acor/issues/148))

## [v0.8.0](https://github.com/skyoo2003/acor/releases/tag/v0.8.0) - 2026-07-17
### Changed
* **BREAKING**: Merge Ultimate engine into Balanced and share Bloom pre-filter ([#138](https://github.com/skyoo2003/acor/issues/138))
* Split server and observability into separate modules ([#139](https://github.com/skyoo2003/acor/issues/139))
* Harden and simplify GitHub Actions workflows ([#142](https://github.com/skyoo2003/acor/issues/142))
* Update branch references from master to main ([#141](https://github.com/skyoo2003/acor/issues/141))
* Bump Go dependencies: go-deps group ([#127](https://github.com/skyoo2003/acor/issues/127), [#133](https://github.com/skyoo2003/acor/issues/133), [#137](https://github.com/skyoo2003/acor/issues/137))
* Bump CI dependencies: GitHub Actions ([#126](https://github.com/skyoo2003/acor/issues/126), [#128](https://github.com/skyoo2003/acor/issues/128), [#131](https://github.com/skyoo2003/acor/issues/131), [#136](https://github.com/skyoo2003/acor/issues/136))
### Documentation
* Add release guide (RELEASE.md) ([#143](https://github.com/skyoo2003/acor/issues/143))

## [v0.7.0](https://github.com/skyoo2003/acor/releases/tag/v0.7.0) - 2026-04-20
### Added
- Add RedisBackedAC with preset-optimized engine and benchmarks ([#124](https://github.com/skyoo2003/acor/issues/124))
- Open-source project setup (LICENSE, CLAUDE.md, issue/PR templates, etc.) ([#122](https://github.com/skyoo2003/acor/issues/122))
### Changed
- Reorganize .gitignore with categorized sections ([#123](https://github.com/skyoo2003/acor/issues/123))

## [v0.6.1](https://github.com/skyoo2003/acor/releases/tag/v0.6.1) - 2026-04-17
### Changed
* Extract ops constructors into newV2Ops/newV1Ops helpers, preserve configured values across ops swaps, and promote caseSensitive to struct field ([#120](https://github.com/skyoo2003/acor/issues/120))
### Fixed
* Fix migration/rollback not swapping ops to target schema version, causing operations to use wrong schema after MigrateV1ToV2 or RollbackToV1 ([#120](https://github.com/skyoo2003/acor/issues/120))
* Add http.MaxBytesReader (1MB) to HTTP request decoders to prevent memory exhaustion from oversized payloads ([#120](https://github.com/skyoo2003/acor/issues/120))
* Reorder removeV2Script Lua DEL after cjson.decode for defensive programming ([#120](https://github.com/skyoo2003/acor/issues/120))
* Replace context.Background() with context.WithCancel for proper lifecycle management and fix shutdown order ([#120](https://github.com/skyoo2003/acor/issues/120))
* Stop cache listener on rollback and fix context cancellation before stopping cache listener in Close ([#120](https://github.com/skyoo2003/acor/issues/120))
* Enable changelog in GoReleaser config to allow --release-notes flag to populate GitHub release notes ([#120](https://github.com/skyoo2003/acor/issues/120))

## [v0.6.0](https://github.com/skyoo2003/acor/releases/tag/v0.6.0) - 2026-04-16
### Added
* Add case-sensitive matching support via CaseSensitive field in AhoCorasickArgs ([#112](https://github.com/skyoo2003/acor/issues/112))
* Add RollbackTimeout field to AhoCorasickArgs for configurable V1 rollback timeout ([#112](https://github.com/skyoo2003/acor/issues/112))
### Changed
* Change Remove return value from remaining keyword count to removed keyword count (0 or 1) ([#112](https://github.com/skyoo2003/acor/issues/112))
* Replace inline EVAL calls with redis.NewScript package-level variables for EVALSHA optimization and script caching ([#112](https://github.com/skyoo2003/acor/issues/112))
* Refactor internal architecture: inline v1_operations.go into v1_ops.go, split v2_operations.go into v2_ops.go, v2_lua.go, v2_transaction.go ([#112](https://github.com/skyoo2003/acor/issues/112))
### Removed
* Remove unused ValidationError type, Matcher/Indexer interfaces, and mock types (go.uber.org/mock dependency dropped) ([#112](https://github.com/skyoo2003/acor/issues/112))
### Fixed
* Fix V1 find/findIndex collecting outputs from previous state instead of current state ([#112](https://github.com/skyoo2003/acor/issues/112))
* Fix V1/V2 findIndex off-by-one producing negative start indices ([#112](https://github.com/skyoo2003/acor/issues/112))
* Fix generateVersion int64 overflow by packing timestamp and random suffix into separate bit ranges ([#112](https://github.com/skyoo2003/acor/issues/112))
* Fix potential panics from unprotected type assertions on trieKey/outputsKey in Lua script runners ([#112](https://github.com/skyoo2003/acor/issues/112))
* Fix FindParallel returning duplicate matches in overlap regions ([#109](https://github.com/skyoo2003/acor/issues/109))
* Fix rollback deadlock on context cancellation by adding ctx.Done() select in semaphore acquisition ([#112](https://github.com/skyoo2003/acor/issues/112))
* Replace leakable atomic counter with unique message IDs for cache self-invalidation skip-self mechanism ([#109](https://github.com/skyoo2003/acor/issues/109))
* Cache prefixSet in trieCache and precompute output rune lengths to avoid repeated allocations in Find/FindIndex hot loop ([#112](https://github.com/skyoo2003/acor/issues/112))
### Security
* Run Docker containers as non-root user to prevent RCE ([#110](https://github.com/skyoo2003/acor/issues/110))
* Pin third-party GitHub Actions to commit SHAs to prevent supply chain attacks ([#111](https://github.com/skyoo2003/acor/issues/111))
### Documentation
* Sync API reference and V2 schema docs with source code ([#112](https://github.com/skyoo2003/acor/issues/112))

## [v0.5.1](https://github.com/skyoo2003/acor/releases/tag/v0.5.1) - 2026-04-14
### Fixed
* Prevent pub/sub self-message from invalidating local cache ([#106](https://github.com/skyoo2003/acor/issues/106))
### Documentation
* Fix broken Hugo documentation links with relative paths ([#107](https://github.com/skyoo2003/acor/issues/107))
* Add single page template to fix broken links ([#105](https://github.com/skyoo2003/acor/issues/105))

## [v0.5.0](https://github.com/skyoo2003/acor/releases/tag/v0.5.0) - 2026-04-13
### Added
* Add local caching for Find/FindIndex operations with Redis Pub/Sub invalidation ([#99](https://github.com/skyoo2003/acor/issues/99))
* Add *Context variants for all public API methods (AddContext, FindContext, etc.) ([#96](https://github.com/skyoo2003/acor/issues/96))
* Add BatchOptions support to RemoveMany for API symmetry with AddMany ([#96](https://github.com/skyoo2003/acor/issues/96))
* Add V1+Cache guard to prevent unsupported schema/cache configuration ([#96](https://github.com/skyoo2003/acor/issues/96))
* Add internal Operations interface with Strategy pattern for V1/V2 dispatch ([#102](https://github.com/skyoo2003/acor/issues/102))
* Add KVStorage interface for Redis dependency injection ([#102](https://github.com/skyoo2003/acor/issues/102))
* Add redisStorage adapter wrapping go-redis UniversalClient ([#102](https://github.com/skyoo2003/acor/issues/102))
* Add CI hardening: race detector, coverage threshold (70%), and fuzz testing ([#96](https://github.com/skyoo2003/acor/issues/96))
* Add Makefile targets: vet, lint-fix, fuzz, race ([#96](https://github.com/skyoo2003/acor/issues/96))
* Add Issue/PR templates and SECURITY.md ([#96](https://github.com/skyoo2003/acor/issues/96))
### Changed
* Go required version 1.25 or higher ([#97](https://github.com/skyoo2003/acor/issues/97))
* Refactor AhoCorasick struct to use KVStorage DI and operations Strategy pattern ([#102](https://github.com/skyoo2003/acor/issues/102))
* Activate error helpers (newRedisError, newOperationError) in V1 and V2 operations ([#96](https://github.com/skyoo2003/acor/issues/96))
* Replace mustJSON panic with toJSON error return for safer error propagation ([#96](https://github.com/skyoo2003/acor/issues/96))
* Rename underscore-prefixed methods in trie.go (buildTrie, gotoNode, failNode, collectOutputs) ([#96](https://github.com/skyoo2003/acor/issues/96))
* Split monolithic test file into feature-specific test files ([#96](https://github.com/skyoo2003/acor/issues/96))
* Fix README example API names (BatchModeTransactional, ChunkBoundaryWord, Boundary) ([#96](https://github.com/skyoo2003/acor/issues/96))
* Bump dependencies: go-redis, gRPC, OpenTelemetry, zerolog ([#100](https://github.com/skyoo2003/acor/issues/100), [#103](https://github.com/skyoo2003/acor/issues/103))
* Bump CI dependencies: GitHub Actions ([#101](https://github.com/skyoo2003/acor/issues/101))
### Removed
* Remove unused non-Context wrapper functions in trie.go ([#96](https://github.com/skyoo2003/acor/issues/96))
### Documentation
* Add cross-references between Hugo documentation pages ([#98](https://github.com/skyoo2003/acor/issues/98))
* Add comprehensive Hugo documentation: guides, API reference, deployment, monitoring, troubleshooting ([#96](https://github.com/skyoo2003/acor/issues/96))

## [v0.4.0](https://github.com/skyoo2003/acor/releases/tag/v0.4.0) - 2026-03-18
### Added
* Add CLI commands: migrate, migrate-rollback, schema-version ([#83](https://github.com/skyoo2003/acor/issues/83))
* Add RollbackToV1() for safe rollback when V1 keys are kept ([#83](https://github.com/skyoo2003/acor/issues/83))
* Add V2 schema with 80-85% fewer Redis round trips and 99% fewer keys ([#83](https://github.com/skyoo2003/acor/issues/83))
* Add MigrateV1ToV2() with dry-run support and progress callbacks ([#83](https://github.com/skyoo2003/acor/issues/83))
* Add parallel matching (FindParallel, FindIndexParallel) with configurable chunk boundaries ([#84](https://github.com/skyoo2003/acor/issues/84))
* Add batch operations (AddMany, RemoveMany, FindMany) with BestEffort and Transactional modes ([#84](https://github.com/skyoo2003/acor/issues/84))
* Add pkg/metrics: Prometheus metrics registry with HTTP/gRPC middleware ([#85](https://github.com/skyoo2003/acor/issues/85))
* Add pkg/health: Kubernetes-compatible health checks (liveness/readiness) for HTTP/gRPC ([#85](https://github.com/skyoo2003/acor/issues/85))
* Add observability integration to gRPC server (NewGRPCServerWithObservability) ([#85](https://github.com/skyoo2003/acor/issues/85))
* Add pkg/tracing: OpenTelemetry distributed tracing with HTTP/gRPC middleware ([#85](https://github.com/skyoo2003/acor/issues/85))
* Add pkg/logging: zerolog structured logging with HTTP/gRPC middleware ([#85](https://github.com/skyoo2003/acor/issues/85))
### Changed
* Go required version 1.24 or higher ([#80](https://github.com/skyoo2003/acor/issues/80))
* Add SchemaVersion field to AhoCorasickArgs for explicit schema selection ([#83](https://github.com/skyoo2003/acor/issues/83))
* **BREAKING**: New collections now default to V2 schema. Use SchemaVersion: 1 to keep V1 behavior ([#83](https://github.com/skyoo2003/acor/issues/83))
* chore: update pre-commit hooks to latest versions ([#88](https://github.com/skyoo2003/acor/issues/88))
### Removed
* Remove unused BatchSize field from MigrationOptions (was never implemented) ([#83](https://github.com/skyoo2003/acor/issues/83))
### Fixed
* Correct migration progress step constants ([#83](https://github.com/skyoo2003/acor/issues/83))
### Documentation
* Add performance tradeoffs and migration notes to README ([#83](https://github.com/skyoo2003/acor/issues/83))

## [v0.3.0](https://github.com/skyoo2003/acor/releases/tag/v0.3.0) - 2026-03-14
### Added
* Add index APIs for find and suggest ([#67](https://github.com/skyoo2003/acor/issues/67))
* Add Redis topology-aware client selection ([#69](https://github.com/skyoo2003/acor/issues/69))
* Add HTTP and gRPC server adapters ([#70](https://github.com/skyoo2003/acor/issues/70))
* Add CLI support ([#75](https://github.com/skyoo2003/acor/issues/75))
### Changed
* Handle Redis errors during AC execution ([#68](https://github.com/skyoo2003/acor/issues/68))
### Documentation
* Add GitHub Pages documentation and deployment workflow ([#76](https://github.com/skyoo2003/acor/issues/76))

## [v0.2.0](https://github.com/skyoo2003/acor/releases/tag/v0.2.0) - 2021-07-09

### Changed

- Changed to standard project structure ([#2](https://github.com/skyoo2003/acor/issues/2))
- Changed supported Go versions ([#5](https://github.com/skyoo2003/acor/issues/5))
- Changed RedisAlreadyClosed error name ([#7](https://github.com/skyoo2003/acor/issues/7))

### Fixed

- Fixed NodeKey output was not written ([#13](https://github.com/skyoo2003/acor/issues/13))

## [v0.1.0](https://github.com/skyoo2003/acor/releases/tag/v0.1.0) - 2020-11-17

### Changed
* Bump go-redis/redis libraries
* Applied go modules
* Bump Go required version (1.8 -> 1.11)

## [v0.0.0](https://github.com/skyoo2003/acor/releases/tag/v0.0.0) - 2017-06-29

### Added
* Created ACOR APIs
