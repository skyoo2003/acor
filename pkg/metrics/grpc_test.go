package metrics

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
)

func TestGRPCUnaryInterceptor(t *testing.T) {
	reg := NewRegistry(prometheus.NewRegistry())

	interceptor := GRPCUnaryInterceptor(reg)

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
