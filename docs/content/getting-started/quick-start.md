---
title: "Quick Start"
weight: 2
---

# Quick Start

This guide walks you through building your first ACOR application.

## Basic Usage

```go
package main

import (
    "fmt"
    "github.com/skyoo2003/acor/pkg/acor"
)

func main() {
    args := &acor.AhoCorasickArgs{
        Addr: "localhost:6379",
        Name: "sample",
    }
    
    ac, err := acor.Create(args)
    if err != nil {
        panic(err)
    }
    defer ac.Close()

    keywords := []string{"he", "her", "him"}
    for _, k := range keywords {
        if _, err := ac.Add(k); err != nil {
            panic(err)
        }
    }

    matched, err := ac.Find("he is him")
    if err != nil {
        panic(err)
    }
    fmt.Println(matched)

    if err := ac.Flush(); err != nil {
        panic(err)
    }
}
```

## Redis Topologies

ACOR supports multiple Redis configurations:

### Standalone

```go
args := &acor.AhoCorasickArgs{
    Addr:     "localhost:6379",
    Password: "",
    DB:       0,
    Name:     "sample",
}
```

### Sentinel

```go
args := &acor.AhoCorasickArgs{
    Addrs:      []string{"localhost:26379", "localhost:26380"},
    MasterName: "mymaster",
    Password:   "",
    DB:         0,
    Name:       "sample",
}
```

### Cluster

```go
args := &acor.AhoCorasickArgs{
    Addrs:    []string{"localhost:7000", "localhost:7001", "localhost:7002"},
    Password: "",
    Name:     "sample",
}
```

### Ring

```go
args := &acor.AhoCorasickArgs{
    RingAddrs: map[string]string{
        "shard-1": "localhost:7000",
        "shard-2": "localhost:7001",
    },
    Password: "",
    DB:       0,
    Name:     "sample",
}
```

## Next Steps

- [Batch Operations](/guides/batch-operations/) - Optimize bulk operations
- [Parallel Matching](/guides/parallel-matching/) - Process large texts efficiently
- [API Reference](/reference/api/) - Complete API documentation
