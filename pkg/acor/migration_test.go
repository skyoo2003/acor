package acor

import (
	"context"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

func TestMigrateV1ToV2(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	client.SAdd(ctx, "{test}:keyword", "he", "she", "his")
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 0, Member: ""})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "h"})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "he"})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "s"})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "sh"})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "she"})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "hi"})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "his"})

	client.SAdd(ctx, "{test}:output:he", "he")
	client.SAdd(ctx, "{test}:output:she", "he", "she")
	client.SAdd(ctx, "{test}:output:his", "his")

	client.SAdd(ctx, "{test}:node:he", "h", "he")
	client.SAdd(ctx, "{test}:node:she", "s", "sh", "she")
	client.SAdd(ctx, "{test}:node:his", "h", "hi", "his")

	ac := &AhoCorasick{
		redisClient: client,
		ctx:         ctx,
		name:        "test",
	}

	result, err := ac.MigrateV1ToV2(&MigrationOptions{DryRun: true})
	if err != nil {
		t.Fatalf("MigrateV1ToV2(dry-run) error: %v", err)
	}

	if !result.DryRun {
		t.Error("DryRun should be true in result")
	}

	if client.Exists(ctx, "{test}:prefix").Val() == 0 {
		t.Error("V1 prefix key should still exist after dry-run")
	}

	result, err = ac.MigrateV1ToV2(&MigrationOptions{KeepOldKeys: false})
	if err != nil {
		t.Fatalf("MigrateV1ToV2() error: %v", err)
	}

	if result.Status != migrationStatusSuccess {
		t.Errorf("Status = %s, want success", result.Status)
	}
	if result.FromSchema != SchemaV1 {
		t.Errorf("FromSchema = %d, want %d", result.FromSchema, SchemaV1)
	}
	if result.ToSchema != SchemaV2 {
		t.Errorf("ToSchema = %d, want %d", result.ToSchema, SchemaV2)
	}
	if result.Keywords != 3 {
		t.Errorf("Keywords = %d, want 3", result.Keywords)
	}

	if client.Exists(ctx, "{test}:trie").Val() == 0 {
		t.Error("V2 trie key should exist after migration")
	}

	if client.Exists(ctx, "{test}:prefix").Val() != 0 {
		t.Error("V1 prefix key should be deleted after migration")
	}

	trieData := client.HGetAll(ctx, "{test}:trie").Val()
	var keywords []string
	_ = parseJSON(trieData["keywords"], &keywords)
	if len(keywords) != 3 {
		t.Errorf("Migrated keywords count = %d, want 3", len(keywords))
	}
}

func TestMigrateAlreadyV2(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	client.HSet(context.Background(), "{test}:trie", "version", "123")

	ac := &AhoCorasick{
		redisClient: client,
		ctx:         context.Background(),
		name:        "test",
	}

	_, err := ac.MigrateV1ToV2(nil)
	if err == nil {
		t.Error("MigrateV1ToV2 should return error for already V2")
	}
}

func TestMigrateNoData(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	ac := &AhoCorasick{
		redisClient: client,
		ctx:         context.Background(),
		name:        "test",
	}

	_, err := ac.MigrateV1ToV2(nil)
	if err == nil {
		t.Error("MigrateV1ToV2 should return error when no V1 data exists")
	}
}

func TestMigrationProgress(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	client.SAdd(ctx, "{test}:keyword", "he")
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 0, Member: ""})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "h"})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "he"})
	client.SAdd(ctx, "{test}:output:he", "he")
	client.SAdd(ctx, "{test}:node:he", "h", "he")

	ac := &AhoCorasick{
		redisClient: client,
		ctx:         ctx,
		name:        "test",
	}

	var progressCalls []string
	opts := &MigrationOptions{
		DryRun: true,
		Progress: func(step, total int, message string) {
			progressCalls = append(progressCalls, message)
		},
	}

	_, err := ac.MigrateV1ToV2(opts)
	if err != nil {
		t.Fatalf("MigrateV1ToV2 error: %v", err)
	}

	if len(progressCalls) < 3 {
		t.Errorf("Expected at least 3 progress calls, got %d", len(progressCalls))
	}
}

func TestRollbackToV1(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	client.SAdd(ctx, "{test}:keyword", "he")
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 0, Member: ""})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "h"})
	client.ZAdd(ctx, "{test}:prefix", &redis.Z{Score: 1, Member: "he"})
	client.ZAdd(ctx, "{test}:suffix", &redis.Z{Score: 0, Member: ""})
	client.ZAdd(ctx, "{test}:suffix", &redis.Z{Score: 1, Member: "eh"})
	client.ZAdd(ctx, "{test}:suffix", &redis.Z{Score: 1, Member: "h"})

	ac := &AhoCorasick{
		redisClient: client,
		ctx:         ctx,
		name:        "test",
	}

	_, err := ac.MigrateV1ToV2(&MigrationOptions{KeepOldKeys: true})
	if err != nil {
		t.Fatalf("MigrateV1ToV2 error: %v", err)
	}

	if client.Exists(ctx, "{test}:trie").Val() == 0 {
		t.Error("V2 trie key should exist after migration")
	}

	err = ac.RollbackToV1()
	if err != nil {
		t.Fatalf("RollbackToV1 error: %v", err)
	}

	if client.Exists(ctx, "{test}:trie").Val() != 0 {
		t.Error("V2 trie key should be deleted after rollback")
	}

	if ac.schemaVersion != SchemaV1 {
		t.Errorf("schemaVersion = %d, want %d", ac.schemaVersion, SchemaV1)
	}
}

func TestRollbackToV1NoV1Keys(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	ctx := context.Background()
	client.HSet(ctx, "{test}:trie", "version", "123")

	ac := &AhoCorasick{
		redisClient: client,
		ctx:         ctx,
		name:        "test",
	}

	err := ac.RollbackToV1()
	if err == nil {
		t.Error("RollbackToV1 should return error when V1 keys don't exist")
	}
}
