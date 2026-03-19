package acor

import (
	"errors"
	"fmt"
)

var (
	// ErrEmptyKeyword is returned when an empty keyword is provided.
	ErrEmptyKeyword = errors.New("keyword cannot be empty")
	// ErrInvalidChunkSize is returned when an invalid chunk size is provided.
	ErrInvalidChunkSize = errors.New("chunk size must be positive")
	// ErrInvalidWorkerCount is returned when an invalid worker count is provided.
	ErrInvalidWorkerCount = errors.New("worker count must be positive")
	// ErrNoBoundariesFound is returned when no suitable chunk boundaries are found.
	ErrNoBoundariesFound = errors.New("could not find suitable chunk boundaries")
	// ErrStreamInterrupted is returned when stream processing is interrupted.
	ErrStreamInterrupted = errors.New("stream processing was interrupted")
)

type OperationError struct {
	Op      string
	Keyword string
	Schema  int
	Err     error
}

func (e *OperationError) Error() string {
	if e.Keyword != "" {
		return fmt.Sprintf("%s(%q, schema=%d): %v", e.Op, e.Keyword, e.Schema, e.Err)
	}
	return fmt.Sprintf("%s(schema=%d): %v", e.Op, e.Schema, e.Err)
}

func (e *OperationError) Unwrap() error { return e.Err }

type ValidationError struct {
	Field   string
	Value   interface{}
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s=%v, %s", e.Field, e.Value, e.Message)
}

type RedisError struct {
	Op  string
	Key string
	Err error
}

func (e *RedisError) Error() string {
	return fmt.Sprintf("redis %s on key %q: %v", e.Op, e.Key, e.Err)
}

func (e *RedisError) Unwrap() error { return e.Err }

func newOperationError(op string, schema int, err error) error {
	return &OperationError{Op: op, Schema: schema, Err: err}
}

func newOperationErrorWithKeyword(op, keyword string, schema int, err error) error {
	return &OperationError{Op: op, Keyword: keyword, Schema: schema, Err: err}
}

func newValidationError(field string, value interface{}, msg string) error {
	return &ValidationError{Field: field, Value: value, Message: msg}
}

func newRedisError(op, key string, err error) error {
	return &RedisError{Op: op, Key: key, Err: err}
}
