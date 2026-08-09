---
title: ACOR Documentation
hero_title: Aho-Corasick on Redis, with one API across multiple topologies.
hero_text: ACOR stores and queries Aho-Corasick patterns in Redis, and reaches you three ways — a Go library, an acor CLI, and an experimental server module speaking HTTP and gRPC. All three drive the same collection, across standalone Redis, Sentinel, Cluster, and Ring.
---

## Getting Started

ACOR requires Go 1.25 or newer and Redis 3.0 or newer, or Valkey 7.2 or newer.

```sh
go get github.com/skyoo2003/acor/pkg/acor@latest
```

```go
package main

import (
  "fmt"

  "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
  ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr: "localhost:6379",
    Name: "sample",
  })
  if err != nil {
    panic(err)
  }
  defer ac.Close()

  _, _ = ac.Add("redis")
  matched, _ := ac.Find("redis-backed matching")
  fmt.Println(matched)
}
```

## Explore the Documentation

<div class="doc-grid">
  <a class="doc-card" href="getting-started/">
    <strong>Getting Started</strong>
    <span>Install ACOR and build your first matcher.</span>
  </a>
  <a class="doc-card" href="guides/">
    <strong>Guides</strong>
    <span>Use batch, parallel, and Redis-backed engines.</span>
  </a>
  <a class="doc-card" href="reference/">
    <strong>API Reference</strong>
    <span>Review public APIs and storage schemas.</span>
  </a>
  <a class="doc-card" href="operations/">
    <strong>Operations</strong>
    <span>Deploy, monitor, and troubleshoot ACOR.</span>
  </a>
  <a class="doc-card" href="server/">
    <strong>Server</strong>
    <span>Serve a collection over HTTP or gRPC.</span>
  </a>
  <a class="doc-card" href="cli/">
    <strong>CLI</strong>
    <span>Drive a collection from the shell, nineteen commands.</span>
  </a>
  <a class="doc-card" href="extending/">
    <strong>Extending</strong>
    <span>Why Redis is the only backend, and how to test without one.</span>
  </a>
</div>
