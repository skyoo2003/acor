---
title: "Custom Storage"
weight: 1
---

# Custom Storage

`acor.KVStorage` abstracts ACOR's storage operations.

> **V1 schema only.** Custom backends are supported only with `SchemaVersion:
> acor.SchemaV1`. The V2 schema and the preset engine rely on Redis Lua scripts
> and a raw Redis client, so `Preset` and `EnableCache` must be unset, and
> `MigrateV1ToV2`/`RollbackToV1` are unavailable (they return
> `acor.ErrMigrationRequiresRedis`). Misconfigurations return
> `acor.ErrCustomStorageRequiresV1`.

## Overview

Implement `acor.KVStorage` and pass it via `AhoCorasickArgs.Storage` to plug in
a custom backend instead of the built-in Redis adapter:

```go
ac, err := acor.Create(&acor.AhoCorasickArgs{
    Name:          "my-collection",
    SchemaVersion: acor.SchemaV1, // required for custom storage
    Storage:       myStorage,     // any acor.KVStorage implementation
})
```

When `Storage` is non-nil, `Create` uses it directly and ignores the Redis
connection fields (`Addr`, `Addrs`, `Password`, `DB`, ...). This enables:

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
    SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
    Pipeline() Pipeliner
    Publish(ctx context.Context, channel string, message interface{}) error
    Subscribe(ctx context.Context, channels ...string) Subscription
    Close() error
}
```

The `Z`, `Pipeliner`, `StringMapResult`, `Subscription`, and `PubSubMessage`
types referenced above are re-exported from the `acor` package (use
`acor.KVStorage`, `acor.Z`, etc.). Their definitions live in
[`internal/storage/interfaces.go`](https://github.com/skyoo2003/acor/blob/main/internal/storage/interfaces.go);
they are documented in [Helper Types](#helper-types) below.

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
    "github.com/go-redis/redis/v8"
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
    HGetAll(ctx context.Context, key string) StringMapResult
    ZAdd(ctx context.Context, key string, members ...*Z) error
    Del(ctx context.Context, keys ...string) error
    Exec(ctx context.Context) error
}
```

## Helper Types

These types are referenced by `KVStorage` and `Pipeliner` and are defined in
[`internal/storage/interfaces.go`](https://github.com/skyoo2003/acor/blob/main/internal/storage/interfaces.go).

`Z` — a sorted set member (score + value):

```go
type Z struct {
    Score  float64
    Member string
}
```

`StringMapResult` — a deferred result from a pipelined `HGetAll`; call `Val()`
after the pipeline's `Exec`:

```go
type StringMapResult interface {
    Val() map[string]string
}
```

`Subscription` — a pub/sub subscription returned by `Subscribe`:

```go
type Subscription interface {
    Receive(ctx context.Context) error
    Channel() <-chan PubSubMessage
    Close() error
}
```
