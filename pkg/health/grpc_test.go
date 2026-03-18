package health

import (
	"context"
	"testing"

	"google.golang.org/grpc/health/grpc_health_v1"
)

func TestGRPCHealthServer(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: true, latency: 1})

	server := NewGRPCHealthServer(checker)

	resp, err := server.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING, got %v", resp.Status)
	}
}
