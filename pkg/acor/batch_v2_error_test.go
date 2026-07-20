// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestV2TryAddBadKeywordsJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `not-json`,
		"prefixes": `[""]`,
		"version":  "100",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryAddV2(context.Background(), "she")
	if err == nil {
		t.Fatal("expected error for bad JSON in keywords")
	}
}

func TestV2TryRemoveBadKeywordsJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `not-json`,
		"prefixes": `[""]`,
		"version":  "100",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryRemoveV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error for bad JSON in keywords")
	}
}

func TestV2TryRemoveVersionFallback(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `["","h","he"]`,
		"version":  "not-a-number",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryRemoveV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected concurrency conflict with bad version in Redis")
	}
}

func TestV2TryAddVersionFallback(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `[]`,
		"prefixes": `[""]`,
		"suffixes": `[""]`,
		"version":  "not-a-number",
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.tryAddV2(context.Background(), "he")
	if err == nil {
		t.Fatal("expected concurrency conflict with bad version in Redis")
	}
}

func TestV2FindIndexError(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: "localhost:1"})),
		client:  redis.NewClient(&redis.Options{Addr: "localhost:1"}),
		name:    "test",
		cache:   nil,
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	_, err := ops.findIndex(context.Background(), "hello")
	if err == nil {
		t.Fatal("expected error from closed Redis in findIndex")
	}
}

func TestV2GetOrLoadEngineError(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Close()

	cache := &trieCache{}
	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: "localhost:1"})),
		client:  redis.NewClient(&redis.Options{Addr: "localhost:1"}),
		name:    "test",
		cache:   cache,
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	_, err := ops.loadEngine(context.Background())
	if err == nil {
		t.Fatal("expected error from closed Redis in loadEngine")
	}
}

func TestV2FlushError(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: "localhost:1"})),
		client:  redis.NewClient(&redis.Options{Addr: "localhost:1"}),
		name:    "test",
		cache:   &trieCache{},
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	err := ops.flush(context.Background())
	if err == nil {
		t.Fatal("expected error from closed Redis in flush")
	}
}

func TestV2InfoBadPrefixesJSON(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `not-json`,
	})

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.info(context.Background())
	if err == nil {
		t.Fatal("expected error for bad JSON in info prefixes")
	}
}

func TestV2SuggestError(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: addr})),
		client:  redis.NewClient(&redis.Options{Addr: addr}),
		name:    "test",
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	_, err := ops.suggest(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error from closed Redis in suggest")
	}
}

func TestV2SuggestIndexError(t *testing.T) {
	mr := miniredis.RunT(t)
	addr := mr.Addr()
	mr.Close()

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: addr})),
		client:  redis.NewClient(&redis.Options{Addr: addr}),
		name:    "test",
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	_, err := ops.suggestIndex(context.Background(), "he")
	if err == nil {
		t.Fatal("expected error from closed Redis in suggestIndex")
	}
}
