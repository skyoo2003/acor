package health

import (
	"context"

	"google.golang.org/grpc/health/grpc_health_v1"
)

type GRPCHealthServer struct {
	grpc_health_v1.UnimplementedHealthServer
	checker *HealthChecker
}

func NewGRPCHealthServer(checker *HealthChecker) *GRPCHealthServer {
	return &GRPCHealthServer{checker: checker}
}

func (s *GRPCHealthServer) Check(ctx context.Context, req *grpc_health_v1.HealthCheckRequest) (*grpc_health_v1.HealthCheckResponse, error) {
	result := s.checker.Check()

	status := grpc_health_v1.HealthCheckResponse_SERVING
	if result.Status != StatusHealthy {
		status = grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}

	return &grpc_health_v1.HealthCheckResponse{
		Status: status,
	}, nil
}

func (s *GRPCHealthServer) Watch(req *grpc_health_v1.HealthCheckRequest, stream grpc_health_v1.Health_WatchServer) error {
	result := s.checker.Check()
	status := grpc_health_v1.HealthCheckResponse_SERVING
	if result.Status != StatusHealthy {
		status = grpc_health_v1.HealthCheckResponse_NOT_SERVING
	}
	return stream.Send(&grpc_health_v1.HealthCheckResponse{
		Status: status,
	})
}
