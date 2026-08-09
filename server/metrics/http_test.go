// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

// The Hijack and Flush forwarding these tests used to cover now lives in one
// place, and is tested there: server/internal/httpx/responsewriter_test.go.

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "/"},
		{"/", "/"},
		{"/users", "/users"},
		{"/users/123", "/users/{id}"},
		{"/users/123/posts", "/users/{id}/posts"},
		{"/users/550e8400-e29b-41d4-a716-446655440000", "/users/{uuid}"},
		{"/api/v1/users/550e8400-e29b-41d4-a716-446655440000/posts/42", "/api/v1/users/{uuid}/posts/{id}"},
		{"/static/file.txt", "/static/file.txt"},
		{"users/123", "/"},             // relative paths collapse to a single series
		{"/users/123/", "/users/{id}"}, // trailing slash dropped
		{"/users//123", "/users/{id}"}, // empty segments dropped
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizePath(tt.input)
			if result != tt.expected {
				t.Errorf("normalizePath(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHTTPMiddleware(t *testing.T) {
	reg := NewRegistry(prometheus.NewRegistry())
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	middleware := HTTPMiddleware(reg)(handler)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}
