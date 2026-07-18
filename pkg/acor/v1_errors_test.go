// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"io"
	"log"
	"testing"

	redis "github.com/redis/go-redis/v9"
)

func setupV1WithError(t *testing.T) *AhoCorasick {
	t.Helper()

	mr := createTestRedisServer(t)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	// Seed some V1 data so operations can start before the server closes
	if err := client.SAdd(context.Background(), keywordKey("test"), "he", "she").Err(); err != nil {
		t.Fatalf("failed to seed keywords: %v", err)
	}
	if err := client.ZAdd(context.Background(), prefixKey("test"), redis.Z{Score: 0, Member: "h"}, redis.Z{Score: 1, Member: "s"}).Err(); err != nil {
		t.Fatalf("failed to seed prefixes: %v", err)
	}
	if err := client.SAdd(context.Background(), nodeKey("test", "he"), "he").Err(); err != nil {
		t.Fatalf("failed to seed node: %v", err)
	}
	if err := client.SAdd(context.Background(), outputKey("test", "he"), "he").Err(); err != nil {
		t.Fatalf("failed to seed output: %v", err)
	}

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

	_, err := ac.AddContext(context.Background(), "newword")
	assertV1RedisError(t, err, "SISMEMBER")

	// Test public dispatch path (AddContext)
	_, err = ac.AddContext(context.Background(), "anotherword")
	assertV1RedisError(t, err, "SISMEMBER")
}

func TestV1RemoveRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.RemoveContext(context.Background(), "he")
	assertV1RedisError(t, err, "SISMEMBER")

	// Test public dispatch path (RemoveContext)
	_, err = ac.RemoveContext(context.Background(), "he")
	assertV1RedisError(t, err, "SISMEMBER")
}

func TestV1FlushRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	err := ac.FlushContext(context.Background())
	assertV1RedisError(t, err, "SMEMBERS")

	// Test public dispatch path (FlushContext)
	err = ac.FlushContext(context.Background())
	assertV1RedisError(t, err, "SMEMBERS")
}

func TestV1InfoRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.InfoContext(context.Background())
	assertV1RedisError(t, err, "SCARD")

	// Test public dispatch path (InfoContext)
	_, err = ac.InfoContext(context.Background())
	assertV1RedisError(t, err, "SCARD")
}

func TestV1SuggestRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.SuggestContext(context.Background(), "h")
	assertV1RedisError(t, err, "ZRANK")

	// Test public dispatch path (SuggestContext)
	_, err = ac.SuggestContext(context.Background(), "h")
	assertV1RedisError(t, err, "ZRANK")
}

func TestV1SuggestIndexRedisError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.SuggestIndexContext(context.Background(), "h")
	assertV1RedisError(t, err, "ZRANK")

	// Test public dispatch path (SuggestIndexContext)
	_, err = ac.SuggestIndexContext(context.Background(), "h")
	assertV1RedisError(t, err, "ZRANK")
}

func TestV1FindOperationError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.FindContext(context.Background(), "hello")
	assertV1OperationError(t, err, "find")

	// Test public dispatch path (FindContext)
	_, err = ac.FindContext(context.Background(), "hello")
	assertV1OperationError(t, err, "find")
}

func TestV1FindIndexOperationError(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	_, err := ac.FindIndexContext(context.Background(), "hello")
	assertV1OperationError(t, err, "findIndex")

	// Test public dispatch path (FindIndexContext)
	_, err = ac.FindIndexContext(context.Background(), "hello")
	assertV1OperationError(t, err, "findIndex")
}

func TestV1ErrorsAreUnwrappable(t *testing.T) {
	ac := setupV1WithError(t)
	defer func() { _ = ac.redisClient.Close() }()

	// Test RedisError unwrapping
	_, err := ac.InfoContext(context.Background())
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
	_, err = ac.FindContext(context.Background(), "test")
	if err == nil {
		t.Fatal("expected error from FindContext")
	}

	var opErr *OperationError
	if !errors.As(err, &opErr) {
		t.Fatal("errors.As must match OperationError")
	}
	if errors.Unwrap(opErr) == nil {
		t.Error("OperationError inner error must unwrap to non-nil")
	}
}
