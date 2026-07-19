// SPDX-License-Identifier: Apache-2.0

package tracing

import (
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc/stats"
)

// GRPCStatsHandler returns the standard OpenTelemetry gRPC server instrumentation
// bound to the given Tracer's provider, so the Tracer that a caller passes
// actually controls gRPC tracing. A nil or disabled Tracer yields a no-op
// provider (no spans), independent of any global provider. Attach it with
// grpc.StatsHandler(...); the interceptor form is deprecated upstream.
func GRPCStatsHandler(t *Tracer) stats.Handler {
	var provider trace.TracerProvider = noop.NewTracerProvider()
	if t != nil && t.provider != nil {
		provider = t.provider
	}
	return otelgrpc.NewServerHandler(otelgrpc.WithTracerProvider(provider))
}
