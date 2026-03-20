---
title: "Custom Storage"
weight: 1
---

# Custom Storage

Implement custom storage backends for ACOR.

## Overview

ACOR uses the `KVStorage` interface to abstract storage operations. You can implement custom backends for:

- In-memory storage (testing)
- Alternative databases
- Custom caching layers

## KVStorage Interface

```go
type KVStorage interface {
    Get(ctx context.Context, key string) (string, error)
    Set(ctx context.Context, key string, value interface{}) error
    HGetAll(ctx context.Context, key string) (map[string]string, error)
    HSet(ctx context.Context, key string, values ...interface{}) error
    SAdd(ctx context.Context, key string, members ...interface{}) error
    SMembers(ctx context.Context, key string) ([]string, error)
    SRem(ctx context.Context, key string, members ...interface{}) error
    SCard(ctx context.Context, key string) (int64, error)
    SIsMember(ctx context.Context, key, member string) (bool, error)
    ZAdd(ctx context.Context, key string, members ...*Z) error
    ZRange(ctx context.Context, key string, start, stop int64) ([]string, error)
    ZRank(ctx context.Context, key, member string) (int64, error)
    ZScore(ctx context.Context, key, member string) (float64, error)
    ZCard(ctx context.Context, key string) (int64, error)
    ZRem(ctx context.Context, key string, members ...interface{}) error
    Del(ctx context.Context, keys ...string) error
    Exists(ctx context.Context, keys ...string) (int64, error)
    TxPipelined(ctx context.Context, fn func(Pipeliner) error) error
    Close() error
}
```

## Example: In-Memory Storage

```go
package main

import (
    "context"
    "fmt"
    "sync"

    "github.com/skyoo2003/acor/pkg/acor"
)

type MemoryStorage struct {
    mu     sync.RWMutex
    data   map[string]string
    sets   map[string]map[string]struct{}
    hashes map[string]map[string]string
    zsets  map[string]map[string]float64
}

func NewMemoryStorage() *MemoryStorage {
    return &MemoryStorage{
        data:   make(map[string]string),
        sets:   make(map[string]map[string]struct{}),
        hashes: make(map[string]map[string]string),
        zsets:  make(map[string]map[string]float64),
    }
}

func (m *MemoryStorage) Get(ctx context.Context, key string) (string, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.data[key], nil
}

func (m *MemoryStorage) Set(ctx context.Context, key string, value interface{}) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    s, ok := value.(string)
    if !ok {
        return fmt.Errorf("unsupported value type: %T", value)
    }
    m.data[key] = s
    return nil
}

func (m *MemoryStorage) SAdd(ctx context.Context, key string, members ...interface{}) error {
    m.mu.Lock()
    defer m.mu.Unlock()
    if m.sets[key] == nil {
        m.sets[key] = make(map[string]struct{})
    }
    for _, member := range members {
        s, ok := member.(string)
        if !ok {
            return fmt.Errorf("unsupported member type: %T", member)
        }
        m.sets[key][s] = struct{}{}
    }
    return nil
}

func (m *MemoryStorage) SMembers(ctx context.Context, key string) ([]string, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()
    var members []string
    for member := range m.sets[key] {
        members = append(members, member)
    }
    return members, nil
}

// Implement remaining methods...

func (m *MemoryStorage) Close() error {
    return nil
}
```

## Using Custom Storage

Currently, ACOR uses Redis internally. Custom storage support is planned for future releases.

For testing, use miniredis which provides a Redis-compatible in-memory implementation:

```go
import (
    "testing"
    "github.com/alicebob/miniredis/v2"
    "github.com/redis/go-redis/v9"
)

func TestWithMiniredis(t *testing.T) {
    mr, err := miniredis.Run()
    if err != nil {
        t.Fatal(err)
    }
    defer mr.Close()

    client := redis.NewClient(&redis.Options{
        Addr: mr.Addr(),
    })
    defer client.Close()

    // Use client with ACOR
}
```

## Pipeliner Interface

For transaction support, implement `Pipeliner`:

```go
type Pipeliner interface {
    SAdd(ctx context.Context, key string, members ...interface{}) error
    HSet(ctx context.Context, key string, values ...interface{}) error
    ZAdd(ctx context.Context, key string, members ...*Z) error
    Del(ctx context.Context, keys ...string) error
}
```

## Z Type

The sorted set member type:

```go
type Z struct {
    Score  float64
    Member string
}
```
