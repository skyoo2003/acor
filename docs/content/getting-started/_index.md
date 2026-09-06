---
title: "Getting Started"
weight: 1
---

# Getting Started

- [Installation](installation/) — prerequisites, the package, and the `acor` CLI
- [Quick Start](quick-start/) — a first matcher, and every Redis topology

## Runnable examples

Three complete programs in the repository build against the current API:
[`examples/basic`](https://github.com/skyoo2003/acor/tree/main/examples/basic),
[`examples/batch`](https://github.com/skyoo2003/acor/tree/main/examples/batch), and
[`examples/parallel`](https://github.com/skyoo2003/acor/tree/main/examples/parallel).
Each expects Redis at `localhost:6379` and uses its own collection name, so one does not
disturb another.

```bash
go run ./examples/basic
```
