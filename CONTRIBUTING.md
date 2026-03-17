# Contributing to ACOR

Thank you for your interest in contributing to ACOR! This document provides guidelines and instructions for contributing.

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [How Can I Contribute?](#how-can-i-contribute)
- [Development Setup](#development-setup)
- [Development Workflow](#development-workflow)
- [Coding Standards](#coding-standards)
- [Commit Messages](#commit-messages)
- [Pull Requests](#pull-requests)

## Code of Conduct

This project and everyone participating in it is governed by the [CODE_OF_CONDUCT.md](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behavior by [opening an issue](https://github.com/skyoo2003/acor/issues/new).

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

- Go >= 1.24
- Redis >= 3.0 (for integration tests)
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

### Running All Checks

```sh
make all
```

This runs lint, test, and build.

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

```
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

1. Create a feature branch from `master`:
   ```sh
   git checkout -b feature/my-feature
   ```

2. Make your changes and commit them with clear messages

3. Push to your fork:
   ```sh
   git push origin feature/my-feature
   ```

4. Open a Pull Request against the `master` branch

5. Ensure all CI checks pass

6. Wait for review and address any feedback

### PR Checklist

- [ ] Tests pass (`make test`)
- [ ] Linting passes (`make lint`)
- [ ] Build succeeds (`make build`)
- [ ] Documentation updated if needed
- [ ] Commit messages follow guidelines

## Questions?

Feel free to [open an issue](https://github.com/skyoo2003/acor/issues/new) with the `question` label if you have any questions about contributing.

Thank you for contributing!
