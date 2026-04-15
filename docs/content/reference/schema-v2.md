---
title: "Schema V2 (Optimized)"
weight: 3
---

# Schema V2 (Optimized)

V2 is the recommended schema for ACOR. It uses a fixed 3 keys per collection.

## Overview

V2 consolidates storage into 3 keys:

| Key Pattern | Purpose |
|-------------|---------|
| `{name}:trie` | Serialized trie structure (keywords, prefixes, suffixes, version) |
| `{name}:outputs` | All output mappings (state -> keywords) |
| `{name}:nodes` | Node metadata (migration only, cleaned up by flush) |

## Performance Characteristics

| Operation | Complexity |
|-----------|------------|
| Find() | 3 RTT (fixed), 0 RTT with EnableCache |
| Add() | 2-3 RTT |

## Comparison with V1

| Metric | V1 | V2 | Improvement |
|--------|----|----|-------------|
| Keys per 100K keywords | ~500K | 3 | 166,667x |
| Find() RTT | 3-5 per state | 3 total | 50-60x |
| Memory efficiency | Lower | Higher | 10x+ |

## Architecture

```mermaid
graph TB
    subgraph V2 Schema
        A[trie key] --> B[Serialized Trie]
        C[outputs key] --> D[Output Map]
        E[nodes key] --> F[Node Metadata - migration only]
    end

    G[Find Operation] --> A
    G --> C
    G -.-> E
```

## Enabling V2

V2 is automatically used for new collections. No configuration needed.

```go
ac, err := acor.Create(&acor.AhoCorasickArgs{
    Addr: "localhost:6379",
    Name: "my-v2-collection",
})
if err != nil {
    log.Fatal(err)
}
// Automatically uses V2 schema
```

## Migration from V1

```bash
# Check current schema
acor -name mycollection schema-version

# Preview migration
acor -name mycollection migrate --dry-run

# Execute migration
acor -name mycollection migrate
```

## Key Structure

### trie key

Stores the serialized trie as a hash with four fields:

```text
{collection}:trie
  keywords -> ["keyword1", "keyword2", ...]
  prefixes  -> ["", "h", "he", ...]
  suffixes  -> ["", "e", "eh", ...]
  version   -> <int64 optimistic lock>
```

### outputs key

Stores output keywords per trie state as a hash:

```text
{collection}:outputs
  he  -> ["he"]
  she -> ["he", "she"]
```

### nodes key

Stores node-level metadata:

```text
{collection}:nodes
  keyword1 -> {"count": 1, "depth": 3}
```

## Recommendation

**Use V2 for all new collections.** It provides significantly better performance and lower resource usage.
