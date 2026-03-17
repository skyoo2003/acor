package logging

import (
	"context"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

func GRPCUnaryInterceptor(logger *Logger) grpc.UnaryServerInterceptor {
	if logger == nil {
		panic("logging: nil logger passed to GRPCUnaryInterceptor")
	}
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		duration := time.Since(start)
		st, _ := status.FromError(err)

		logger.Info().
			Str("method", info.FullMethod).
			Str("status", st.Code().String()).
			Int64("latency_ms", duration.Milliseconds()).
			Msg("request completed")

		return resp, err
	}
}
