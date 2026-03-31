package health

import (
	"context"
	"testing"

	"google.golang.org/grpc/health/grpc_health_v1"
)

type mockWatchStream struct {
	grpc_health_v1.Health_WatchServer
	responses []*grpc_health_v1.HealthCheckResponse
}

func (m *mockWatchStream) Send(resp *grpc_health_v1.HealthCheckResponse) error {
	m.responses = append(m.responses, resp)
	return nil
}

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

func TestGRPCHealthServerNotServing(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: false, latency: 1})

	server := NewGRPCHealthServer(checker)

	resp, err := server.Check(context.Background(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Errorf("expected NOT_SERVING, got %v", resp.Status)
	}
}

func TestGRPCHealthServerWatch(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: true, latency: 1})

	server := NewGRPCHealthServer(checker)

	mockStream := &mockWatchStream{}
	err := server.Watch(&grpc_health_v1.HealthCheckRequest{}, mockStream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mockStream.responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(mockStream.responses))
	}
	if mockStream.responses[0].Status != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Errorf("expected SERVING, got %v", mockStream.responses[0].Status)
	}
}

func TestGRPCHealthServerWatchNotServing(t *testing.T) {
	checker := NewChecker()
	checker.Register("test", &mockChecker{healthy: false, latency: 1})

	server := NewGRPCHealthServer(checker)

	mockStream := &mockWatchStream{}
	err := server.Watch(&grpc_health_v1.HealthCheckRequest{}, mockStream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(mockStream.responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(mockStream.responses))
	}
	if mockStream.responses[0].Status != grpc_health_v1.HealthCheckResponse_NOT_SERVING {
		t.Errorf("expected NOT_SERVING, got %v", mockStream.responses[0].Status)
	}
}
