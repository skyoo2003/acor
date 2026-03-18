package metrics

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"regexp"
	"strconv"
	"time"
)

var (
	uuidPattern   = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	numberPattern = regexp.MustCompile(`^\d+$`)
)

func normalizePath(path string) string {
	if path == "" || path == "/" {
		return "/"
	}

	segments := splitPath(path)
	for i, seg := range segments {
		if uuidPattern.MatchString(seg) {
			segments[i] = "{uuid}"
		} else if numberPattern.MatchString(seg) {
			segments[i] = "{id}"
		}
	}

	result := "/"
	for i, seg := range segments {
		if i > 0 {
			result += "/"
		}
		result += seg
	}
	return result
}

func splitPath(path string) []string {
	if path == "" || path[0] != '/' {
		return nil
	}

	result := []string{}
	start := 1
	for i := 1; i < len(path); i++ {
		if path[i] == '/' {
			if i > start {
				result = append(result, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		result = append(result, path[start:])
	}
	return result
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
