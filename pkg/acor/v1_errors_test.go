package acor

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	redis "github.com/go-redis/redis/v8"
)

func setupV1WithError(t *testing.T) *AhoCorasick {
	t.Helper()

	mr := createTestRedisServer(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	// Seed some V1 data so operations can start before the server closes
	client.SAdd(context.Background(), keywordKey("test"), "he", "she")
	client.ZAdd(context.Background(), prefixKey("test"), &redis.Z{Score: 0, Member: "h"}, &redis.Z{Score: 1, Member: "s"})
	client.SAdd(context.Background(), nodeKey("test", "he"), "he")
	client.SAdd(context.Background(), outputKey("test", "he"), "he")

	storage := newRedisStorage(client)
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       storage,
		ctx:           context.Background(),
		name:          "test",
		schemaVersion: SchemaV1,
	}
	ac.ops = &v1Operations{
		storage: storage,
		name:    "test",
		ctx:     ac.ctx,
		logger:  log.New(io.Discard, "", 0),
		ac:      ac,
	}

	mr.Close()

	return ac
}

func assertV1RedisError(t *testing.T, err error, wantOp string) {
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

func assertV1OperationError(t *testing.T, err error, wantOp string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatalf("expected OperationError, got %T: %v", err, err)
	}
	if opErr.Op != wantOp {
		t.Errorf("OperationError.Op = %q, want %q", opErr.Op, wantOp)
	}
	if opErr.Schema != SchemaV1 {
		t.Errorf("OperationError.Schema = %d, want %d", opErr.Schema, SchemaV1)
	}
	if opErr.Err == nil {
		t.Error("OperationError.Err (inner) should not be nil")
	}
}

func TestV1AddRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.addV1(context.Background(), "newword")
	assertV1RedisError(t, err, "SISMEMBER")
}

func TestV1RemoveRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.removeV1(context.Background(), "he")
	assertV1RedisError(t, err, "SMEMBERS")
}

func TestV1FlushRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	err := ac.flushV1(context.Background())
	assertV1RedisError(t, err, "SMEMBERS")
}

func TestV1InfoRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.infoV1(context.Background())
	assertV1RedisError(t, err, "SCARD")
}

func TestV1SuggestRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.suggestV1(context.Background(), "h")
	assertV1RedisError(t, err, "ZRANK")
}

func TestV1SuggestIndexRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.suggestIndexV1(context.Background(), "h")
	assertV1RedisError(t, err, "ZRANK")
}

func TestV1FindOperationError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.findV1(context.Background(), "hello")
	assertV1OperationError(t, err, "find")
}

func TestV1FindIndexOperationError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.findIndexV1(context.Background(), "hello")
	assertV1OperationError(t, err, "findIndex")
}

func TestV1ErrorsAreUnwrappable(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	// Test RedisError unwrapping
	_, err := ac.infoV1(context.Background())
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

	// Test OperationError unwrapping
	_, err = ac.findV1(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from findV1")
	}

	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatal("errors.As must match OperationError")
	}
	if errors.Unwrap(opErr) == nil {
		t.Error("OperationError inner error must unwrap to non-nil")
	}
}
