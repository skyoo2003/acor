# Changelog
All notable changes to this project will be documented in this file.

## [v1.6.0](https://github.com/skyoo2003/acor/releases/tag/v1.6.0) - 2026-09-06

### Added

* Add opt-in automatic parallel boundary protection and cancellation-independent Preset reloads, version-only polling, and refresh failure counters. No data migration is required. ([#243](https://github.com/skyoo2003/acor/issues/243))
* Add opt-in V3 versioned dictionaries with atomic batch updates, snapshots, background engine refresh, bucket reuse, V2 migration and dictionary CLI tools; add bounded scanning, masking and replacement with original text positions. ([#244](https://github.com/skyoo2003/acor/issues/244))

### Documentation

* Resynced the docs site with the code it describes: a stale nineteen-command CLI count, five pages linked from no section index, a V3 section stranded below `commands.md`'s nav footer, and three fields missing from the `CacheStats`/`ParallelOptions` listings. Also documented the benchmark and `proto` Make targets and linked the runnable `examples/` programs. ([#245](https://github.com/skyoo2003/acor/issues/245))

## [v1.5.2](https://github.com/skyoo2003/acor/releases/tag/v1.5.2) - 2026-08-09

### Changed

* Dropped the unreachable `Get`, `Set`, and `SetNX` methods from the internal `kvStorage` seam and their `redisStorage` implementations, which no production path called. Also moved `stringRuneSource` into its only caller's test file and replaced the deprecated `sort.Ints`/`sort.Strings`/`sort.Slice` with the `slices` equivalents; `api/v1.txt` and `NOTICE` come back byte-identical. ([#220](https://github.com/skyoo2003/acor/issues/220))
* Moved the V1 writer and the trie-building helpers it drove out of the production build and into `v1_fixture_test.go`, since v1.5.0 closed V1 to writes and nothing reachable from a released binary could call them. Everything moved is unexported, so `api/v1.txt` is byte-identical, and no production binary now carries a way to write V1. ([#221](https://github.com/skyoo2003/acor/issues/221))
* Collapsed the CLI's four string-to-enum flag parsers — `-preset`, `-batch-mode`, `-boundary`, and `-match-kind` — onto one generic `parseEnum` plus a name table each. Accepted values, case-insensitive matching, and the error wording are unchanged. ([#223](https://github.com/skyoo2003/acor/issues/223))

### Fixed

* The logging, metrics, and tracing HTTP middlewares now share one `http.ResponseWriter` wrapper in `server/internal/httpx` that forwards only the optional interfaces the underlying writer actually implements. Their three drifted copies had broken server-sent-event flushing and websocket upgrades under tracing, and made `Hijack` assertions succeed on HTTP/2 where the call then failed. ([#222](https://github.com/skyoo2003/acor/issues/222))
* The HTTP handler now reports `413 Request Entity Too Large` for every request body over the 1 MiB cap, where a body whose first JSON value fit under the cap previously came back as `400`, making `413` unusable as a client-side signal. ([#231](https://github.com/skyoo2003/acor/issues/231))

### Documentation

* Corrected the docs site's install instructions, which pinned `@v0.10.0` — a release predating both `v0.11.0`'s breaking changes and the entire `v1` line — so all four snippets now use `@latest`, matching `README.md`. Also recorded that `v1.5.1` changed no entry of the frozen `v1` surface and de-hardcoded the release verification walkthrough. ([#224](https://github.com/skyoo2003/acor/issues/224))
* Documented the experimental `acor/server` module, which had no section on the docs site, in a new top-level Server section covering running a server, the HTTP API, and the gRPC API, with a complete compile-gated `main` for each protocol. The error contracts are recorded as they behave rather than as they ought to. ([#225](https://github.com/skyoo2003/acor/issues/225))
* Corrected `CONTRIBUTING.md` where it described a process this repository does not run: branching from a `master` that does not exist, a `make all` listed as six targets when it runs seven, a duplicated pull-request checklist, a missing changelog-fragment step, and a Redis `>= 6.0` floor no other document states. ([#227](https://github.com/skyoo2003/acor/issues/227))
* Gave the CLI its own section on the documentation site, moving its usage out of the Getting Started page's `CLI Installation` heading and adding a command index plus the caveat that CLI flags, output format, and exit codes sit outside the `v1` promise. The landing page's hero and card grid now match what the site actually contains. ([#229](https://github.com/skyoo2003/acor/issues/229))
* Replaced the README's seven-bullet duplicate of the documentation site's section index — already drifted to a `#cli-installation` anchor that had moved — with one paragraph and one link to the site. Every removed target remains reachable from the landing page. ([#230](https://github.com/skyoo2003/acor/issues/230))

## [v1.5.1](https://github.com/skyoo2003/acor/releases/tag/v1.5.1) - 2026-08-09

### Fixed

* Restored the verbatim Apache-2.0 text in `LICENSE`, whose drift in three phrases and dropped `APPENDIX` had put pkg.go.dev's license match under its 75% threshold, so the site reported `License: UNKNOWN` and hid the package documentation. Per-version license data is immutable, so this takes effect from the next tag; `server/` carries its own copy because module zips skip symlinks. ([#217](https://github.com/skyoo2003/acor/issues/217))

## [v1.5.0](https://github.com/skyoo2003/acor/releases/tag/v1.5.0) - 2026-08-09

### Added

* `AhoCorasick.CacheStats()` reports the local cache hit rate, rebuild cost, and the lag of the last peer invalidation with no Redis I/O, where these metrics were previously exposed only by the experimental `server/` module. Counters are per instance and per process, and the struct may gain fields within v1. ([#202](https://github.com/skyoo2003/acor/issues/202))
* CI verifies that the `retract [v1.0.0, v1.4.0]` block is still present in `go.mod`, so a release cannot silently un-retract the withdrawn version range. ([#203](https://github.com/skyoo2003/acor/issues/203))
* CI checks the public API surface of `pkg/acor` against a committed snapshot (`api/v1.txt`), so a breaking change must be recorded in the diff that introduces it; `make api-check` runs the same check locally. ([#203](https://github.com/skyoo2003/acor/issues/203))
* `NOTICE` now lists the copyright notice and full license text of every third-party module linked into the `acor` binary and ships in the release archives and container images, regenerated by `make license-check` and verified in CI. ([#199](https://github.com/skyoo2003/acor/issues/199))

### Changed

* Every public type that was an alias into an internal package — `KVStorage`, `Z`, `Pipeliner`, `Subscription`, `StringMapResult`, `PubSubMessage`, `Preset`, and `Match` — is now declared in `pkg/acor` itself and `internal/storage` is gone. Method sets and struct fields are unchanged and Go satisfies interfaces structurally, so custom backends and `Match` consumers need no changes. ([#201](https://github.com/skyoo2003/acor/issues/201))
* V1 collections are now read-only: `Add` and `Remove` return the new `ErrV1ReadOnly` (batch calls report it per keyword in `BatchResult.Failed`), while `Find`, `FindIndex`, `Suggest`, `Info`, `Flush`, and `MigrateV1ToV2` are unchanged, so an existing collection can still be read and converted in place. The V1 read path stays for the whole v1 line and is removed no earlier than v2. ([#201](https://github.com/skyoo2003/acor/issues/201))
* `FindSet` now dedups by a 4-byte pattern id with keyword strings interned once instead of by string, making it 1.4-3.2x faster at 1000 keywords and cutting per-query allocations from 178 KB/44 to 35 KB/10. Sparse text is unchanged and there is no public API change. ([#215](https://github.com/skyoo2003/acor/issues/215))

### Deprecated

* Retracted `v1.0.0`-`v1.4.0`, whose tags were deleted long ago but which the module proxy still resolved, so `go get github.com/skyoo2003/acor` landed on `v1.4.0` instead of the supported line. This release is the first supported v1, which is why the numbering jumps from v0.11.x; existing pins keep building and are reported by `go list -m -u`. ([#198](https://github.com/skyoo2003/acor/issues/198))

### Removed

* Removed `acor.InMemoryInfo`, a field-for-field duplicate of `AhoCorasickInfo` that no exported function accepted or returned. ([#201](https://github.com/skyoo2003/acor/issues/201))
* Removed `PresetUltimate`, a deprecated alias for `PresetBalanced` since v0.11.0, so switching to `PresetBalanced` is a rename with identical behavior. ([#201](https://github.com/skyoo2003/acor/issues/201))
* Unexported the storage abstraction — `KVStorage`, `Pipeliner`, `Subscription`, `StringMapResult`, `PubSubMessage`, and `Z` — removing 43 of the 223 entries on the frozen v1 surface, since no exported function accepted or returned one. Their shape stays open for the pluggable-backend work they were exported for, and no behavior changed. ([#209](https://github.com/skyoo2003/acor/issues/209))

### Fixed

* `FindParallel`, `FindIndexParallel`, and `FindMany` now cost one Redis round trip per call instead of one per chunk or per text, where a 63-chunk text previously issued 63 `HGETALL` calls against the full outputs hash. The automaton is loaded once per call, so every chunk also scans a consistent dictionary and `CacheStats` records one read per call. ([#206](https://github.com/skyoo2003/acor/issues/206))
* Corrected thirteen godoc claims on the frozen v1 surface that did not match the code, covering `ParallelOptions.ChunkSize`/`Overlap`, `BatchOptions.Mode`, `BatchResult.Skipped`, `Debug`, `Info`, `Add`/`Remove` on empty keywords, `ErrEmptyKeyword`, `ErrConcurrencyConflict`, `CacheStats.Misses`, and `KVStorage`. No signature, sentinel identity, or behavior changed, and `api/v1.txt` is unchanged. ([#208](https://github.com/skyoo2003/acor/issues/208))
* Completed the godoc audit of the frozen v1 surface, checking all 180 entries against the code and correcting the remaining claims about topology selection, the V2 key count, the migration result fields, `RollbackToV1` leaving a collection read-only, and `FlushContext` running under `RollbackTimeout`. No signature, sentinel identity, or behavior changed, and `api/v1.txt` is unchanged. ([#211](https://github.com/skyoo2003/acor/issues/211))
* Fixed a lost update in Preset mode where a single `Add` or `Remove` rebuilt the local automaton from an incrementally maintained keyword set instead of the snapshot it had just committed, so a peer's keyword vanished from local `Find` results and was never refetched. Single writes now apply the committed snapshot, as batch writes already did. ([#207](https://github.com/skyoo2003/acor/issues/207))
* `Create` now rejects `RingAddrs` combined with `Addrs` instead of silently building a ring client and dropping `Addrs`; the old guard tested the merged address list for more than one entry, so a one-element `Addrs` slipped through and connected the caller to ring shards they had not asked for. `Addr` beside `RingAddrs` is still accepted with `Addr` ignored, as documented. ([#211](https://github.com/skyoo2003/acor/issues/211))

### Documentation

* Documented the v1 compatibility promise: the exported API of `pkg/acor`, its documented behavior, sentinel error identity, and additive-only changes to the on-Redis V2 format are covered for every `v1.x.y` release, so mixed-version fleets stay safe during a rolling deploy. The CLI contract, the experimental `acor/server` module, and `internal/...` are excluded. ([#200](https://github.com/skyoo2003/acor/issues/200))
* Rewrote the monitoring page to separate the core library from the experimental `server/` module and documented how to read the new cache statistics, including why `Rebuilds` does not equal `Misses` and why `LastInvalidationLag` includes clock skew. ([#202](https://github.com/skyoo2003/acor/issues/202))

## [v0.11.0](https://github.com/skyoo2003/acor/releases/tag/v0.11.0) - 2026-08-05

### Added

* Homebrew installation via `brew install skyoo2003/tap/acor`, published to the tap on each release ([#181](https://github.com/skyoo2003/acor/issues/181))
* `FindSet`/`FindSetContext` return each matched keyword once in first-match order; `Find` remains unchanged and reports every occurrence ([#188](https://github.com/skyoo2003/acor/issues/188))
* `FindMatchesAppend` appends matches to a caller-supplied buffer, allowing the result slice to be reused across scans ([#188](https://github.com/skyoo2003/acor/issues/188))
* Expose batch operations, parallel matching, Redis-backed engine presets, and local caching through the `acor` CLI ([#195](https://github.com/skyoo2003/acor/issues/195))
* `CreateContext` constructs an instance with a context bounding the setup I/O only, so canceling it cannot silently stop the invalidation listener ([#196](https://github.com/skyoo2003/acor/issues/196))
* CLI commands `version`, `find-set`, `find-matches`, and `contains`, with `-match-kind` and `-whole-word` for `find-matches`; `acor version` reports the version stamped into release builds and needs no Redis ([#196](https://github.com/skyoo2003/acor/issues/196))

### Changed

* Improved `FindMatches` and `Contains` performance by scanning input strings directly and preallocating a small result buffer, while `FindStream` retains its streaming path for `io.Reader` inputs ([#188](https://github.com/skyoo2003/acor/issues/188))
* Improved leftmost-longest `FindMatches` sorting and selection to reduce time and allocations; `Find` and `FindSet` also avoid allocating results when no keywords match ([#188](https://github.com/skyoo2003/acor/issues/188))
* Flattened and packed the DFA transition tables to reduce per-character lookup overhead across presets, and `Find`, `FindSet`, and `Contains` now scan raw bytes for ASCII-only dictionaries ([#187](https://github.com/skyoo2003/acor/issues/187))
* `PresetUltimate` is now a deprecated alias for `PresetBalanced`, its Bloom pre-filter removed because it was 1.7-1.8x slower and blocked the ASCII byte-scan fast path. Existing code stays compatible and now gets the faster engine, while `PresetMemoryEfficient` keeps its Bloom pre-filter. ([#186](https://github.com/skyoo2003/acor/issues/186))
* Every preset now stores each keyword once and follows output links for suffix matches, keeping output storage linear for suffix-nested dictionaries while preserving match results and order ([#187](https://github.com/skyoo2003/acor/issues/187))
* Improved `Contains` by scanning directly and stopping at the first match, and optimized `FindSet` deduplication to reduce scan time and allocations ([#188](https://github.com/skyoo2003/acor/issues/188))
* `RemoveMany` now takes `*BatchOptions` like `AddMany` and `RemoveManyWithOptions` is removed, so replace `RemoveMany(kw)` with `RemoveMany(kw, nil)` and `RemoveManyWithOptions(kw, opts)` with `RemoveMany(kw, opts)` ([#196](https://github.com/skyoo2003/acor/issues/196))

### Deprecated

* `SchemaV1` is deprecated: V1 collections stay readable and writable and `MigrateV1ToV2` converts them in place, but V1 gains no new features and support may be removed in a future major version ([#196](https://github.com/skyoo2003/acor/issues/196))

### Removed

* Removed the exported V1 key-format constants `KeywordKey`, `PrefixKey`, `SuffixKey`, `OutputKey`, and `NodeKey`, which never produced the keys ACOR writes (a real key is hash-tagged, `{name}:keyword`) and which nothing read ([#196](https://github.com/skyoo2003/acor/issues/196))

### Fixed

* Uncached V2 `Find()` no longer rebuilds the match engine on every call and reads only the outputs hash, taking 1000 keywords from 1,163,098 to 221,253 ns/op and 11,704 to 2,063 allocs/op — from ~9x slower than V1 to ~1.7x. Freshness is unchanged, since the read still happens on every call. ([#183](https://github.com/skyoo2003/acor/issues/183))
* `AddMany` and `RemoveMany` now process V2 and preset batches in one pass and commit them in a single atomic transaction, making a 1,000-keyword add about 400x faster with 970x fewer allocations at a fixed two round trips. V1 behavior is unchanged. ([#185](https://github.com/skyoo2003/acor/issues/185))
* `Info().TrieDepth` now reports the correct value for `PresetSpeed` and is recomputed on every rebuild ([#187](https://github.com/skyoo2003/acor/issues/187))
* `AddMany` and `RemoveMany` now detect case-insensitive duplicates using normalized keywords in V2 and preset collections, so `["Foo", "foo"]` reports one Added and one Skipped, matching V1 and single-keyword operations ([#185](https://github.com/skyoo2003/acor/issues/185))
* `MigrateV1ToV2` and `RollbackToV1` no longer panic with a nil pointer dereference on a Preset-mode instance and return the new `ErrMigrationRequiresRedis` instead, matching the check the CLI already performed ([#196](https://github.com/skyoo2003/acor/issues/196))
* `Create` now rejects `EnableCache` combined with a `Preset` (`ErrCacheWithPreset`) instead of silently ignoring the cache setting, since Preset mode already serves reads from a local engine ([#196](https://github.com/skyoo2003/acor/issues/196))
* `Create(nil)` now returns the new `ErrNilArgs` instead of panicking with a nil pointer dereference, and `CreateContext` shares the guard ([#196](https://github.com/skyoo2003/acor/issues/196))
* The `acor` CLI now rejects `suggest` and `suggest-index` under `-preset` with the usage exit code instead of connecting, loading the whole dictionary, and then failing with `ErrSuggestRequiresRedis`; `acor version` also no longer ignores stray arguments and inapplicable flags ([#196](https://github.com/skyoo2003/acor/issues/196))

### Documentation

* Replaced the published performance claims with measured values and a reproducible benchmarks page: both V1 and V2 `Find()` cost one round trip, and the "50-60x faster Find()" belongs to `EnableCache`/`Preset`, not the V2 schema. Round-trip counts are now enforced by tests on every CI run. ([#182](https://github.com/skyoo2003/acor/issues/182))
* Corrected the security policy, which listed supported v1.x/v2.x lines that were never released, and labeled the `acor/server` module experimental and separately versioned. Also documented the repository's module layout and why the library import path keeps its `pkg/` segment. ([#196](https://github.com/skyoo2003/acor/issues/196))

## [v0.10.1](https://github.com/skyoo2003/acor/releases/tag/v0.10.1) - 2026-07-30

### Changed

* The V2 trie hash no longer stores the `suffixes` field, which held every prefix reversed, was rewritten on each add and remove, and was never read by any matcher. Existing collections are unaffected: a leftover field is ignored on read and disappears on the next `Flush`, so old and new binaries can share a collection during a rolling upgrade. ([#170](https://github.com/skyoo2003/acor/issues/170))
* V1 `Find` and `FindIndex` now scan with the same in-memory automaton the other modes use — one `SMEMBERS` per call instead of two to three commands per rune — with identical results pinned by `v1_engine_parity_test.go`. The set is re-read on every call, so another instance's write is still picked up immediately. ([#173](https://github.com/skyoo2003/acor/issues/173))
* `logging.NewLogger` now accepts every level `zerolog.ParseLevel` understands, adding `trace`, `fatal`, `panic`, and `disabled` to the four that already worked, while unrecognized and empty values still default to `info`. The `acor` CLI help also renders its option list from the flag definitions instead of a hand-maintained copy, so the two can no longer disagree. ([#174](https://github.com/skyoo2003/acor/issues/174))

### Fixed

* `FindParallelContext` now deduplicates its results for short texts, matching `FindParallel` and the documented contract that each keyword appears at most once. ([#172](https://github.com/skyoo2003/acor/issues/172))
* The `Preset` modes now honor a canceled or expired context in `FindContext`, `FindIndexContext`, and their parallel variants, which previously ran the full scan and returned complete results with a nil error once the local engine was warm. ([#172](https://github.com/skyoo2003/acor/issues/172))
* `Create` now returns the new `ErrRedisAddrs` for an `Addrs` list of only blank entries instead of connecting to localhost, and `ErrRedisClusterDB` whenever cluster mode is selected rather than only for lists of more than one address. `logging.NewLogger` also range-checks numeric levels, and the CLI rejects `-dry-run` and `-keep-old-keys` outside `migrate` instead of ignoring them. ([#174](https://github.com/skyoo2003/acor/issues/174))

## [v0.10.0](https://github.com/skyoo2003/acor/releases/tag/v0.10.0) - 2026-07-25

### Added

* Add `FindMatches`, `Contains`, and `FindStream` plus Context variants: scan-order matches with rune-offset spans and `MatchKind`/`WholeWord` options, an early-exit containment gate, and a scan over an `io.Reader` that never buffers the whole input. Semantics are identical to `Find`/`FindIndex` in preset, V2, and V1 modes. ([#158](https://github.com/skyoo2003/acor/issues/158))
* Add `AhoCorasickArgs.InvalidationPollInterval`, an opt-in Preset-mode reload that bounds staleness from a dropped best-effort Pub/Sub invalidation, and `MatchOptions.WordRune` to override whole-word boundary detection for scripts the default misclassifies such as CJK. ([#158](https://github.com/skyoo2003/acor/issues/158))
* Expose the Redis connection-resilience knobs `DialTimeout`, `ReadTimeout`, `WriteTimeout`, `MaxRetries`, and `PoolSize` on `AhoCorasickArgs`, passed straight through to go-redis across all topologies; a zero value keeps the existing go-redis default, so behavior is unchanged unless set. ([#161](https://github.com/skyoo2003/acor/issues/161))

### Changed

* `AddMany` and `RemoveMany` now coalesce the preset-mode automaton rebuild and pub/sub invalidation into one per batch instead of one per keyword, taking a bulk load from O(N^2) to O(N). The engine swaps in a freshly built automaton on each rebuild, so local match snapshots are immutable and scannable lock-free. ([#158](https://github.com/skyoo2003/acor/issues/158))
* Pinned golangci-lint to the CI version via `go run` in the Makefile and relaxed the `goconst` config to upstream defaults, so local `make lint` no longer diverges from CI. ([#160](https://github.com/skyoo2003/acor/issues/160))

### Removed

* Removed the unused exported errors `ErrNoBoundariesFound` and `ErrStreamInterrupted`, which were declared but never returned by any operation. ([#159](https://github.com/skyoo2003/acor/issues/159))

### Fixed

* `RemoveMany` and `RemoveManyWithOptions` now record an absent keyword in `BatchResult.Skipped` instead of `BatchResult.Removed`, matching `AddMany` and single `Remove`, which also stops a no-op `RemoveMany` from triggering a cluster-wide cache reload in Preset mode. ([#158](https://github.com/skyoo2003/acor/issues/158))
* The internal match engine no longer panics on an invalid (negative) rune supplied to `Engine.Stream`; such runes are treated as not in the alphabet, matching any other out-of-alphabet character. ([#159](https://github.com/skyoo2003/acor/issues/159))
* `FindParallel` and `FindIndexParallel` now clamp a negative `ParallelOptions.Overlap` to 0, which previously pushed each chunk's start past the previous boundary and silently dropped any match in between. ([#159](https://github.com/skyoo2003/acor/issues/159))
* `FindParallel` now returns a deduplicated result set regardless of text size; the single-chunk fast path previously returned `Find`'s per-occurrence multiplicity, contradicting the dedup contract the multi-chunk path already followed. ([#159](https://github.com/skyoo2003/acor/issues/159))

### Documentation

* Documented on `FindParallel` and `FindIndexParallel` that a keyword longer than `opts.Overlap` straddling a chunk boundary can be missed, and that `Overlap` should be set to at least the longest expected keyword length. ([#159](https://github.com/skyoo2003/acor/issues/159))

## [v0.9.0](https://github.com/skyoo2003/acor/releases/tag/v0.9.0) - 2026-07-20

### Added

* First-class Valkey support, with an opt-in real-server integration suite (`ACOR_INTEGRATION_ADDR`) and a CI job that validates against both redis and valkey service containers ([#150](https://github.com/skyoo2003/acor/issues/150))
* `NewGRPCServerWithObservability` adds OpenTelemetry tracing, Prometheus `grpc_server_*` metrics, zerolog request logging, and a live `grpc.health.v1` service backed by a background readiness poller ([#153](https://github.com/skyoo2003/acor/issues/153))

### Changed

* Migrated the Redis client from go-redis/v8 to go-redis/v9 (RESP3 by default, with automatic RESP2 fallback), with no public API change ([#150](https://github.com/skyoo2003/acor/issues/150))
* V2 cached `Find`/`FindIndex` now build the local automaton once per cache load instead of recomputing the failure function per character, making match latency constant with dictionary size (4-12x faster, ~300x fewer allocations on large dictionaries) ([#150](https://github.com/skyoo2003/acor/issues/150))
* The V2 cache no longer retains a full clone of the outputs map alongside the built automaton, halving its resident memory ([#150](https://github.com/skyoo2003/acor/issues/150))
* In-memory matching gained an ASCII direct-index fast path and rune-code threading through the failure traversal, making default Balanced `Find`/`FindIndex` ~2.2-2.4x faster (Speed up to 3x) with no change to results ([#152](https://github.com/skyoo2003/acor/issues/152))
* **BREAKING**: The gRPC adapter is now standard protobuf (`server/proto/acor/v1/acor.proto`, regenerate with `make proto`) instead of a hand-rolled JSON-over-gRPC codec, so existing gRPC clients must regenerate from the `.proto` ([#153](https://github.com/skyoo2003/acor/issues/153))
* **BREAKING**: Renamed the gRPC Prometheus metrics `acor_grpc_requests_total` and `acor_grpc_request_duration_seconds` to the standard `grpc_server_*` series; update dashboards and alerts ([#153](https://github.com/skyoo2003/acor/issues/153))
* gRPC request logs now use go-grpc-middleware field names (`grpc.method`, `grpc.code`, `grpc.time_ms`, message "finished call") instead of the previous "request completed" line; update log-based alerts ([#153](https://github.com/skyoo2003/acor/issues/153))
* Extracted the Aho-Corasick engines into `internal/engine` and storage into `internal/storage`, leaving the public API in `pkg/acor` unchanged ([#151](https://github.com/skyoo2003/acor/issues/151))

### Removed

* **BREAKING**: Unexported `PresetDefault`, an internal non-user-selectable sentinel identical to `PresetNone`; use `acor.PresetNone` instead ([#153](https://github.com/skyoo2003/acor/issues/153))

### Fixed

* Fixed a double-applied goto transition in the Speed and MemoryEfficient failure-link construction, which could make MemoryEfficient `Find` loop forever and Speed silently drop matches on suffix-nested keyword sets such as `{a, aa, aaa}` ([#152](https://github.com/skyoo2003/acor/issues/152))
* Fixed `PresetSpeed` silently dropping matches when a failure link pointed at a state inserted later in the trie; the DFA transition table is now filled breadth-first rather than in state-id order ([#152](https://github.com/skyoo2003/acor/issues/152))

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
