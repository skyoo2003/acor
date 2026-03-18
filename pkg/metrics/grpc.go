package metrics

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func GRPCUnaryInterceptor(reg *Registry) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()
		method := info.FullMethod

		resp, err := handler(ctx, req)

		duration := time.Since(start).Seconds()
		st, _ := status.FromError(err)

		reg.GRPCRequestsTotal.WithLabelValues(method, st.Code().String()).Inc()
		reg.GRPCRequestDuration.WithLabelValues(method).Observe(duration)

		return resp, err
	}
}
