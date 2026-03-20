package acor

//go:generate go run go.uber.org/mock/mockgen -source=interfaces.go -destination=mock_storage.go -package acor

import "context"

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
	// Close closes the storage connection.
	Close() error
}

// Pipeliner defines the interface for pipelined Redis operations.
// Commands are buffered and sent together for efficiency.
type Pipeliner interface {
	// SAdd adds members to a set in the pipeline.
	SAdd(ctx context.Context, key string, members ...interface{}) error
	// HSet sets hash fields in the pipeline.
	HSet(ctx context.Context, key string, values ...interface{}) error
	// ZAdd adds sorted set members in the pipeline.
	ZAdd(ctx context.Context, key string, members ...*Z) error
	// Del deletes keys in the pipeline.
	Del(ctx context.Context, keys ...string) error
}

// Matcher defines the interface for text matching operations.
// Implementations can use different strategies for matching (sequential, parallel).
type Matcher interface {
	// Find returns all keyword matches in the text.
	Find(text string) ([]string, error)
	// FindIndex returns matches with their start positions.
	FindIndex(text string) (map[string][]int, error)
	// FindMany matches keywords across multiple texts.
	FindMany(texts []string) (map[string][]string, error)
	// FindParallel matches keywords using parallel processing.
	FindParallel(text string, opts *ParallelOptions) ([]string, error)
}

// Indexer defines the interface for keyword management operations.
// Implementations can use different storage strategies (single, batch).
type Indexer interface {
	// Add inserts a single keyword.
	Add(keyword string) (int, error)
	// AddMany inserts multiple keywords with batch options.
	AddMany(keywords []string, opts *BatchOptions) (*BatchResult, error)
	// Remove deletes a single keyword.
	Remove(keyword string) (int, error)
	// RemoveMany deletes multiple keywords.
	RemoveMany(keywords []string) (*BatchResult, error)
}
