package tracing

import (
	"context"
	"testing"

	"google.golang.org/grpc"
)

func TestGRPCUnaryInterceptor(t *testing.T) {
	cfg := &Config{Enabled: false, ServiceName: "acor"}
	tracer, _ := NewTracer(cfg)

	interceptor := GRPCUnaryInterceptor(tracer)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/Method",
	}

	_, err := interceptor(context.Background(), nil, info, handler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}
