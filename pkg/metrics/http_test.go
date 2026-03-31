package metrics

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

type metricsMockHijackResponseWriter struct {
	http.ResponseWriter
	hijacked bool
}

func (m *metricsMockHijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	m.hijacked = true
	return nil, nil, nil
}

type metricsMockFlushResponseWriter struct {
	http.ResponseWriter
	flushed bool
}

func (m *metricsMockFlushResponseWriter) Flush() {
	m.flushed = true
}

func TestMetricsResponseWriterHijack(t *testing.T) {
	t.Run("supported", func(t *testing.T) {
		inner := &metricsMockHijackResponseWriter{}
		rw := &responseWriter{ResponseWriter: inner, statusCode: http.StatusOK}
		conn, br, err := rw.Hijack()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if conn != nil || br != nil {
			t.Error("expected nil conn and br from mock")
		}
		if !inner.hijacked {
			t.Error("expected hijacked to be true")
		}
	})

	t.Run("not supported", func(t *testing.T) {
		inner := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: inner, statusCode: http.StatusOK}
		_, _, err := rw.Hijack()
		if err == nil {
			t.Error("expected error when hijack not supported")
		}
	})
}

func TestMetricsResponseWriterFlush(t *testing.T) {
	t.Run("supported", func(t *testing.T) {
		inner := &metricsMockFlushResponseWriter{}
		rw := &responseWriter{ResponseWriter: inner, statusCode: http.StatusOK}
		rw.Flush()
		if !inner.flushed {
			t.Error("expected flushed to be true")
		}
	})

	t.Run("not supported", func(t *testing.T) {
		inner := httptest.NewRecorder()
		rw := &responseWriter{ResponseWriter: inner, statusCode: http.StatusOK}
		rw.Flush()
	})
}

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
