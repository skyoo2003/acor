// SPDX-License-Identifier: Apache-2.0

// Package httpx holds the http.Handler plumbing the observability middlewares
// share. It is internal to the server module: nothing outside it can import
// this, so the types here are free to change without a compatibility question.
package httpx

import (
	"bufio"
	"net"
	"net/http"
)

// ResponseWriter records the status code a handler wrote, which is otherwise
// unreadable once the handler returns. The logging, metrics, and tracing
// middlewares each need exactly that, and each used to carry its own copy.
//
// Wrapping hides the optional interfaces the original writer satisfied, so a
// handler doing w.(http.Flusher) sees the wrapper instead and silently loses
// streaming; a websocket upgrade fails outright. Reporting those capabilities
// unconditionally trades that for the opposite lie — HTTP/2 has no Hijack, so
// the assertion would succeed and the call fail. WrapResponseWriter forwards
// only what the writer underneath actually implements, so an assertion answers
// what it would have answered with no middleware in the way. Unwrap covers the
// rest — deadlines, HTTP/2 full duplex — for handlers on ResponseController.
type ResponseWriter interface {
	http.ResponseWriter
	// Status returns the code the handler wrote, or 200 if it never called
	// WriteHeader.
	Status() int
	// Unwrap returns the writer underneath, which http.ResponseController
	// follows to reach capabilities this wrapper does not forward itself.
	Unwrap() http.ResponseWriter
}

// WrapResponseWriter returns w wrapped to record its status code, keeping
// whichever of http.Flusher and http.Hijacker w already implemented.
func WrapResponseWriter(w http.ResponseWriter) ResponseWriter {
	rec := &recorder{ResponseWriter: w, status: http.StatusOK}
	_, canFlush := w.(http.Flusher)
	_, canHijack := w.(http.Hijacker)
	switch {
	case canFlush && canHijack:
		return flushHijackRecorder{rec}
	case canFlush:
		return flushRecorder{rec}
	case canHijack:
		return hijackRecorder{rec}
	default:
		return rec
	}
}

type recorder struct {
	http.ResponseWriter
	status int
}

func (rw *recorder) Status() int { return rw.status }

func (rw *recorder) Unwrap() http.ResponseWriter { return rw.ResponseWriter }

func (rw *recorder) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// flush and hijack are reached only through a variant below, and
// WrapResponseWriter returns one of those only after checking the assertion
// holds.
func (rw *recorder) flush() { rw.ResponseWriter.(http.Flusher).Flush() }

func (rw *recorder) hijack() (net.Conn, *bufio.ReadWriter, error) {
	return rw.ResponseWriter.(http.Hijacker).Hijack()
}

type flushRecorder struct{ *recorder }

func (rw flushRecorder) Flush() { rw.flush() }

type hijackRecorder struct{ *recorder }

func (rw hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return rw.hijack() }

type flushHijackRecorder struct{ *recorder }

func (rw flushHijackRecorder) Flush() { rw.flush() }

func (rw flushHijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) { return rw.hijack() }
