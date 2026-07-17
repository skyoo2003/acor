// SPDX-License-Identifier: Apache-2.0

package health

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type mockChecker struct {
	healthy bool
	latency int64
}

func (m *mockChecker) Check() CheckResult {
	return CheckResult{
		Status:  map[bool]string{true: StatusHealthy, false: StatusUnhealthy}[m.healthy],
		Latency: m.latency,
		Details: nil,
	}
}

func TestChecker(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: true, latency: 1})

	result := checker.Check()

	if result.Status != StatusHealthy {
		t.Errorf("expected healthy, got %s", result.Status)
	}
}

func TestCheckerUnhealthy(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: false, latency: 1})

	result := checker.Check()

	if result.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy, got %s", result.Status)
	}
}

func TestHTTPHandlers(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: true, latency: 1})

	mux := http.NewServeMux()
	RegisterHTTPHandlers(mux, checker)

	t.Run("healthz", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), "GET", "/healthz", http.NoBody)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("readyz", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), "GET", "/readyz", http.NoBody)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		var result OverallResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if result.Status != StatusHealthy {
			t.Errorf("expected healthy, got %s", result.Status)
		}
	})
}

func TestHTTPHandlersWrongMethod(t *testing.T) {
	checker := NewChecker()
	mux := http.NewServeMux()
	RegisterHTTPHandlers(mux, checker)

	t.Run("healthz wrong method", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), "POST", "/healthz", http.NoBody)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}
	})

	t.Run("readyz wrong method", func(t *testing.T) {
		req := httptest.NewRequestWithContext(context.Background(), "POST", "/readyz", http.NoBody)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected %d, got %d", http.StatusMethodNotAllowed, rec.Code)
		}
	})
}

func TestHTTPHandlersReadyzUnhealthy(t *testing.T) {
	checker := NewChecker()
	checker.Register("db", &mockChecker{healthy: false, latency: 1})
	mux := http.NewServeMux()
	RegisterHTTPHandlers(mux, checker)

	req := httptest.NewRequestWithContext(context.Background(), "GET", "/readyz", http.NoBody)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected %d, got %d", http.StatusServiceUnavailable, rec.Code)
	}
}

func TestCheckerEmpty(t *testing.T) {
	checker := NewChecker()
	result := checker.Check()
	if result.Status != StatusHealthy {
		t.Errorf("empty checker should be healthy, got %s", result.Status)
	}
}

func TestCheckerMultiple(t *testing.T) {
	checker := NewChecker()
	checker.Register("healthy", &mockChecker{healthy: true, latency: 1})
	checker.Register("unhealthy", &mockChecker{healthy: false, latency: 2})
	result := checker.Check()
	if result.Status != StatusUnhealthy {
		t.Errorf("expected unhealthy when any check fails, got %s", result.Status)
	}
	if len(result.Checks) != 2 {
		t.Errorf("expected 2 checks, got %d", len(result.Checks))
	}
}
