package health

type CheckResult struct {
	Status  string      `json:"status"`
	Latency int64       `json:"latency_ms,omitempty"`
	Details interface{} `json:"details,omitempty"`
}

type Checker interface {
	Check() CheckResult
}

type HealthChecker struct {
	checkers map[string]Checker
}

func NewChecker() *HealthChecker {
	return &HealthChecker{
		checkers: make(map[string]Checker),
	}
}

func (h *HealthChecker) Register(name string, checker Checker) {
	h.checkers[name] = checker
}

type OverallResult struct {
	Status string                 `json:"status"`
	Checks map[string]CheckResult `json:"checks"`
}

func (h *HealthChecker) Check() OverallResult {
	checks := make(map[string]CheckResult)
	overallStatus := "healthy"

	for name, checker := range h.checkers {
		result := checker.Check()
		checks[name] = result
		if result.Status != "healthy" {
			overallStatus = "unhealthy"
		}
	}

	return OverallResult{
		Status: overallStatus,
		Checks: checks,
	}
}
