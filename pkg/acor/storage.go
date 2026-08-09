// SPDX-License-Identifier: Apache-2.0

package acor

import "context"

// zMember represents a sorted set member with score, compatible with Redis ZSET operations.
type zMember struct {
	// Score is the numeric score for ordering in the sorted set.
	Score float64
	// Member is the string value stored in the sorted set.
	Member string
}

// kvStorage defines the interface for key-value storage operations. It lets the
// Aho-Corasick automaton work against a storage abstraction rather than a Redis
// client directly.
//
// It is unexported deliberately. It was public through v1.4.0 and was withdrawn
// before v1.5.0 froze the surface, because nothing on that surface ever accepted
// or returned one: a caller could name the interface but never supply an
// implementation. Freezing it would also have capped the pluggable-backend work it
// was exported for — compatibility.md forbids adding a method to an exported
// interface inside v1, so a backend needing one more operation would have had
// nowhere to put it. Unexported, the shape stays free until that feature can
// choose it, and publishing an interface later is an addition v1 allows.
//
// All operations accept a context for cancellation and timeout support.
type kvStorage interface {
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
	ZAdd(ctx context.Context, key string, members ...*zMember) error
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
	TxPipelined(ctx context.Context, fn func(pipeliner) error) error
	// Pipeline returns a non-transactional pipeline for batching commands.
	Pipeline() pipeliner
	// Publish sends a message to a pub/sub channel.
	Publish(ctx context.Context, channel string, message interface{}) error
	// Subscribe subscribes to pub/sub channels and returns a subscription.
	Subscribe(ctx context.Context, channels ...string) subscription
	// Close closes the storage connection.
	Close() error
}

// stringMapResult represents a deferred string map result from a pipeline
// HGetAll operation. An interface rather than a concrete type so a pipeliner
// implementation can return its own deferred result; see kvStorage on why none
// but the Redis one exists today.
type stringMapResult interface {
	// Val returns the map result. Must be called after Exec on the pipeline.
	Val() map[string]string
}

// pubSubMessage represents a message received from a pub/sub subscription.
type pubSubMessage struct {
	// Channel is the name of the pub/sub channel the message was published to.
	Channel string
	// Payload is the content of the message.
	Payload string
}

// subscription defines the interface for a pub/sub subscription.
type subscription interface {
	// Receive waits for a subscription confirmation from the server.
	Receive(ctx context.Context) error
	// Channel returns a channel that delivers incoming messages.
	Channel() <-chan pubSubMessage
	// Close closes the subscription and releases resources.
	Close() error
}

// pipeliner defines the interface for pipelined Redis operations.
// Commands are buffered and sent together for efficiency.
type pipeliner interface {
	// SAdd adds members to a set in the pipeline.
	SAdd(ctx context.Context, key string, members ...interface{}) error
	// HSet sets hash fields in the pipeline.
	HSet(ctx context.Context, key string, values ...interface{}) error
	// HGetAll retrieves all field-value pairs from a hash in the pipeline.
	// Returns a deferred result that can be read after Exec is called.
	HGetAll(ctx context.Context, key string) stringMapResult
	// ZAdd adds sorted set members in the pipeline.
	ZAdd(ctx context.Context, key string, members ...*zMember) error
	// Del deletes keys in the pipeline.
	Del(ctx context.Context, keys ...string) error
	// Exec executes all commands in the pipeline.
	Exec(ctx context.Context) error
}
