package acor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

func TestV2GetOrLoadCacheNoCache(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		cache:   nil,
		logger:  &testLogger{},
	}

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `["","h","he"]`,
		"suffixes": `["","e","eh"]`,
		"version":  "100",
	})
	client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he": `["he"]`,
	})

	prefixes, outputs, err := ops.getOrLoadCache(context.Background())
	if err != nil {
		t.Fatalf("getOrLoadCache() error: %v", err)
	}
	if len(prefixes) != 3 {
		t.Errorf("len(prefixes) = %d, want 3", len(prefixes))
	}
	if len(outputs) != 1 {
		t.Errorf("len(outputs) = %d, want 1", len(outputs))
	}
}

func TestV2PublishInvalidate(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	cache := &trieCache{}
	cache.set([]string{"a"}, map[string][]string{"a": {"a"}})

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		cache:   cache,
		logger:  &testLogger{},
	}

	ops.publishInvalidate(context.Background())

	_, _, valid := cache.get()
	if valid {
		t.Error("cache should be invalid after publishInvalidate")
	}
}

func TestV2FetchTrieData(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he","she"]`,
		"prefixes": `["","h","he","s","sh","she"]`,
		"version":  "100",
	})
	client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he":  `["he"]`,
		"she": `["he","she"]`,
	})

	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		logger:  &testLogger{},
	}

	prefixes, outputs, err := ops.fetchTrieData(context.Background())
	if err != nil {
		t.Fatalf("fetchTrieData() error: %v", err)
	}
	if len(prefixes) != 6 {
		t.Errorf("len(prefixes) = %d, want 6", len(prefixes))
	}
	if len(outputs) != 2 {
		t.Errorf("len(outputs) = %d, want 2", len(outputs))
	}
}

func TestV2LoadCache(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `["","h","he"]`,
		"suffixes": `["","e","eh"]`,
		"version":  "100",
	})
	client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he": `["he"]`,
	})

	cache := &trieCache{}
	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		cache:   cache,
		logger:  &testLogger{},
	}

	if err := ops.loadCache(context.Background()); err != nil {
		t.Fatalf("loadCache() error: %v", err)
	}

	prefixes, outputs, valid := cache.get()
	if !valid {
		t.Fatal("cache should be valid after loadCache")
	}
	if len(prefixes) != 3 {
		t.Errorf("len(prefixes) = %d, want 3", len(prefixes))
	}
	if len(outputs) != 1 {
		t.Errorf("len(outputs) = %d, want 1", len(outputs))
	}
}

func TestNewTrieCache(t *testing.T) {
	cache := &trieCache{}
	_, _, valid := cache.get()
	if valid {
		t.Error("new cache should not be valid")
	}
}

func TestV2GetOrLoadCacheDoubleCheck(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he"]`,
		"prefixes": `["","h","he"]`,
		"suffixes": `["","e","eh"]`,
		"version":  "100",
	})
	client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he": `["he"]`,
	})

	cache := &trieCache{}
	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		cache:   cache,
		logger:  &testLogger{},
	}

	prefixes, outputs, err := ops.getOrLoadCache(context.Background())
	if err != nil {
		t.Fatalf("getOrLoadCache() error: %v", err)
	}
	if len(prefixes) != 3 {
		t.Errorf("len(prefixes) = %d, want 3", len(prefixes))
	}
	if len(outputs) != 1 {
		t.Errorf("len(outputs) = %d, want 1", len(outputs))
	}

	prefixes2, outputs2, err := ops.getOrLoadCache(context.Background())
	if err != nil {
		t.Fatalf("second getOrLoadCache() error: %v", err)
	}
	if len(prefixes2) != len(prefixes) {
		t.Errorf("second call returned different prefixes")
	}
	if len(outputs2) != len(outputs) {
		t.Errorf("second call returned different outputs")
	}
}

func TestV2PublishInvalidateWithPublishError(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.Close()

	cache := &trieCache{}
	cache.set([]string{"a"}, map[string][]string{"a": {"a"}})

	ops := &v2Operations{
		storage: newRedisStorage(redis.NewClient(&redis.Options{Addr: "localhost:1"})),
		client:  redis.NewClient(&redis.Options{Addr: "localhost:1"}),
		name:    "test",
		cache:   cache,
		logger:  &testLogger{},
	}
	defer func() { _ = ops.client.Close() }()

	ops.publishInvalidate(context.Background())

	_, _, valid := cache.get()
	if valid {
		t.Error("cache should be invalid even if publish fails")
	}
}

func TestV2LoadCacheError(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	cache := &trieCache{}
	ops := &v2Operations{
		storage: newRedisStorage(client),
		client:  client,
		name:    "test",
		cache:   cache,
		logger:  &testLogger{},
	}

	mr.Close()

	err := ops.loadCache(context.Background())
	if err == nil {
		t.Fatal("expected error when Redis is closed")
	}
}
