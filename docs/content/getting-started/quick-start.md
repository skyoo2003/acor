---
title: "Quick Start"
weight: 2
---

# Quick Start

<!-- doccheck -->
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

    if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
        panic(err)
    }

    matched, err := ac.Find("he is him")
    if err != nil {
        panic(err)
    }
    fmt.Println(matched)
}
```

## Redis topologies

Pick exactly one set of connection fields — mixing them returns
`ErrRedisConflictingTopology`.

```go
// Standalone
args := &acor.AhoCorasickArgs{Addr: "localhost:6379", Name: "sample"}

// Sentinel — Addrs plus MasterName
args = &acor.AhoCorasickArgs{
    Addrs:      []string{"localhost:26379", "localhost:26380"},
    MasterName: "mymaster",
    Name:       "sample",
}

// Cluster — Addrs without MasterName
args = &acor.AhoCorasickArgs{
    Addrs: []string{"localhost:7000", "localhost:7001"},
    Name:  "sample",
}

// Ring — shard name to address
args = &acor.AhoCorasickArgs{
    RingAddrs: map[string]string{"shard-1": "localhost:7000", "shard-2": "localhost:7001"},
    Name:      "sample",
}
```

`Password`, `DB`, and the timeout and pool fields apply to every topology; `DB` is
rejected together with `Addrs`. Full field list:
[API Reference](../../reference/api/#ahocorasickargs).

## Next

[Batch operations](../../guides/batch-operations/) ·
[Parallel matching](../../guides/parallel-matching/) ·
[Preset engine](../../guides/preset-engine/) ·
[API reference](../../reference/api/)
