// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

func TestCreateReturnsErrorWhenRedisUnavailable(t *testing.T) {
	mr := createTestRedisServer(t)
	addr := mr.Addr()
	mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:     addr,
		Password: "",
		DB:       0,
		Name:     "test",
		Debug:    false,
	})
	if err == nil {
		t.Fatal("expected create to return an error")
	}
	if ac != nil {
		t.Fatal("expected create to return nil aho-corasick")
	}
}

func TestNewRedisClientSelectsStandaloneTopology(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	client, err := newRedisClient(&AhoCorasickArgs{
		Addr: mr.Addr(),
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	if _, ok := client.(*redis.Client); !ok {
		t.Fatalf("expected standalone redis client, got %T", client)
	}
}

func TestNewRedisClientSelectsSentinelTopology(t *testing.T) {
	client, err := newRedisClient(&AhoCorasickArgs{
		Addrs:      []string{"127.0.0.1:26379"},
		MasterName: "mymaster",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	standaloneClient, ok := client.(*redis.Client)
	if !ok {
		t.Fatalf("expected failover client to use redis.Client, got %T", client)
	}
	if standaloneClient.Options().Addr != "FailoverClient" {
		t.Fatalf("expected sentinel failover client, got addr %q", standaloneClient.Options().Addr)
	}
}

func TestNewRedisClientSelectsClusterTopology(t *testing.T) {
	// Note: This test expects connection to fail since no real cluster is running
	// The test validates that cluster client creation attempts connection validation
	_, err := newRedisClient(&AhoCorasickArgs{
		Addrs: []string{"127.0.0.1:7000"},
	})
	if err == nil {
		t.Fatal("expected connection error for non-existent cluster")
	}
}

func TestNewRedisClientSelectsRingTopology(t *testing.T) {
	client, err := newRedisClient(&AhoCorasickArgs{
		RingAddrs: map[string]string{
			"shard-1": "127.0.0.1:7000",
			"shard-2": "127.0.0.1:7001",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	ringClient, ok := client.(*redis.Ring)
	if !ok {
		t.Fatalf("expected ring redis client, got %T", client)
	}
	if len(ringClient.Options().Addrs) != 2 {
		t.Fatalf("expected ring shard addresses to be preserved, got %v", ringClient.Options().Addrs)
	}
}

func TestNewRedisClientRejectsInvalidTopologyConfigurations(t *testing.T) {
	tests := []struct {
		name string
		args *AhoCorasickArgs
		err  error
	}{
		{
			name: "conflicting standalone and cluster",
			args: &AhoCorasickArgs{
				Addr:  "127.0.0.1:6379",
				Addrs: []string{"127.0.0.1:7000"},
			},
			err: ErrRedisConflictingTopology,
		},
		{
			name: "conflicting cluster and ring",
			args: &AhoCorasickArgs{
				Addrs: []string{"127.0.0.1:7000", "127.0.0.1:7001"},
				RingAddrs: map[string]string{
					"shard-1": "127.0.0.1:7100",
				},
			},
			err: ErrRedisConflictingTopology,
		},
		{
			name: "sentinel requires address",
			args: &AhoCorasickArgs{
				MasterName: "mymaster",
			},
			err: ErrRedisSentinelAddrs,
		},
		{
			name: "cluster does not support db selection",
			args: &AhoCorasickArgs{
				Addrs: []string{"127.0.0.1:7000", "127.0.0.1:7001"},
				DB:    1,
			},
			err: ErrRedisClusterDB,
		},
		{
			name: "ring requires non-empty shard address",
			args: &AhoCorasickArgs{
				RingAddrs: map[string]string{
					"shard-1": "   ",
				},
			},
			err: ErrRedisRingAddrs,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := newRedisClient(tt.args)
			if !errors.Is(err, tt.err) {
				t.Fatalf("expected %v, got %v", tt.err, err)
			}
			if client != nil {
				_ = client.Close()
				t.Fatalf("expected client to be nil, got %T", client)
			}
		})
	}
}

func TestCloseWithNilStorage(t *testing.T) {
	ac := &AhoCorasick{redisClient: nil, storage: nil}
	if err := ac.Close(); err != ErrRedisAlreadyClosed {
		t.Errorf("Close() with nil client error = %v, want ErrRedisAlreadyClosed", err)
	}
}

//nolint:gocyclo,funlen
func TestStorageAdapterMethods(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	storage := newRedisStorage(client)
	ctx := context.Background()

	t.Run("SRem", func(t *testing.T) {
		_ = storage.SAdd(ctx, "set", "a", "b")
		if err := storage.SRem(ctx, "set", "a"); err != nil {
			t.Errorf("SRem() error: %v", err)
		}
	})

	t.Run("SCard", func(t *testing.T) {
		count, err := storage.SCard(ctx, "set")
		if err != nil {
			t.Errorf("SCard() error: %v", err)
		}
		if count != 1 {
			t.Errorf("SCard() = %d, want 1", count)
		}
	})

	t.Run("SIsMember", func(t *testing.T) {
		isMember, err := storage.SIsMember(ctx, "set", "b")
		if err != nil {
			t.Errorf("SIsMember() error: %v", err)
		}
		if !isMember {
			t.Error("SIsMember() = false, want true")
		}
	})

	t.Run("ZAdd", func(t *testing.T) {
		if err := storage.ZAdd(ctx, "zset2", &Z{Score: 1.0, Member: "a"}); err != nil {
			t.Errorf("ZAdd() error: %v", err)
		}
	})

	t.Run("ZRank", func(t *testing.T) {
		rank, err := storage.ZRank(ctx, "zset2", "a")
		if err != nil {
			t.Errorf("ZRank() error: %v", err)
		}
		if rank != 0 {
			t.Errorf("ZRank() = %d, want 0", rank)
		}
	})

	t.Run("ZScore", func(t *testing.T) {
		score, err := storage.ZScore(ctx, "zset2", "a")
		if err != nil {
			t.Errorf("ZScore() error: %v", err)
		}
		if score != 1.0 {
			t.Errorf("ZScore() = %f, want 1.0", score)
		}
	})

	t.Run("ZCard", func(t *testing.T) {
		count, err := storage.ZCard(ctx, "zset2")
		if err != nil {
			t.Errorf("ZCard() error: %v", err)
		}
		if count != 1 {
			t.Errorf("ZCard() = %d, want 1", count)
		}
	})

	t.Run("ZRem", func(t *testing.T) {
		if err := storage.ZRem(ctx, "zset2", "a"); err != nil {
			t.Errorf("ZRem() error: %v", err)
		}
	})

	t.Run("Exists", func(t *testing.T) {
		_ = storage.Set(ctx, "existskey", "value")
		count, err := storage.Exists(ctx, "existskey")
		if err != nil {
			t.Errorf("Exists() error: %v", err)
		}
		if count != 1 {
			t.Errorf("Exists() = %d, want 1", count)
		}
	})

	t.Run("TxPipelined", func(t *testing.T) {
		err := storage.TxPipelined(ctx, func(pipe Pipeliner) error {
			_ = pipe.SAdd(ctx, "txset", "member")
			_ = pipe.HSet(ctx, "txhash", "field", "value")
			_ = pipe.ZAdd(ctx, "txzset", &Z{Score: 1.0, Member: "a"})
			return nil
		})
		if err != nil {
			t.Errorf("TxPipelined() error: %v", err)
		}
	})

	t.Run("Close", func(t *testing.T) {
		if err := storage.Close(); err != nil {
			t.Errorf("Close() error: %v", err)
		}
	})
}

func TestStorageDel(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	storage := newRedisStorage(client)
	ctx := context.Background()

	_ = storage.Set(ctx, "delkey", "value")

	if delErr := storage.Del(ctx, "delkey"); delErr != nil {
		t.Errorf("Del() error: %v", delErr)
	}

	_, err = storage.Get(ctx, "delkey")
	if err == nil {
		t.Error("Get() should fail for deleted key")
	}
}

func TestCreate_WithCacheDisabled(t *testing.T) {
	mr := miniredis.RunT(t)

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-no-cache",
		EnableCache: false,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if ac.cache != nil {
		t.Error("expected cache to be nil when EnableCache=false")
	}
}

func TestCreate_WithCacheEnabled(t *testing.T) {
	mr := miniredis.RunT(t)

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-cache",
		EnableCache: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if ac.cache == nil {
		t.Error("expected cache to be non-nil when EnableCache=true")
	}
}

func TestCreate_V1WithCache_ReturnsError(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-v1-cache",
		SchemaVersion: SchemaV1,
		EnableCache:   true,
	})
	if !errors.Is(err, ErrCacheRequiresV2) {
		t.Fatalf("expected ErrCacheRequiresV2, got %v", err)
	}
	if ac != nil {
		t.Fatal("expected nil AhoCorasick when V1 + EnableCache")
	}
}

func TestCreate_V2WithCache_Succeeds(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "test-v2-cache",
		SchemaVersion: SchemaV2,
		EnableCache:   true,
	})
	if err != nil {
		t.Fatalf("expected no error for V2+cache, got %v", err)
	}
	defer func() { _ = ac.Close() }()

	if ac.cache == nil {
		t.Error("expected cache to be initialized with V2 schema")
	}
}

func TestNormalizeAddrsDeduplicates(t *testing.T) {
	result := normalizeAddrs("  localhost:6379  ", []string{"localhost:6379", "  localhost:6379"})
	if len(result) != 1 {
		t.Fatalf("expected 1 unique addr, got %d: %v", len(result), result)
	}
	if result[0] != "localhost:6379" {
		t.Fatalf("expected localhost:6379, got %s", result[0])
	}
}
