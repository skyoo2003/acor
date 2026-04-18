// SPDX-License-Identifier: Apache-2.0

package tracing

import (
	"context"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCUnaryInterceptor(t *testing.T) {
	cfg := &Config{Enabled: false, ServiceName: "acor"}
	tracer, err := NewTracer(cfg)
	if err != nil {
		t.Fatalf("unexpected error from NewTracer: %v", err)
	}

	interceptor := GRPCUnaryInterceptor(tracer)

	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return "ok", nil
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/Method",
	}

	_, err = interceptor(context.Background(), nil, info, handler)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGRPCUnaryInterceptorWithError(t *testing.T) {
	cfg := &Config{Enabled: false, ServiceName: "acor"}
	tracer, err := NewTracer(cfg)
	if err != nil {
		t.Fatalf("unexpected error from NewTracer: %v", err)
	}

	interceptor := GRPCUnaryInterceptor(tracer)

	testErr := status.Error(codes.Internal, "something broke")
	handler := func(ctx context.Context, req interface{}) (interface{}, error) {
		return nil, testErr
	}

	info := &grpc.UnaryServerInfo{
		FullMethod: "/test.Service/FailMethod",
	}

	_, err = interceptor(context.Background(), nil, info, handler)
	if err == nil {
		t.Error("expected error from handler")
	}
}
