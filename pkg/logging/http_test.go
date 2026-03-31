package logging

import (
	"bufio"
	"bytes"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type mockHijackResponseWriter struct {
	http.ResponseWriter
	hijacked bool
}

func (m *mockHijackResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	m.hijacked = true
	return nil, nil, nil
}

type mockFlushResponseWriter struct {
	http.ResponseWriter
	flushed bool
}

func (m *mockFlushResponseWriter) Flush() {
	m.flushed = true
}

func TestResponseWriterHijack(t *testing.T) {
	t.Run("supported", func(t *testing.T) {
		inner := &mockHijackResponseWriter{}
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

func TestResponseWriterFlush(t *testing.T) {
	t.Run("supported", func(t *testing.T) {
		inner := &mockFlushResponseWriter{}
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

func TestHTTPMiddleware(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(buf, "info")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := HTTPMiddleware(logger)(handler)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if !strings.Contains(buf.String(), "request completed") {
		t.Error("expected request log")
	}
}

func TestHTTPMiddlewareServerError(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(buf, "info")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	middleware := HTTPMiddleware(logger)(handler)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/fail", http.NoBody)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	output := buf.String()
	if !strings.Contains(output, "request completed") {
		t.Error("expected request log")
	}
	if !strings.Contains(output, `"error"`) {
		t.Error("expected error level log for 500")
	}
}

func TestHTTPMiddlewareClientError(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(buf, "info")
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
	})

	middleware := HTTPMiddleware(logger)(handler)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/bad", http.NoBody)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	output := buf.String()
	if !strings.Contains(output, "request completed") {
		t.Error("expected request log")
	}
	if !strings.Contains(output, `"warn"`) {
		t.Error("expected warn level log for 400")
	}
}
