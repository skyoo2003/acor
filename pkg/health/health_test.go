package health

import (
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
