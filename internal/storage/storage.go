// SPDX-License-Identifier: Apache-2.0

package storage

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type redisStorage struct {
	client redis.UniversalClient
}

// NewRedisStorage returns a KVStorage backed by the given Redis client.
func NewRedisStorage(client redis.UniversalClient) KVStorage {
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
	zMembers := make([]redis.Z, len(members))
	for i, m := range members {
		zMembers[i] = redis.Z{Score: m.Score, Member: m.Member}
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

func (s *redisStorage) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	return s.client.SetNX(ctx, key, value, expiration).Result()
}

func (s *redisStorage) Pipeline() Pipeliner {
	return &redisPipeliner{pipe: s.client.Pipeline()}
}

func (s *redisStorage) Publish(ctx context.Context, channel string, message interface{}) error {
	return s.client.Publish(ctx, channel, message).Err()
}

func (s *redisStorage) Subscribe(ctx context.Context, channels ...string) Subscription {
	return &redisSubscription{pubsub: s.client.Subscribe(ctx, channels...), done: make(chan struct{})}
}

func (s *redisStorage) Close() error {
	return s.client.Close()
}

type redisStringMapResult struct {
	cmd *redis.MapStringStringCmd
}

func (r *redisStringMapResult) Val() map[string]string {
	return r.cmd.Val()
}

type redisSubscription struct {
	pubsub    *redis.PubSub
	ch        chan PubSubMessage
	once      sync.Once
	done      chan struct{}
	closeOnce sync.Once
}

func (s *redisSubscription) Receive(ctx context.Context) error {
	_, err := s.pubsub.Receive(ctx)
	return err
}

const pubsubChannelSize = 100

func (s *redisSubscription) Channel() <-chan PubSubMessage {
	s.once.Do(func() {
		ch := make(chan PubSubMessage, pubsubChannelSize)
		src := s.pubsub.Channel()
		go func() {
			defer close(ch)
			for {
				select {
				case <-s.done:
					return
				case msg, ok := <-src:
					if !ok {
						return
					}
					select {
					case ch <- PubSubMessage{Channel: msg.Channel, Payload: msg.Payload}:
					case <-s.done:
						return
					}
				}
			}
		}()
		s.ch = ch
	})
	return s.ch
}

func (s *redisSubscription) Close() error {
	err := s.pubsub.Close()
	s.closeOnce.Do(func() {
		if s.done != nil {
			close(s.done)
		}
	})
	return err
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
	zMembers := make([]redis.Z, len(members))
	for i, m := range members {
		zMembers[i] = redis.Z{Score: m.Score, Member: m.Member}
	}
	return p.pipe.ZAdd(ctx, key, zMembers...).Err()
}

func (p *redisPipeliner) Del(ctx context.Context, keys ...string) error {
	return p.pipe.Del(ctx, keys...).Err()
}

func (p *redisPipeliner) HGetAll(ctx context.Context, key string) StringMapResult {
	return &redisStringMapResult{cmd: p.pipe.HGetAll(ctx, key)}
}

func (p *redisPipeliner) Exec(ctx context.Context) error {
	_, err := p.pipe.Exec(ctx)
	return err
}
