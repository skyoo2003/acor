# Changelog

All notable changes to this project will be documented in this file.

## [v1.5.1](https://github.com/skyoo2003/acor/releases/tag/v1.5.1) - 2026-04-14

### Fixed

- Prevent pub/sub self-message from invalidating local cache ([#106](https://github.com/skyoo2003/acor/issues/106))

### Documentation

- Fix broken Hugo documentation links with relative paths ([#107](https://github.com/skyoo2003/acor/issues/107))
- Add single page template to fix broken links ([#105](https://github.com/skyoo2003/acor/issues/105))

## [v1.5.0](https://github.com/skyoo2003/acor/releases/tag/v1.5.0) - 2026-04-13

### Added

- Add local caching for Find/FindIndex operations with Redis Pub/Sub invalidation ([#99](https://github.com/skyoo2003/acor/issues/99))
- Add *Context variants for all public API methods (AddContext, FindContext, etc.) ([#96](https://github.com/skyoo2003/acor/issues/96))
- Add BatchOptions support to RemoveMany for API symmetry with AddMany ([#96](https://github.com/skyoo2003/acor/issues/96))
- Add V1+Cache guard to prevent unsupported schema/cache configuration ([#96](https://github.com/skyoo2003/acor/issues/96))
- Add internal Operations interface with Strategy pattern for V1/V2 dispatch ([#102](https://github.com/skyoo2003/acor/issues/102))
- Add KVStorage interface for Redis dependency injection ([#102](https://github.com/skyoo2003/acor/issues/102))
- Add redisStorage adapter wrapping go-redis UniversalClient ([#102](https://github.com/skyoo2003/acor/issues/102))
- Add CI hardening: race detector, coverage threshold (70%), and fuzz testing ([#96](https://github.com/skyoo2003/acor/issues/96))
- Add Makefile targets: vet, lint-fix, fuzz, race ([#96](https://github.com/skyoo2003/acor/issues/96))
- Add Issue/PR templates and SECURITY.md ([#96](https://github.com/skyoo2003/acor/issues/96))

### Changed

- Go required version 1.25 or higher ([#97](https://github.com/skyoo2003/acor/issues/97))
- Refactor AhoCorasick struct to use KVStorage DI and operations Strategy pattern ([#102](https://github.com/skyoo2003/acor/issues/102))
- Activate error helpers (newRedisError, newOperationError) in V1 and V2 operations ([#96](https://github.com/skyoo2003/acor/issues/96))
- Replace mustJSON panic with toJSON error return for safer error propagation ([#96](https://github.com/skyoo2003/acor/issues/96))
- Rename underscore-prefixed methods in trie.go (buildTrie, gotoNode, failNode, collectOutputs) ([#96](https://github.com/skyoo2003/acor/issues/96))
- Split monolithic test file into feature-specific test files ([#96](https://github.com/skyoo2003/acor/issues/96))
- Fix README example API names (BatchModeTransactional, ChunkBoundaryWord, Boundary) ([#96](https://github.com/skyoo2003/acor/issues/96))
- Bump dependencies: go-redis, gRPC, OpenTelemetry, zerolog ([#100](https://github.com/skyoo2003/acor/issues/100), [#103](https://github.com/skyoo2003/acor/issues/103))
- Bump CI dependencies: GitHub Actions ([#101](https://github.com/skyoo2003/acor/issues/101))

### Removed

- Remove unused non-Context wrapper functions in trie.go ([#96](https://github.com/skyoo2003/acor/issues/96))

### Documentation

- Add cross-references between Hugo documentation pages ([#98](https://github.com/skyoo2003/acor/issues/98))
- Add comprehensive Hugo documentation: guides, API reference, deployment, monitoring, troubleshooting ([#96](https://github.com/skyoo2003/acor/issues/96))

## [v1.4.0](https://github.com/skyoo2003/acor/releases/tag/v1.4.0) - 2026-03-18

### Added

- Add CLI commands: migrate, migrate-rollback, schema-version ([#83](https://github.com/skyoo2003/acor/issues/83))
- Add RollbackToV1() for safe rollback when V1 keys are kept ([#83](https://github.com/skyoo2003/acor/issues/83))
- Add V2 schema with 80-85% fewer Redis round trips and 99% fewer keys ([#83](https://github.com/skyoo2003/acor/issues/83))
- Add MigrateV1ToV2() with dry-run support and progress callbacks ([#83](https://github.com/skyoo2003/acor/issues/83))
- Add parallel matching (FindParallel, FindIndexParallel) with configurable chunk boundaries ([#84](https://github.com/skyoo2003/acor/issues/84))
- Add batch operations (AddMany, RemoveMany, FindMany) with BestEffort and Transactional modes ([#84](https://github.com/skyoo2003/acor/issues/84))
- Add pkg/metrics: Prometheus metrics registry with HTTP/gRPC middleware ([#85](https://github.com/skyoo2003/acor/issues/85))
- Add pkg/health: Kubernetes-compatible health checks (liveness/readiness) for HTTP/gRPC ([#85](https://github.com/skyoo2003/acor/issues/85))
- Add observability integration to gRPC server (NewGRPCServerWithObservability) ([#85](https://github.com/skyoo2003/acor/issues/85))
- Add pkg/tracing: OpenTelemetry distributed tracing with HTTP/gRPC middleware ([#85](https://github.com/skyoo2003/acor/issues/85))
- Add pkg/logging: zerolog structured logging with HTTP/gRPC middleware ([#85](https://github.com/skyoo2003/acor/issues/85))

### Changed

- Go required version 1.24 or higher ([#80](https://github.com/skyoo2003/acor/issues/80))
- Add SchemaVersion field to AhoCorasickArgs for explicit schema selection ([#83](https://github.com/skyoo2003/acor/issues/83))
- **BREAKING**: New collections now default to V2 schema. Use SchemaVersion: 1 to keep V1 behavior ([#83](https://github.com/skyoo2003/acor/issues/83))
- chore: update pre-commit hooks to latest versions ([#88](https://github.com/skyoo2003/acor/issues/88))

### Removed

- Remove unused BatchSize field from MigrationOptions (was never implemented) ([#83](https://github.com/skyoo2003/acor/issues/83))

### Fixed

- Correct migration progress step constants ([#83](https://github.com/skyoo2003/acor/issues/83))

### Documentation

- Add performance tradeoffs and migration notes to README ([#83](https://github.com/skyoo2003/acor/issues/83))

## [v1.3.0](https://github.com/skyoo2003/acor/releases/tag/v1.3.0) - 2026-03-14

### Added

- Add index APIs for find and suggest ([#67](https://github.com/skyoo2003/acor/issues/67))
- Add Redis topology-aware client selection ([#69](https://github.com/skyoo2003/acor/issues/69))
- Add HTTP and gRPC server adapters ([#70](https://github.com/skyoo2003/acor/issues/70))
- Add CLI support ([#75](https://github.com/skyoo2003/acor/issues/75))

### Changed

- Handle Redis errors during AC execution ([#68](https://github.com/skyoo2003/acor/issues/68))

### Documentation

- Add GitHub Pages documentation and deployment workflow ([#76](https://github.com/skyoo2003/acor/issues/76))

## [v1.2.0](https://github.com/skyoo2003/acor/releases/tag/v1.2.0) - 2021-07-09

### Changed

- Changed to standard project structure ([#2](https://github.com/skyoo2003/acor/issues/2))
- Changed supported Go versions ([#5](https://github.com/skyoo2003/acor/issues/5))
- Changed RedisAlreadyClosed error name ([#7](https://github.com/skyoo2003/acor/issues/7))

### Fixed

- Fixed NodeKey output was not written ([#13](https://github.com/skyoo2003/acor/issues/13))

## [v1.1.0](https://github.com/skyoo2003/acor/releases/tag/v1.1.0) - 2020-11-17

### Changed

- Bump go-redis/redis libraries
- Applied go modules
- Bump Go required version (1.8 -> 1.11)

## [v1.0.0](https://github.com/skyoo2003/acor/releases/tag/v1.0.0) - 2017-06-29

### Added

- Created ACOR APIs
