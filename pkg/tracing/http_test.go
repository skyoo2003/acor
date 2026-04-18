// SPDX-License-Identifier: Apache-2.0

package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPMiddleware(t *testing.T) {
	cfg := &Config{Enabled: false, ServiceName: "acor"}
	tracer, _ := NewTracer(cfg)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	middleware := HTTPMiddleware(tracer)(handler)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/test", http.NoBody)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}

func TestHTTPMiddlewareErrorStatus(t *testing.T) {
	cfg := &Config{Enabled: false, ServiceName: "acor"}
	tracer, _ := NewTracer(cfg)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	middleware := HTTPMiddleware(tracer)(handler)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/fail", http.NoBody)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, rec.Code)
	}
}
