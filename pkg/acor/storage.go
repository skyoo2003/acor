package acor

import (
	"context"

	"github.com/go-redis/redis/v8"
)

type redisStorage struct {
	client redis.UniversalClient
}

func newRedisStorage(client redis.UniversalClient) *redisStorage {
	return &redisStorage{client: client}
}

func (s *redisStorage) Get(ctx context.Context, key string) (string, error) {
	return s.client.Get(ctx, key).Result()
}

func (s *redisStorage) Set(ctx context.Context, key string, value interface{}) error {
	return s.client.Set(ctx, key, value, 0).Err()
}

func (s *redisStorage) HGetAll(ctx context.Context, key string) (map[string]string, error) {
	return s.client.HGetAll(ctx, key).Result()
}

func (s *redisStorage) HSet(ctx context.Context, key string, values ...interface{}) error {
	return s.client.HSet(ctx, key, values...).Err()
}

func (s *redisStorage) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return s.client.SAdd(ctx, key, members...).Err()
}

func (s *redisStorage) SMembers(ctx context.Context, key string) ([]string, error) {
	return s.client.SMembers(ctx, key).Result()
}

func (s *redisStorage) SRem(ctx context.Context, key string, members ...interface{}) error {
	return s.client.SRem(ctx, key, members...).Err()
}

func (s *redisStorage) SCard(ctx context.Context, key string) (int64, error) {
	return s.client.SCard(ctx, key).Result()
}

func (s *redisStorage) SIsMember(ctx context.Context, key, member string) (bool, error) {
	return s.client.SIsMember(ctx, key, member).Result()
}

func (s *redisStorage) ZAdd(ctx context.Context, key string, members ...*Z) error {
	zMembers := make([]*redis.Z, len(members))
	for i, m := range members {
		zMembers[i] = &redis.Z{Score: m.Score, Member: m.Member}
	}
	return s.client.ZAdd(ctx, key, zMembers...).Err()
}

func (s *redisStorage) ZRange(ctx context.Context, key string, start, stop int64) ([]string, error) {
	return s.client.ZRange(ctx, key, start, stop).Result()
}

func (s *redisStorage) ZRank(ctx context.Context, key, member string) (int64, error) {
	return s.client.ZRank(ctx, key, member).Result()
}

func (s *redisStorage) ZScore(ctx context.Context, key, member string) (float64, error) {
	return s.client.ZScore(ctx, key, member).Result()
}

func (s *redisStorage) ZCard(ctx context.Context, key string) (int64, error) {
	return s.client.ZCard(ctx, key).Result()
}

func (s *redisStorage) ZRem(ctx context.Context, key string, members ...interface{}) error {
	return s.client.ZRem(ctx, key, members...).Err()
}

func (s *redisStorage) Del(ctx context.Context, keys ...string) error {
	return s.client.Del(ctx, keys...).Err()
}

func (s *redisStorage) Exists(ctx context.Context, keys ...string) (int64, error) {
	return s.client.Exists(ctx, keys...).Result()
}

func (s *redisStorage) TxPipelined(ctx context.Context, fn func(Pipeliner) error) error {
	_, err := s.client.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		return fn(&redisPipeliner{pipe: pipe})
	})
	return err
}

func (s *redisStorage) Close() error {
	return s.client.Close()
}

type redisPipeliner struct {
	pipe redis.Pipeliner
}

func (p *redisPipeliner) SAdd(ctx context.Context, key string, members ...interface{}) error {
	return p.pipe.SAdd(ctx, key, members...).Err()
}

func (p *redisPipeliner) HSet(ctx context.Context, key string, values ...interface{}) error {
	return p.pipe.HSet(ctx, key, values...).Err()
}

func (p *redisPipeliner) ZAdd(ctx context.Context, key string, members ...*Z) error {
	zMembers := make([]*redis.Z, len(members))
	for i, m := range members {
		zMembers[i] = &redis.Z{Score: m.Score, Member: m.Member}
	}
	return p.pipe.ZAdd(ctx, key, zMembers...).Err()
}

func (p *redisPipeliner) Del(ctx context.Context, keys ...string) error {
	return p.pipe.Del(ctx, keys...).Err()
}
