# Changelog
All notable changes to this project will be documented in this file.
## [v0.8.0](https://github.com/skyoo2003/acor/releases/tag/v0.8.0) - 2026-07-17
### Changed
* **BREAKING**: Merge Ultimate engine into Balanced and share Bloom pre-filter ([#138](https://github.com/skyoo2003/acor/issues/138))
* Split server and observability into separate module ([#139](https://github.com/skyoo2003/acor/issues/139))
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
* Stop cache listener on rollback and fix cancel context before stopping cache listener in Close ([#120](https://github.com/skyoo2003/acor/issues/120))
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
