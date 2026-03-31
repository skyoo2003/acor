package acor

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	redis "github.com/go-redis/redis/v8"
)

func setupV2WithError(t *testing.T) *AhoCorasick {
	t.Helper()

	mr := createTestRedisServer(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	if err := client.HSet(context.Background(), "{test}:trie", map[string]interface{}{
		"keywords": `["he","she"]`,
		"prefixes": `["","h","he","s","sh","she"]`,
		"suffixes": `["","e","eh","s","hs","ehs"]`,
		"version":  "100",
	}).Err(); err != nil {
		t.Fatalf("failed to seed trie data: %v", err)
	}
	if err := client.HSet(context.Background(), "{test}:outputs", map[string]interface{}{
		"he":  `["he"]`,
		"she": `["he","she"]`,
	}).Err(); err != nil {
		t.Fatalf("failed to seed output data: %v", err)
	}

	storage := newRedisStorage(client)
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       storage,
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV2,
	}
	ac.ops = &v2Operations{
		storage: storage,
		client:  client,
		name:    "test",
		ctx:     ac.ctx,
		logger:  log.New(io.Discard, "", 0),
	}

	mr.Close()

	return ac
}

func assertRedisError(t *testing.T, err error, wantOp string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var redisErr *RedisError
	if !errors.As(err, &redisErr) {
		t.Fatalf("expected RedisError, got %T: %v", err, err)
	}
	if redisErr.Op != wantOp {
		t.Errorf("RedisError.Op = %q, want %q", redisErr.Op, wantOp)
	}
	if redisErr.Key == "" {
		t.Error("RedisError.Key should not be empty")
	}
	if redisErr.Err == nil {
		t.Error("RedisError.Err (inner) should not be nil")
	}

	if unwrapped := errors.Unwrap(redisErr); unwrapped == nil {
		t.Error("errors.Unwrap should return non-nil inner error")
	}
}

func TestV2InfoRedisError(t *testing.T) {
	ac := setupV2WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.Info()
	assertRedisError(t, err, "HGETALL")
}

func TestV2SuggestRedisError(t *testing.T) {
	ac := setupV2WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.Suggest("he")
	assertRedisError(t, err, "HGETALL")
}

func TestV2TryAddRedisError(t *testing.T) {
	ac := setupV2WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.tryAddV2(context.Background(), "him")
	assertRedisError(t, err, "HGETALL")
}

func TestV2TryRemoveRedisError(t *testing.T) {
	ac := setupV2WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.tryRemoveV2(context.Background(), "he")
	assertRedisError(t, err, "HGETALL")
}

func TestV2FlushRedisError(t *testing.T) {
	ac := setupV2WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	err := ac.Flush()
	assertRedisError(t, err, "DEL")
}

func TestV2ErrorsAreUnwrappable(t *testing.T) {
	ac := setupV2WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.Info()
	if err == nil {
		t.Fatal("expected error")
	}

	var redisErr *RedisError
	if !errors.As(err, &redisErr) {
		t.Fatal("errors.As must match RedisError")
	}
	if errors.Unwrap(redisErr) == nil {
		t.Error("RedisError inner error must unwrap to non-nil")
	}
}
