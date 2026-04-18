// SPDX-License-Identifier: Apache-2.0

package acor

//go:generate go run go.uber.org/mock/mockgen -source=interfaces.go -destination=mock/mock_storage.go -package mock

import (
	"context"
	"time"
)

// Z represents a sorted set member with score, compatible with Redis ZSET operations.
type Z struct {
	// Score is the numeric score for ordering in the sorted set.
	Score float64
	// Member is the string value stored in the sorted set.
	Member string
}

// KVStorage defines the interface for key-value storage operations.
// This abstraction allows the Aho-Corasick automaton to work with different
// storage backends. The primary implementation uses Redis, but mock
// implementations can be used for testing.
//
// All operations accept a context for cancellation and timeout support.
type KVStorage interface {
	// Get retrieves a string value by key. Returns redis.Nil error if key doesn't exist.
	Get(ctx context.Context, key string) (string, error)
	// Set stores a value at the given key with no expiration.
	Set(ctx context.Context, key string, value interface{}) error
	// HGetAll retrieves all field-value pairs from a hash.
	HGetAll(ctx context.Context, key string) (map[string]string, error)
	// HSet sets multiple field-value pairs in a hash.
	HSet(ctx context.Context, key string, values ...interface{}) error
	// SAdd adds members to a set.
	SAdd(ctx context.Context, key string, members ...interface{}) error
	// SMembers retrieves all members of a set.
	SMembers(ctx context.Context, key string) ([]string, error)
	// SRem removes members from a set.
	SRem(ctx context.Context, key string, members ...interface{}) error
	// SCard returns the number of members in a set.
	SCard(ctx context.Context, key string) (int64, error)
	// SIsMember checks if a member exists in a set.
	SIsMember(ctx context.Context, key, member string) (bool, error)
	// ZAdd adds members with scores to a sorted set.
	ZAdd(ctx context.Context, key string, members ...*Z) error
	// ZRange returns members in a sorted set by index range.
	ZRange(ctx context.Context, key string, start, stop int64) ([]string, error)
	// ZRank returns the index of a member in a sorted set.
	ZRank(ctx context.Context, key, member string) (int64, error)
	// ZScore returns the score of a member in a sorted set.
	ZScore(ctx context.Context, key, member string) (float64, error)
	// ZCard returns the number of members in a sorted set.
	ZCard(ctx context.Context, key string) (int64, error)
	// ZRem removes members from a sorted set.
	ZRem(ctx context.Context, key string, members ...interface{}) error
	// Del deletes one or more keys.
	Del(ctx context.Context, keys ...string) error
	// Exists checks if one or more keys exist. Returns the count of existing keys.
	Exists(ctx context.Context, keys ...string) (int64, error)
	// TxPipelined executes commands in a transaction pipeline.
	TxPipelined(ctx context.Context, fn func(Pipeliner) error) error
	// SetNX sets a value only if the key does not exist. Returns true if the key was set.
	SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error)
	// Pipeline returns a non-transactional pipeline for batching commands.
	Pipeline() Pipeliner
	// Publish sends a message to a pub/sub channel.
	Publish(ctx context.Context, channel string, message interface{}) error
	// Subscribe subscribes to pub/sub channels and returns a Subscription.
	Subscribe(ctx context.Context, channels ...string) Subscription
	// Close closes the storage connection.
	Close() error
}

// StringMapResult represents a deferred string map result from a pipeline HGetAll operation.
type StringMapResult interface {
	// Val returns the map result. Must be called after Exec on the pipeline.
	Val() map[string]string
}

// PubSubMessage represents a message received from a pub/sub subscription.
type PubSubMessage struct {
	// Channel is the name of the pub/sub channel the message was published to.
	Channel string
	// Payload is the content of the message.
	Payload string
}

// Subscription defines the interface for a pub/sub subscription.
type Subscription interface {
	// Receive waits for a subscription confirmation from the server.
	Receive(ctx context.Context) error
	// Channel returns a channel that delivers incoming messages.
	Channel() <-chan PubSubMessage
	// Close closes the subscription and releases resources.
	Close() error
}

// Pipeliner defines the interface for pipelined Redis operations.
// Commands are buffered and sent together for efficiency.
type Pipeliner interface {
	// SAdd adds members to a set in the pipeline.
	SAdd(ctx context.Context, key string, members ...interface{}) error
	// HSet sets hash fields in the pipeline.
	HSet(ctx context.Context, key string, values ...interface{}) error
	// HGetAll retrieves all field-value pairs from a hash in the pipeline.
	// Returns a deferred result that can be read after Exec is called.
	HGetAll(ctx context.Context, key string) StringMapResult
	// ZAdd adds sorted set members in the pipeline.
	ZAdd(ctx context.Context, key string, members ...*Z) error
	// Del deletes keys in the pipeline.
	Del(ctx context.Context, keys ...string) error
	// Exec executes all commands in the pipeline.
	Exec(ctx context.Context) error
}
