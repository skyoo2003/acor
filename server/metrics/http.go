// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/skyoo2003/acor/server/internal/httpx"
)

var (
	uuidPattern   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	numberPattern = regexp.MustCompile(`^\d+$`)
)

// normalizePath collapses identifier-shaped path segments into placeholders so
// metric labels stay bounded: /v1/users/42 and /v1/users/43 share one series.
func normalizePath(path string) string {
	if path == "" || path[0] != '/' {
		return "/"
	}

	segments := strings.FieldsFunc(path, func(r rune) bool { return r == '/' })
	for i, seg := range segments {
		switch {
		case uuidPattern.MatchString(seg):
			segments[i] = "{uuid}"
		case numberPattern.MatchString(seg):
			segments[i] = "{id}"
		}
	}
	return "/" + strings.Join(segments, "/")
}

func HTTPMiddleware(reg *Registry) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			path := normalizePath(r.URL.Path)
			method := r.Method

			wrapped := httpx.WrapResponseWriter(w)
			next.ServeHTTP(wrapped, r)

			duration := time.Since(start).Seconds()
			status := strconv.Itoa(wrapped.Status())

			reg.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
			reg.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration)
		})
	}
}
