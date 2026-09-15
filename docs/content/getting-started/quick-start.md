---
title: "Quick Start"
weight: 2
---

# Quick Start

<!-- doccheck -->
```go
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/skyoo2003/acor/pkg/acor"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ac, err := acor.CreateContext(ctx, &acor.AhoCorasickArgs{
		Addr: "localhost:6379",
		Name: "sample",
	})
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.AddManyContext(ctx, []string{"he", "her", "him"}, nil); err != nil {
		return fmt.Errorf("add keywords: %w", err)
	}

	matched, err := ac.FindMatchesContext(ctx, "he is him", nil)
	if err != nil {
		return fmt.Errorf("find matches: %w", err)
	}
	fmt.Println(matched)
	return nil
}
```

The setup context bounds construction I/O only. `Close` controls the instance
lifetime; use the `*Context` methods when an operation needs cancellation or a
timeout. The [API reference](../../reference/api/#creating-a-collection) documents
the full constructor and lifecycle contract.

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
