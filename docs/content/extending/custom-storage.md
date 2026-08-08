---
title: "Custom Storage"
weight: 1
---

# Custom Storage

**ACOR does not support custom storage backends.** Redis is the only backend, and
`Create()` always builds it. This page explains why the interface you may have seen in
older releases is gone, and what to do instead for the case it was usually reached for:
testing.

## What changed in v1.5.0

Through `v1.4.0` the package exported a `KVStorage` interface, along with `Pipeliner`,
`Subscription`, `StringMapResult`, `PubSubMessage`, and `Z`. They were exported in
anticipation of pluggable backends.

They are unexported as of `v1.5.0`, for two reasons:

- **Nothing could be plugged in.** No public constructor accepted a `KVStorage`, and no
  public function returned one. You could name the interface and write an
  implementation, but there was no way to hand it to ACOR — so the capability the
  interface implied never existed.
- **Freezing it would have blocked the feature it was for.** The
  [compatibility policy](../../reference/compatibility/) forbids adding a method to an
  exported interface inside `v1`. `KVStorage` has 23 methods; had `v1.5.0` frozen it, a
  future backend needing one more operation would have had nowhere to put it for the
  rest of the `v1` line.

Leaving the shape unfrozen is what keeps pluggable storage possible. Publishing an
interface later is an addition, which `v1` allows; growing a frozen one is not.

## Testing without Redis

This is what the interface was most often reached for, and it has a better answer that
works today: [miniredis](https://github.com/alicebob/miniredis), an in-process
Redis-compatible server. ACOR's own test suite uses it, so the behavior you test against
is the behavior ACOR is tested against.

<!-- doccheck -->
```go
package example

import (
    "testing"

    "github.com/alicebob/miniredis/v2"
    "github.com/skyoo2003/acor/pkg/acor"
)

func TestWithMiniredis(t *testing.T) {
    mr := miniredis.RunT(t)

    ac, err := acor.Create(&acor.AhoCorasickArgs{
        Addr: mr.Addr(),
        Name: "test-collection",
    })
    if err != nil {
        t.Fatal(err)
    }
    defer func() { _ = ac.Close() }()

    if _, err := ac.Add("hello"); err != nil {
        t.Fatal(err)
    }

    matches, err := ac.Find("hello world")
    if err != nil {
        t.Fatal(err)
    }
    if len(matches) != 1 || matches[0] != "hello" {
        t.Fatalf("Find() = %v, want [hello]", matches)
    }
}
```

`miniredis` covers the commands ACOR issues, including the Lua scripts the V2 schema
uses for atomic writes. To exercise Pub/Sub-driven cache invalidation across instances,
point two instances at the same miniredis address.

## Reducing Redis traffic

If the reason for a custom backend was to avoid Redis round trips on reads rather than
to replace Redis, that is already available without one:

- **`Preset`** serves reads from a local automaton and touches Redis only on writes and
  invalidations. See [Presets](../../guides/presets/).
- **`EnableCache`** caches trie data locally and invalidates over Pub/Sub.

`CacheStats()` reports whether either is working — hit rate, rebuild cost, and
invalidation lag — without any Redis I/O of its own.

## Navigation

← [Extending](../)
