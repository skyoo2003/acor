# Contributing to ACOR

Thank you for your interest in contributing to ACOR! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How Can I Contribute?](#how-can-i-contribute)
- [Development Setup](#development-setup)
- [Development Workflow](#development-workflow)
- [Repository Layout](#repository-layout)
- [Coding Standards](#coding-standards)
- [Commit Messages](#commit-messages)
- [Pull Requests](#pull-requests)

## Code of Conduct

This project and everyone participating in it is governed by the [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior through [GitHub Security Advisory](https://github.com/skyoo2003/acor/security/advisories/new).

## How Can I Contribute?

### Reporting Bugs

Before creating bug reports, please check the [existing issues](https://github.com/skyoo2003/acor/issues) as you might find that the issue has already been reported.

When creating a bug report, please include:

- **A clear and descriptive title**
- **Steps to reproduce the problem**
- **Expected behavior**
- **Actual behavior**
- **Environment details** (Go version, Redis version, OS)
- **Code sample** if applicable

### Suggesting Enhancements

Enhancement suggestions are tracked as GitHub issues. When creating an enhancement suggestion, include:

- **A clear and descriptive title**
- **A detailed description of the proposed enhancement**
- **Examples of how the enhancement would be used**
- **Why this enhancement would be useful**

### Pull Requests

- Fill in the required template
- Do not include issue numbers in the PR title
- Follow the coding standards
- Include tests for new functionality
- Update documentation as needed

## Development Setup

### Prerequisites

- Go >= 1.25 (CI builds and tests on 1.25 and 1.26)
- Redis >= 3.0, or Valkey >= 7.2 (for integration tests) — the same floor stated in
  [`README.md`](README.md) and the
  [installation guide](docs/content/getting-started/installation.md). CI exercises
  `redis:8` and `valkey/valkey:8` only, so anything older is supported but not
  covered by a build
- golangci-lint (for linting)
- pre-commit (optional, for git hooks)

### Getting Started

1. Fork the repository

2. Clone your fork:

   ```sh
   git clone https://github.com/YOUR_USERNAME/acor.git
   cd acor
   ```

3. Install dependencies:

   ```sh
   go mod download
   ```

4. Set up git hooks (optional):

   ```sh
   make setup
   ```

## Development Workflow

### Running Tests

```sh
make test
```

Or with verbose output:
```sh
go test -v ./...
```

With race detection:
```sh
make race
```

### Building

```sh
make build
```

The binary will be output to `dist/acor`.

### Linting

```sh
make lint
```

We use [golangci-lint](https://golangci-lint.run/) with configuration in `.golangci.yaml`.

Auto-fix linting issues:
```sh
make lint-fix
```

### Static Analysis

```sh
make vet
```

### Coverage

```sh
make coverage
```

Generates `coverage.out` and `coverage.html`.

### Fuzzing

```sh
make fuzz
```

### Running All Checks

```sh
make all
```

This runs, in order, `vet`, `lint`, `test`, `build`, `docs-verify`, `license-check`,
and `api-check` (`Makefile:8`). Three of those gate things the sections above do not
cover:

| Target | What it gates |
| ------------- | ------------------------------------------------------------- |
| `docs-verify` | Compiles the Go blocks in `README.md` and `docs/content/**` that opt in with a `<!-- doccheck -->` or `<!-- doccheck:server -->` marker — 25 of the 93 Go fences today. An unmarked block is never compiled, so add the marker when you add an example that should not be allowed to rot |
| `api-check` | Rewrites `api/v1.txt` and fails on the diff, so an unrecorded change to the public API cannot merge. See [`api/README.md`](api/README.md) |
| `tidy-check` | Runs `go mod tidy` across all three modules and fails on the diff. Not part of `all` — it runs as a pre-commit hook |

`make setup` installs `lint`, `test`, `build`, `tidy-check`, `license-check`, and
`api-check` as pre-commit hooks, along with an SPDX-header check, so they run before the
commit rather than in review. **`docs-verify` is not among them** — a broken example is
caught by `make all` or by CI, not by the hook.

### Benchmarks

```sh
make bench          # pkg/acor and internal/engine microbenchmarks
make bench-module   # public-API timings, memory, and propagation (separate module)
make bench-v3       # full V3 matrix via scripts/benchmark-v3.sh
```

`bench-module` requires `ACOR_INTEGRATION_ADDR`; without it the figures are measured
against miniredis, which has no round-trip cost and so must never be published as
timings. `bench` produces the evidence behind
[`docs/content/reference/benchmarks.md`](docs/content/reference/benchmarks.md).

### Regenerating gRPC Code

```sh
make proto
```

Regenerates the server's protobuf and gRPC code from `server/proto/acor/v1/acor.proto`.
Requires `protoc`, `protoc-gen-go`, and `protoc-gen-go-grpc` on `PATH` — it is the one
target that needs a toolchain `go` does not install for you.

### Third-Party Notices

```sh
make license-check
```

Regenerates `NOTICE` from the modules linked into the `acor` binary and fails if
the committed file is out of date. Adding a dependency fails this target until
its license is read by hand and recorded in `tools/licensesnap/main.go`.

### Changelog Fragment

```sh
changie new
```

Writes a YAML fragment under `changes/unreleased/`. Commit it alongside your change —
it is what becomes the release note. Skip it for internal-only changes (CI, tests,
refactors); the [PR template](.github/PULL_REQUEST_TEMPLATE.md) states that rule, and
[RELEASE.md](RELEASE.md) covers what happens to fragments when a release is cut.

**Write the body as one sentence — two at the very most.** A second sentence
earns its place only by carrying a consequence: a migration step, a measured
number, or what deliberately did not change. The investigation, the file list,
and the rejected alternatives belong in the pull request; a reader scanning the
changelog wants to know what changed and whether it affects them.

`.changie.yaml` enforces the shape, so an entry that wants paragraphs fails at
`changie new` rather than at review:

| Constraint | Limit                       | Enforced by                             |
|------------|-----------------------------|-----------------------------------------|
| Length     | 5-400 characters            | `body.minLength` / `body.maxLength`     |
| Lines      | one — no paragraph breaks   | `body.block: false`                     |
| Sentences  | one, two at the very most   | convention; reviewers, not the tool     |

The 400-character cap is a ceiling, not a target: most entries land well under
it, and an entry that needs every character is usually two entries.

```text
Good: `Create` now returns `ErrNilArgs` instead of panicking on a nil argument.
Good: V1 collections are now read-only; `Add` and `Remove` return `ErrV1ReadOnly`,
      while every read path and `MigrateV1ToV2` are unchanged.
Bad:  A paragraph reconstructing how the bug was found, which files moved, and
      why three alternative designs were rejected.
```

### Cleaning

```sh
make clean
```

Removes `dist/`, `coverage.out`, and `coverage.html`.

## Repository Layout

Three Go modules live in this repository:

| Path          | Module                                 | Status                                        |
| ------------- | -------------------------------------- | --------------------------------------------- |
| `pkg/acor`    | `github.com/skyoo2003/acor`            | The library. Public API.                      |
| `cmd/acor`    | same module                            | The CLI, released as a binary and image.      |
| `server/`     | `github.com/skyoo2003/acor/server`     | Experimental. Separately versioned, untagged. |
| `benchmarks/` | `github.com/skyoo2003/acor/benchmarks` | Test-only. Never published.                   |

The library's import path is `github.com/skyoo2003/acor/pkg/acor` — the `pkg/`
segment included. It stays that way deliberately: it is the path every released
version has published, and moving the package to the module root would break every
existing import to save eight characters. A proposal to flatten it needs to carry a
migration plan for that, not just the shorter path.

Both non-root modules keep a `replace` pointing at this checkout so local builds
and CI compile against the core in the working tree. Go ignores a dependency's
`replace`, so `server/go.mod`'s `require` for the core must name a released tag —
that is the version an external consumer actually resolves.

## Coding Standards

- Follow [Effective Go](https://golang.org/doc/effective_go) guidelines
- Run `golangci-lint run ./...` before committing
- Write tests for new functionality
- Keep functions focused and concise
- Add documentation comments for exported types and functions

## Commit Messages

- Use the present tense ("Add feature" not "Added feature")
- Use the imperative mood ("Move cursor to..." not "Moves cursor to...")
- Limit the first line to 72 characters or less
- Reference issues and pull requests liberally after the first line

### Commit Message Format

```text
<type>: <subject>

<body>

<footer>
```

Types:
- `feat`: A new feature
- `fix`: A bug fix
- `docs`: Documentation changes
- `style`: Code style changes (formatting, semicolons, etc.)
- `refactor`: Code refactoring
- `test`: Adding or updating tests
- `chore`: Maintenance tasks

## Pull Requests

1. Create a feature branch from `main`:
   ```sh
   git checkout -b feature/my-feature
   ```

2. Make your changes and commit them with clear messages

3. Push to your fork:
   ```sh
   git push origin feature/my-feature
   ```

4. Open a Pull Request against the `main` branch

5. Ensure all CI checks pass

6. Wait for review and address any feedback

### PR Checklist

The checklist lives in
[`.github/PULL_REQUEST_TEMPLATE.md`](.github/PULL_REQUEST_TEMPLATE.md), and GitHub
pre-fills it when you open the PR. It is deliberately not copied here: a second copy is
a second thing to keep in sync, and this one had already drifted out of step with the
template.

`make all` covers the automated entries on that list — tests, vet, lint, build, and the
repository gates — in one command. The rest are judgement calls no target can make for
you: whether the documentation needs updating, whether the change warrants a changelog
fragment, and whether the commit messages read as they should.

## Questions?

Feel free to [open an issue](https://github.com/skyoo2003/acor/issues/new) with the `question` label if you have any questions about contributing.

## License

By contributing to ACOR, you agree that your contributions will be licensed under the [Apache License 2.0](LICENSE).

Thank you for contributing!
