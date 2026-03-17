package tracing

import (
	"net/http"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func HTTPMiddleware(tracer *Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := tracer.Tracer.Start(
				r.Context(),
				"HTTP "+r.Method+" "+r.URL.Path,
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.route", r.URL.Path),
				),
			)
			defer span.End()

			next.ServeHTTP(w, r.WithContext(ctx))

			span.SetAttributes(
				attribute.Int("http.status_code", 200),
			)
			span.SetStatus(codes.Ok, "")
		})
	}
}
