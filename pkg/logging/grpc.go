package logging

import (
	"context"
	"time"

	"github.com/rs/zerolog"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
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

		var event *zerolog.Event
		if st.Code() != codes.OK {
			event = logger.Error()
		} else {
			event = logger.Info()
		}

		event.
			Str("method", info.FullMethod).
			Str("status", st.Code().String()).
			Int64("latency_ms", duration.Milliseconds()).
			Msg("request completed")

		return resp, err
	}
}
