// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func TestRedisStorageSetNX(t *testing.T) {
	const testValue = "value"

	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	store := NewRedisStorage(client)
	ctx := context.Background()

	t.Run("sets new key successfully", func(t *testing.T) {
		ok, err := store.SetNX(ctx, "test-key", testValue, 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected SetNX to return true for new key")
		}

		got, err := store.Get(ctx, "test-key")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if got != testValue {
			t.Errorf("Get() = %q, want %q", got, testValue)
		}
	})

	t.Run("returns false when key already exists", func(t *testing.T) {
		ok, err := store.SetNX(ctx, "test-key", "value2", 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("expected SetNX to return false for existing key")
		}

		got, err := store.Get(ctx, "test-key")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if got != testValue {
			t.Errorf("Get() = %q, want %q", got, testValue)
		}
	})

	t.Run("succeeds after key expires", func(t *testing.T) {
		mr.FastForward(11 * time.Second)
		ok, err := store.SetNX(ctx, "test-key", "value3", 10*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("expected SetNX to return true after expiration")
		}

		got, err := store.Get(ctx, "test-key")
		if err != nil {
			t.Fatalf("Get() error: %v", err)
		}
		if got != "value3" {
			t.Errorf("Get() = %q, want %q", got, "value3")
		}
	})
}

func TestRedisPipelinerDel(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()

	store := NewRedisStorage(client)
	if err := store.Set(ctx, "key1", "val1"); err != nil {
		t.Fatal(err)
	}
	if err := store.Set(ctx, "key2", "val2"); err != nil {
		t.Fatal(err)
	}

	pipe := store.Pipeline()
	if err := pipe.Del(ctx, "key1", "key2"); err != nil {
		t.Fatalf("pipeliner.Del() error: %v", err)
	}
	if err := pipe.Exec(ctx); err != nil {
		t.Fatalf("pipe.Exec() error: %v", err)
	}

	exists, err := store.Exists(ctx, "key1", "key2")
	if err != nil {
		t.Fatal(err)
	}
	if exists != 0 {
		t.Errorf("expected 0 existing keys, got %d", exists)
	}
}

func TestRedisPipelinerHGetAll(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	if err := store.HSet(ctx, "myhash", "field1", "value1", "field2", "value2"); err != nil {
		t.Fatal(err)
	}

	pipe := store.Pipeline()
	result := pipe.HGetAll(ctx, "myhash")
	if err := pipe.Exec(ctx); err != nil {
		t.Fatalf("pipe.Exec() error: %v", err)
	}

	got := result.Val()
	if len(got) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(got))
	}
	if got["field1"] != "value1" {
		t.Errorf("field1 = %q, want %q", got["field1"], "value1")
	}
	if got["field2"] != "value2" {
		t.Errorf("field2 = %q, want %q", got["field2"], "value2")
	}
}

func TestRedisPipelinerSAddAndHSet(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	pipe := store.Pipeline()
	if err := pipe.SAdd(ctx, "myset", "member1", "member2"); err != nil {
		t.Fatalf("pipeliner.SAdd() error: %v", err)
	}
	if err := pipe.HSet(ctx, "myhash", "f1", "v1"); err != nil {
		t.Fatalf("pipeliner.HSet() error: %v", err)
	}
	if err := pipe.Exec(ctx); err != nil {
		t.Fatalf("pipe.Exec() error: %v", err)
	}

	members, err := store.SMembers(ctx, "myset")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 set members, got %d", len(members))
	}

	hashVals, err := store.HGetAll(ctx, "myhash")
	if err != nil {
		t.Fatal(err)
	}
	if hashVals["f1"] != "v1" {
		t.Errorf("hash f1 = %q, want %q", hashVals["f1"], "v1")
	}
}

func TestRedisPipelinerZAdd(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	pipe := store.Pipeline()
	if err := pipe.ZAdd(ctx, "myzset", &Z{Score: 1.0, Member: "a"}, &Z{Score: 2.0, Member: "b"}); err != nil {
		t.Fatalf("pipeliner.ZAdd() error: %v", err)
	}
	if err := pipe.Exec(ctx); err != nil {
		t.Fatalf("pipe.Exec() error: %v", err)
	}

	members, err := store.ZRange(ctx, "myzset", 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 sorted set members, got %d", len(members))
	}
}

func TestRedisStorageTxPipelined(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	err := store.TxPipelined(ctx, func(pipe Pipeliner) error {
		if err := pipe.SAdd(ctx, "txset", "a", "b"); err != nil {
			return err
		}
		if err := pipe.HSet(ctx, "txhash", "k", "v"); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatalf("TxPipelined() error: %v", err)
	}

	members, err := store.SMembers(ctx, "txset")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members, got %d", len(members))
	}

	hash, err := store.HGetAll(ctx, "txhash")
	if err != nil {
		t.Fatal(err)
	}
	if hash["k"] != "v" {
		t.Errorf("hash k = %q, want %q", hash["k"], "v")
	}
}

func TestRedisStorageExists(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	count, err := store.Exists(ctx, "key1", "key2")
	if err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("expected 0 existing, got %d", count)
	}

	if setErr := store.Set(ctx, "key1", "val"); setErr != nil {
		t.Fatal(setErr)
	}

	count, err = store.Exists(ctx, "key1", "key2")
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("expected 1 existing, got %d", count)
	}
}

func TestRedisStorageSCardAndSIsMember(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	if err := store.SAdd(ctx, "myset", "a", "b", "c"); err != nil {
		t.Fatal(err)
	}

	card, err := store.SCard(ctx, "myset")
	if err != nil {
		t.Fatal(err)
	}
	if card != 3 {
		t.Errorf("expected SCard=3, got %d", card)
	}

	isMember, err := store.SIsMember(ctx, "myset", "a")
	if err != nil {
		t.Fatal(err)
	}
	if !isMember {
		t.Error("expected a to be a member")
	}

	isMember, err = store.SIsMember(ctx, "myset", "z")
	if err != nil {
		t.Fatal(err)
	}
	if isMember {
		t.Error("expected z to not be a member")
	}
}

func TestRedisStorageSRem(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	if err := store.SAdd(ctx, "myset", "a", "b", "c"); err != nil {
		t.Fatal(err)
	}
	if err := store.SRem(ctx, "myset", "b"); err != nil {
		t.Fatal(err)
	}

	members, err := store.SMembers(ctx, "myset")
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Errorf("expected 2 members after SRem, got %d", len(members))
	}
}

func TestRedisStorageZOperations(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	if err := store.ZAdd(ctx, "zset", &Z{Score: 1.0, Member: "a"}, &Z{Score: 2.0, Member: "b"}); err != nil {
		t.Fatal(err)
	}

	rank, err := store.ZRank(ctx, "zset", "a")
	if err != nil {
		t.Fatal(err)
	}
	if rank != 0 {
		t.Errorf("ZRank(a) = %d, want 0", rank)
	}

	score, err := store.ZScore(ctx, "zset", "b")
	if err != nil {
		t.Fatal(err)
	}
	if score != 2.0 {
		t.Errorf("ZScore(b) = %f, want 2.0", score)
	}

	card, err := store.ZCard(ctx, "zset")
	if err != nil {
		t.Fatal(err)
	}
	if card != 2 {
		t.Errorf("ZCard = %d, want 2", card)
	}

	if remErr := store.ZRem(ctx, "zset", "a"); remErr != nil {
		t.Fatal(remErr)
	}

	members, err := store.ZRange(ctx, "zset", 0, -1)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 1 || members[0] != "b" {
		t.Errorf("after ZRem, ZRange = %v, want [b]", members)
	}
}

func TestRedisStoragePublish(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	store := NewRedisStorage(client)

	err := store.Publish(ctx, "test-channel", "hello")
	if err != nil {
		t.Fatalf("Publish() error: %v", err)
	}
}
