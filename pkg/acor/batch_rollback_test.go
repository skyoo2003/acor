package acor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

func TestRollbackRemovedWithLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	log := &testLogger{}
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		logger:        log,
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
			cache:   &trieCache{},
			logger:  log,
		},
	}

	mr.Close()

	ac.Debug()
}

func TestRollbackAddedWithLogger(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	log := &testLogger{}
	ac := &AhoCorasick{
		redisClient:   client,
		storage:       newRedisStorage(client),
		ctx:           context.Background(),
		name:          "test",
		logger:        log,
		schemaVersion: SchemaV2,
		ops: &v2Operations{
			storage: newRedisStorage(client),
			client:  client,
			name:    "test",
			cache:   &trieCache{},
			logger:  log,
		},
	}

	mr.Close()

	ac.rollbackAdded(context.Background(), []string{"keyword1"})
}

func TestRollbackAddedEmpty(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	ac.rollbackAdded(context.Background(), []string{})
}
