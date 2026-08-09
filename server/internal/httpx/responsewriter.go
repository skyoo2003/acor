// SPDX-License-Identifier: Apache-2.0

// Package httpx holds the http.Handler plumbing the observability middlewares
// share. It is internal to the server module: nothing outside it can import
// this, so the types here are free to change without a compatibility question.
package httpx

import (
	"bufio"
	"errors"
	"net"
	"net/http"
)

// ResponseWriter records the status code a handler wrote, which is otherwise
// unreadable once the handler returns. The logging, metrics, and tracing
// middlewares each need exactly that, and each used to carry its own copy —
// three copies that had already drifted, the tracing one lacking the Hijack and
// Flush forwarding the other two had.
//
// That drift is why the forwarding matters. Wrapping a ResponseWriter hides the
// optional interfaces the original satisfied, so a handler doing
// w.(http.Flusher) sees the wrapper instead and silently loses streaming;
// server-sent events stop flushing and a websocket upgrade fails outright. The
// two methods below hand those capabilities back.
type ResponseWriter struct {
	http.ResponseWriter
	status int
}

// WrapResponseWriter returns w wrapped to record its status code. A handler that
// never calls WriteHeader has written 200, which is what Status reports.
func WrapResponseWriter(w http.ResponseWriter) *ResponseWriter {
	return &ResponseWriter{ResponseWriter: w, status: http.StatusOK}
}

// Status returns the code the handler wrote.
func (rw *ResponseWriter) Status() int { return rw.status }

func (rw *ResponseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *ResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := rw.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, errors.New("hijack not supported")
}

func (rw *ResponseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
