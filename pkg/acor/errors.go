// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"errors"
	"fmt"
)

var (
	// ErrEmptyKeyword is returned when an empty or whitespace-only keyword is provided.
	ErrEmptyKeyword = errors.New("keyword cannot be empty")
	// ErrInvalidChunkSize is returned when ParallelOptions.ChunkSize is <= 0.
	ErrInvalidChunkSize = errors.New("chunk size must be positive")
	// ErrCacheRequiresV2 is returned when cache is enabled with V1 schema.
	// Cache functionality requires V2 schema for Pub/Sub invalidation support.
	ErrCacheRequiresV2 = errors.New("local cache requires V2 schema")
	// ErrConcurrencyConflict is returned when an optimistic locking conflict
	// occurs during a V2 write operation (Add/Remove). The caller should retry.
	ErrConcurrencyConflict = errors.New("concurrency conflict - please retry")
	// ErrPresetRequiresRedis is returned when a Preset is specified without
	// any Redis address.
	ErrPresetRequiresRedis = errors.New("Preset requires a Redis address")
	// ErrPresetRequiresV2 is returned when a Preset is set with SchemaVersion=1.
	ErrPresetRequiresV2 = errors.New("Preset engine requires V2 schema")
	// ErrSuggestRequiresRedis is returned when Suggest/SuggestIndex is called in
	// preset mode, which doesn't support prefix-based suggestions.
	ErrSuggestRequiresRedis = errors.New("suggest requires Redis-backed mode without Preset")
	// ErrMigrationRequiresRedis is returned when MigrateV1ToV2 or RollbackToV1 is
	// called in preset mode. Migration walks the collection's V1 keys directly,
	// which preset mode never opens: it always speaks V2 and serves reads from its
	// local engine. Migrate with an instance created without a Preset.
	ErrMigrationRequiresRedis = errors.New("schema migration requires Redis-backed mode without Preset")
	// ErrNilArgs is returned when Create or CreateContext is called with nil args.
	// Name is required, so there is no meaningful all-defaults configuration.
	ErrNilArgs = errors.New("args must not be nil")
	// ErrCacheWithPreset is returned when EnableCache is combined with a Preset.
	// Preset mode already answers reads from a local engine kept fresh by the same
	// Pub/Sub invalidation, so the trie cache would be a redundant second copy.
	ErrCacheWithPreset = errors.New("EnableCache cannot be combined with Preset")
)

// OperationError represents an error that occurred during an automaton operation.
// It includes context about the operation, keyword, schema version, and underlying error.
type OperationError struct {
	// Op is the name of the operation that failed (e.g., "add", "remove", "find").
	Op string
	// Keyword is the keyword being processed, if applicable.
	Keyword string
	// Schema is the schema version in use when the error occurred.
	Schema int
	// Err is the underlying error that caused this operation error.
	Err error
}

// Error returns a formatted error message including operation context.
func (e *OperationError) Error() string {
	if e.Keyword != "" {
		return fmt.Sprintf("%s(%q, schema=%d): %v", e.Op, e.Keyword, e.Schema, e.Err)
	}
	return fmt.Sprintf("%s(schema=%d): %v", e.Op, e.Schema, e.Err)
}

// Unwrap returns the underlying error for use with errors.Is and errors.As.
func (e *OperationError) Unwrap() error { return e.Err }

// RedisError represents an error that occurred during a Redis operation.
// It includes the operation type, key, and underlying error.
type RedisError struct {
	// Op is the Redis operation that failed (e.g., "HGET", "SADD").
	Op string
	// Key is the Redis key involved in the failed operation.
	Key string
	// Err is the underlying error from the Redis client.
	Err error
}

// Error returns a formatted Redis error message.
func (e *RedisError) Error() string {
	return fmt.Sprintf("redis %s on key %q: %v", e.Op, e.Key, e.Err)
}

// Unwrap returns the underlying error for use with errors.Is and errors.As.
func (e *RedisError) Unwrap() error { return e.Err }

func newOperationError(op string, schema int, err error) error {
	return &OperationError{Op: op, Schema: schema, Err: err}
}

func newRedisError(op, key string, err error) error {
	return &RedisError{Op: op, Key: key, Err: err}
}
