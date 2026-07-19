// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"context"

	grpclog "github.com/grpc-ecosystem/go-grpc-middleware/v2/interceptors/logging"
	"github.com/rs/zerolog"
	"google.golang.org/grpc"
)

// GRPCUnaryInterceptor returns the standard go-grpc-middleware logging
// interceptor writing through the zerolog-backed Logger. It logs one record per
// completed call (FinishCall), matching the previous one-line-per-request
// behaviour rather than the middleware default of start+finish.
func GRPCUnaryInterceptor(logger *Logger) grpc.UnaryServerInterceptor {
	if logger == nil {
		panic("logging: nil logger passed to GRPCUnaryInterceptor")
	}
	return grpclog.UnaryServerInterceptor(
		zerologAdapter(logger),
		grpclog.WithLogOnEvents(grpclog.FinishCall),
	)
}

// zerologAdapter bridges the middleware's Logger contract to zerolog.
func zerologAdapter(logger *Logger) grpclog.Logger {
	return grpclog.LoggerFunc(func(_ context.Context, level grpclog.Level, msg string, fields ...any) {
		var event *zerolog.Event
		switch level {
		case grpclog.LevelError:
			event = logger.Error()
		case grpclog.LevelWarn:
			event = logger.Warn()
		case grpclog.LevelDebug:
			event = logger.Debug()
		default:
			event = logger.Info()
		}

		it := grpclog.Fields(fields).Iterator()
		for it.Next() {
			k, v := it.At()
			event = event.Interface(k, v)
		}
		event.Msg(msg)
	})
}
