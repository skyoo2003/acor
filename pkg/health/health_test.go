package health

import (
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
		Status:  map[bool]string{true: "healthy", false: "unhealthy"}[m.healthy],
		Latency: m.latency,
		Details: nil,
	}
}

func TestChecker(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: true, latency: 1})

	result := checker.Check()

	if result.Status != "healthy" {
		t.Errorf("expected healthy, got %s", result.Status)
	}
}

func TestCheckerUnhealthy(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: false, latency: 1})

	result := checker.Check()

	if result.Status != "unhealthy" {
		t.Errorf("expected unhealthy, got %s", result.Status)
	}
}

func TestHTTPHandlers(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: true, latency: 1})

	mux := http.NewServeMux()
	RegisterHTTPHandlers(mux, checker)

	t.Run("healthz", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/healthz", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}
	})

	t.Run("readyz", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/readyz", nil)
		rec := httptest.NewRecorder()

		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
		}

		var result OverallResult
		if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
			t.Fatalf("failed to parse response: %v", err)
		}

		if result.Status != "healthy" {
			t.Errorf("expected healthy, got %s", result.Status)
		}
	})
}
