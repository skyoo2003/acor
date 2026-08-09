// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Hijack and Flush forwarding these tests used to cover now lives in one
// place, and is tested there: server/internal/httpx/responsewriter_test.go.

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
