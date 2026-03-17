package tracing

import (
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

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()

	middleware.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, rec.Code)
	}
}
