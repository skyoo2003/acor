package acor

import (
	"context"
	"errors"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

func TestV2Debug(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}

	ac.Debug()
}

func TestCreateWithDebug(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:  mr.Addr(),
		Name:  "test-debug",
		Debug: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, addErr := ac.Add("hello"); addErr != nil {
		t.Fatal(addErr)
	}

	matched, err := ac.Find("hello world")
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(matched, "hello") {
		t.Errorf("Find() = %v, want [hello]", matched)
	}
}

func TestCreateWithCustomLogger(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	logger := &testLogger{}
	ac, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   "test-logger",
		Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}
}

func TestCreateUnsupportedSchema(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	_, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-bad-schema",
		SchemaVersion: 99,
	})
	if err == nil {
		t.Fatal("expected error for unsupported schema version")
	}
}

func TestCreateCacheRequiresV2(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	_, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-cache-v1",
		SchemaVersion: SchemaV1,
		EnableCache:   true,
	})
	if err == nil {
		t.Fatal("expected error for cache with V1 schema")
	}
	if !errors.Is(err, ErrCacheRequiresV2) {
		t.Errorf("expected ErrCacheRequiresV2, got %v", err)
	}
}

func TestDebugV1WithNoData(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		logger:        &testLogger{},
		schemaVersion: SchemaV1,
		ops: &v1Operations{
			storage: newRedisStorage(client),
			name:    "test",
			ac:      nil,
		},
	}

	ac.Debug()
}

func TestDebugV1WithClosedRedis(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		logger:        &testLogger{},
		schemaVersion: SchemaV1,
		ops: &v1Operations{
			storage: newRedisStorage(client),
			name:    "test",
			ac:      nil,
		},
	}

	mr.Close()

	ac.Debug()
}

func TestDebugV2WithData(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	client.HSet(ctx, "{test}:trie", map[string]interface{}{
		"keywords": `["he","she"]`,
		"prefixes": `["","h","he","s","sh","she"]`,
		"suffixes": `["","e","eh","s","hs","ehs"]`,
		"version":  "100",
	})
	client.HSet(ctx, "{test}:outputs", map[string]interface{}{
		"he":  `["he"]`,
		"she": `["he","she"]`,
	})
	client.HSet(ctx, "{test}:nodes", map[string]interface{}{
		"he": `["h","he"]`,
	})

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           ctx,
		name:          "test",
		logger:        &testLogger{},
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
		},
	}

	ac.Debug()
}

func TestDebugV2WithClosedRedis(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		logger:        &testLogger{},
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
		},
	}

	mr.Close()

	ac.Debug()
}

func TestInitV2HSetError(t *testing.T) {
	mr := miniredis.RunT(t)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	mr.Close()

	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV2,
	}

	err := ac.init()
	if err == nil {
		t.Fatal("expected error from closed Redis in init()")
	}
}

func TestInitV1ZAddError(t *testing.T) {
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	mr.Close()
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		name:          "test",
		ctx:           context.Background(),
		schemaVersion: SchemaV1,
	}
	err := ac.init()
	if err == nil {
		t.Fatal("expected error from closed Redis in init() v1")
	}
}
