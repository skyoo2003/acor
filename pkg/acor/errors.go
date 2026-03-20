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
	// ErrInvalidWorkerCount is returned when ParallelOptions.Workers is negative.
	// Zero is valid and defaults to runtime.NumCPU().
	// Note: Currently, negative values are automatically normalized to the default
	// rather than returning this error. This error is defined for future explicit
	// validation if needed.
	ErrInvalidWorkerCount = errors.New("worker count cannot be negative")
	// ErrNoBoundariesFound is returned when parallel processing cannot find
	// suitable chunk boundaries in the text.
	ErrNoBoundariesFound = errors.New("could not find suitable chunk boundaries")
	// ErrStreamInterrupted is returned when stream processing is interrupted
	// before completion.
	ErrStreamInterrupted = errors.New("stream processing was interrupted")
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

// ValidationError represents an error caused by invalid input.
// It includes the field name, invalid value, and a descriptive message.
type ValidationError struct {
	// Field is the name of the field that failed validation.
	Field string
	// Value is the invalid value that was provided.
	Value any
	// Message describes why the validation failed.
	Message string
}

// Error returns a formatted validation error message.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s=%v, %s", e.Field, e.Value, e.Message)
}

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

func newValidationError(field string, value any, msg string) error {
	return &ValidationError{Field: field, Value: value, Message: msg}
}

func newRedisError(op, key string, err error) error {
	return &RedisError{Op: op, Key: key, Err: err}
}
