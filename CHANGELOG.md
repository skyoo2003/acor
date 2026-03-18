# Changelog

All notable changes to this project will be documented in this file.

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
