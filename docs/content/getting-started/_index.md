---
title: "Getting Started"
weight: 1
---

# Getting Started

This section covers everything you need to start using ACOR.

## Prerequisites

- Go >= 1.25
- Redis >= 3.0 or Valkey >= 7.2

## Installation

```bash
go get github.com/skyoo2003/acor/pkg/acor@latest
```

## Next Steps

- [Installation](installation/) - Detailed setup instructions
- [Quick Start](quick-start/) - Your first ACOR application

## Runnable Examples

Three complete programs live in the repository and build against the current API:
[`examples/basic`](https://github.com/skyoo2003/acor/tree/main/examples/basic),
[`examples/batch`](https://github.com/skyoo2003/acor/tree/main/examples/batch), and
[`examples/parallel`](https://github.com/skyoo2003/acor/tree/main/examples/parallel).
Each expects a reachable Redis at `localhost:6379` and uses its own collection name,
so running one does not disturb another.

```bash
go run ./examples/basic
```

## Continue Learning

After getting started, explore the [Guides](../guides/) for advanced usage patterns.
