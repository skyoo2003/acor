// SPDX-License-Identifier: Apache-2.0

package metrics

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
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

			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(wrapped, r)

			duration := time.Since(start).Seconds()
			status := strconv.Itoa(wrapped.statusCode)

			reg.HTTPRequestsTotal.WithLabelValues(method, path, status).Inc()
			reg.HTTPRequestDuration.WithLabelValues(method, path).Observe(duration)
		})
	}
}

type responseWriter struct {
	http.ResponseWriter
	statusCode int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
