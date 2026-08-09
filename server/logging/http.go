// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"net/http"
	"time"

	"github.com/rs/zerolog"

	"github.com/skyoo2003/acor/server/internal/httpx"
)

const (
	statusServerErrorThreshold = 500
	statusClientErrorThreshold = 400
)

func HTTPMiddleware(logger *Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			wrapped := httpx.WrapResponseWriter(w)
			next.ServeHTTP(wrapped, r)

			duration := time.Since(start)

			var event *zerolog.Event
			switch {
			case wrapped.Status() >= statusServerErrorThreshold:
				event = logger.Error()
			case wrapped.Status() >= statusClientErrorThreshold:
				event = logger.Warn()
			default:
				event = logger.Info()
			}

			event.
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", wrapped.Status()).
				Int64("latency_ms", duration.Milliseconds()).
				Msg("request completed")
		})
	}
}
