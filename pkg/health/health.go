package health

import "sync"

const (
	StatusHealthy   = "healthy"
	StatusUnhealthy = "unhealthy"
)

type CheckResult struct {
	Status  string      `json:"status"`
	Latency int64       `json:"latency_ms,omitempty"`
	Details interface{} `json:"details,omitempty"`
}

type Checker interface {
	Check() CheckResult
}

type HealthChecker struct {
	mu       sync.RWMutex
	checkers map[string]Checker
}

func NewChecker() *HealthChecker {
	return &HealthChecker{
		checkers: make(map[string]Checker),
	}
}

func (h *HealthChecker) Register(name string, checker Checker) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checkers[name] = checker
}

type OverallResult struct {
	Status string                 `json:"status"`
	Checks map[string]CheckResult `json:"checks"`
}

func (h *HealthChecker) Check() OverallResult {
	h.mu.RLock()
	snapshot := make(map[string]Checker, len(h.checkers))
	for name, checker := range h.checkers {
		snapshot[name] = checker
	}
	h.mu.RUnlock()

	checks := make(map[string]CheckResult, len(snapshot))
	overallStatus := StatusHealthy

	for name, checker := range snapshot {
		result := checker.Check()
		checks[name] = result
		if result.Status != StatusHealthy {
			overallStatus = StatusUnhealthy
		}
	}

	return OverallResult{
		Status: overallStatus,
		Checks: checks,
	}
}
