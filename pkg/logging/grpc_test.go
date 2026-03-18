package logging

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc"
)

func TestGRPCUnaryInterceptor(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(buf, "info")

	interceptor := GRPCUnaryInterceptor(logger)

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

	if !strings.Contains(buf.String(), "request completed") {
		t.Error("expected request log")
	}
}
