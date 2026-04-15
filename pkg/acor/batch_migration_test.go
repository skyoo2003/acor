package acor

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

const testMigrationLockKey = "{test}:migration_lock"

func TestMigrationConstants(t *testing.T) {
	if migrationStatusError != "error" {
		t.Errorf("migrationStatusError = %q", migrationStatusError)
	}
	if migrationStatusSuccess != "success" {
		t.Errorf("migrationStatusSuccess = %q", migrationStatusSuccess)
	}
	if migrationStatusDryRun != "dry-run" {
		t.Errorf("migrationStatusDryRun = %q", migrationStatusDryRun)
	}
}

func TestMigrationLockKey(t *testing.T) {
	ac := &AhoCorasick{name: "test"}
	key := ac.migrationLockKey()
	if key != testMigrationLockKey {
		t.Errorf("migrationLockKey() = %q, want %q", key, testMigrationLockKey)
	}
}

func TestMigrationLockAcquireRelease(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "lock-test",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	acquired, err := ac.acquireMigrationLock()
	if err != nil {
		t.Fatalf("acquireMigrationLock() error: %v", err)
	}
	if !acquired {
		t.Error("expected to acquire lock")
	}

	if err := ac.releaseMigrationLock(); err != nil {
		t.Fatalf("releaseMigrationLock() error: %v", err)
	}
}

func TestMigrationLockAlreadyHeld(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "lock-test2",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	acquired, err := ac.acquireMigrationLock()
	if err != nil {
		t.Fatal(err)
	}
	if !acquired {
		t.Fatal("first acquire should succeed")
	}

	acquired2, err := ac.acquireMigrationLock()
	if err != nil {
		t.Fatal(err)
	}
	if acquired2 {
		t.Error("second acquire should fail (lock already held)")
	}

	_ = ac.releaseMigrationLock()
}

func TestMigrationInProgress(t *testing.T) {
	s := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: s.Addr()})
	defer func() { _ = client.Close() }()

	// Seed V1 data so migration doesn't fail for other reasons
	client.SAdd(context.Background(), "{test}:keyword", "he")
	client.ZAdd(context.Background(), "{test}:prefix", &redis.Z{Score: 0, Member: ""})

	// Acquire the migration lock with a separate client to simulate another process
	lockKey := "{test}:migration_lock"
	acquired, err := client.SetNX(context.Background(), lockKey, "migrating", 300*time.Second).Result()
	if err != nil {
		t.Fatalf("failed to pre-acquire lock: %v", err)
	}
	if !acquired {
		t.Fatal("expected to acquire lock on first attempt")
	}

	ac := &AhoCorasick{
		redisClient: client,
		ctx:         context.Background(),
		name:        "test",
	}

	_, err = ac.MigrateV1ToV2(nil)
	if err == nil {
		t.Fatal("expected error when migration lock is already held")
	}
	if err.Error() != "migration already in progress" {
		t.Errorf("unexpected error: %v", err)
	}
}
