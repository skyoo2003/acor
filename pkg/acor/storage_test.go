package acor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
)

func TestRedisStorageImplementsKVStorage(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	storage := newRedisStorage(client)

	var _ KVStorage = storage
}

func TestRedisStorageOperations(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	storage := newRedisStorage(client)
	ctx := context.Background()

	t.Run("Set and Get", func(t *testing.T) {
		err := storage.Set(ctx, "key", "value")
		if err != nil {
			t.Fatalf("Set() error = %v", err)
		}

		got, err := storage.Get(ctx, "key")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got != "value" {
			t.Errorf("Get() = %q, want %q", got, "value")
		}
	})

	t.Run("SAdd and SMembers", func(t *testing.T) {
		err := storage.SAdd(ctx, "set", "member1", "member2")
		if err != nil {
			t.Fatalf("SAdd() error = %v", err)
		}

		members, err := storage.SMembers(ctx, "set")
		if err != nil {
			t.Fatalf("SMembers() error = %v", err)
		}
		if len(members) != 2 {
			t.Errorf("SMembers() returned %d members, want 2", len(members))
		}
	})

	t.Run("ZAdd and ZRange", func(t *testing.T) {
		err := storage.ZAdd(ctx, "zset", &Z{Score: 1.0, Member: "a"}, &Z{Score: 2.0, Member: "b"})
		if err != nil {
			t.Fatalf("ZAdd() error = %v", err)
		}

		members, err := storage.ZRange(ctx, "zset", 0, -1)
		if err != nil {
			t.Fatalf("ZRange() error = %v", err)
		}
		if len(members) != 2 {
			t.Errorf("ZRange() returned %d members, want 2", len(members))
		}
	})

	t.Run("HSet and HGetAll", func(t *testing.T) {
		err := storage.HSet(ctx, "hash", "field1", "value1", "field2", "value2")
		if err != nil {
			t.Fatalf("HSet() error = %v", err)
		}

		values, err := storage.HGetAll(ctx, "hash")
		if err != nil {
			t.Fatalf("HGetAll() error = %v", err)
		}
		if len(values) != 2 {
			t.Errorf("HGetAll() returned %d fields, want 2", len(values))
		}
	})

	t.Run("Del", func(t *testing.T) {
		err := storage.Set(ctx, "delkey", "value")
		if err != nil {
			t.Fatal(err)
		}

		err = storage.Del(ctx, "delkey")
		if err != nil {
			t.Fatalf("Del() error = %v", err)
		}

		_, err = storage.Get(ctx, "delkey")
		if err == nil {
			t.Error("Get() should fail for deleted key")
		}
	})
}
