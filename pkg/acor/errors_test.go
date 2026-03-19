package acor

import (
	"errors"
	"testing"
)

func TestOperationError(t *testing.T) {
	tests := []struct {
		name     string
		op       string
		keyword  string
		schema   int
		err      error
		expected string
	}{
		{
			name:     "with keyword",
			op:       "Add",
			keyword:  "test",
			schema:   2,
			err:      errors.New("underlying"),
			expected: `Add("test", schema=2): underlying`,
		},
		{
			name:     "without keyword",
			op:       "Find",
			keyword:  "",
			schema:   1,
			err:      errors.New("redis error"),
			expected: `Find(schema=1): redis error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := &OperationError{
				Op:      tt.op,
				Keyword: tt.keyword,
				Schema:  tt.schema,
				Err:     tt.err,
			}
			if got := e.Error(); got != tt.expected {
				t.Errorf("Error() = %q, want %q", got, tt.expected)
			}
			if !errors.Is(e, tt.err) {
				t.Error("Unwrap() should return underlying error")
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	e := &ValidationError{
		Field:   "keyword",
		Value:   "",
		Message: "cannot be empty",
	}
	expected := "validation error: keyword=, cannot be empty"
	if got := e.Error(); got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
}

func TestRedisError(t *testing.T) {
	underlying := errors.New("connection refused")
	e := &RedisError{
		Op:  "GET",
		Key: "{test}:keyword",
		Err: underlying,
	}
	expected := `redis GET on key "{test}:keyword": connection refused`
	if got := e.Error(); got != expected {
		t.Errorf("Error() = %q, want %q", got, expected)
	}
	if !errors.Is(e, underlying) {
		t.Error("Unwrap() should return underlying error")
	}
}

func TestErrorHelpers(t *testing.T) {
	t.Run("newOperationError", func(t *testing.T) {
		err := newOperationError("Add", 2, errors.New("test"))
		var opErr *OperationError
		if !errors.As(err, &opErr) {
			t.Error("should be OperationError")
		}
	})

	t.Run("newValidationError", func(t *testing.T) {
		err := newValidationError("field", "value", "invalid")
		var valErr *ValidationError
		if !errors.As(err, &valErr) {
			t.Error("should be ValidationError")
		}
	})

	t.Run("newRedisError", func(t *testing.T) {
		err := newRedisError("GET", "key", errors.New("test"))
		var redisErr *RedisError
		if !errors.As(err, &redisErr) {
			t.Error("should be RedisError")
		}
	})
}
