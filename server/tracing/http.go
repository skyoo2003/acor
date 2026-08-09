// SPDX-License-Identifier: Apache-2.0

package tracing

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/skyoo2003/acor/server/internal/httpx"
)

const (
	errorStatusCodeThreshold = 400
)

func HTTPMiddleware(tracer *Tracer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := otel.GetTextMapPropagator().Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			ctx, span := tracer.Tracer.Start(
				ctx,
				"HTTP "+r.Method+" "+r.URL.Path,
				trace.WithAttributes(
					attribute.String("http.method", r.Method),
					attribute.String("http.route", r.URL.Path),
				),
			)
			defer span.End()

			wrapped := httpx.WrapResponseWriter(w)
			next.ServeHTTP(wrapped, r.WithContext(ctx))

			span.SetAttributes(
				attribute.Int("http.status_code", wrapped.Status()),
			)
			if wrapped.Status() >= errorStatusCodeThreshold {
				span.SetStatus(codes.Error, http.StatusText(wrapped.Status()))
			} else {
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}
