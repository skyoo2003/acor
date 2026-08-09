// SPDX-License-Identifier: Apache-2.0

package httpx

import (
	"bufio"
	"errors"
	"net"
	"net/http"
	"testing"
)

// errHijacked is what the fake writers return from Hijack: the test only needs
// to prove the call reached the writer underneath, not to produce a conn.
var errHijacked = errors.New("hijacked")

// bare implements http.ResponseWriter and nothing else, so each fake below opts
// in to exactly the optional interfaces its name claims.
type bare struct {
	header  http.Header
	written int
}

func newBare() *bare { return &bare{header: http.Header{}} }

func (w *bare) Header() http.Header         { return w.header }
func (w *bare) Write(b []byte) (int, error) { return len(b), nil }
func (w *bare) WriteHeader(code int)        { w.written = code }

type flusherOnly struct {
	*bare
	flushed bool
}

func (w *flusherOnly) Flush() { w.flushed = true }

type hijackerOnly struct {
	*bare
}

func (w *hijackerOnly) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errHijacked
}

type flusherHijacker struct {
	*flusherOnly
}

func (w *flusherHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errHijacked
}

func TestWrapResponseWriterForwardsOnlyRealCapabilities(t *testing.T) {
	tests := []struct {
		name       string
		writer     http.ResponseWriter
		wantFlush  bool
		wantHijack bool
	}{
		{name: "neither", writer: newBare()},
		{name: "flusher", writer: &flusherOnly{bare: newBare()}, wantFlush: true},
		{name: "hijacker", writer: &hijackerOnly{bare: newBare()}, wantHijack: true},
		{
			name:       "both",
			writer:     &flusherHijacker{flusherOnly: &flusherOnly{bare: newBare()}},
			wantFlush:  true,
			wantHijack: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := WrapResponseWriter(tt.writer)

			if _, ok := wrapped.(http.Flusher); ok != tt.wantFlush {
				t.Errorf("http.Flusher assertion = %v, want %v", ok, tt.wantFlush)
			}
			if _, ok := wrapped.(http.Hijacker); ok != tt.wantHijack {
				t.Errorf("http.Hijacker assertion = %v, want %v", ok, tt.wantHijack)
			}
			if got := wrapped.Unwrap(); got != tt.writer {
				t.Errorf("Unwrap() = %v, want the writer passed in", got)
			}
		})
	}
}

func TestWrapResponseWriterReachesTheWriterUnderneath(t *testing.T) {
	inner := &flusherHijacker{flusherOnly: &flusherOnly{bare: newBare()}}
	wrapped := WrapResponseWriter(inner)

	flusher, ok := wrapped.(http.Flusher)
	if !ok {
		t.Fatal("wrapped writer does not implement http.Flusher")
	}
	flusher.Flush()
	if !inner.flushed {
		t.Error("Flush did not reach the writer underneath")
	}

	hijacker, ok := wrapped.(http.Hijacker)
	if !ok {
		t.Fatal("wrapped writer does not implement http.Hijacker")
	}
	if _, _, err := hijacker.Hijack(); !errors.Is(err, errHijacked) {
		t.Errorf("Hijack error = %v, want %v", err, errHijacked)
	}
}

func TestWrapResponseWriterRecordsStatus(t *testing.T) {
	inner := newBare()
	wrapped := WrapResponseWriter(inner)

	if got := wrapped.Status(); got != http.StatusOK {
		t.Errorf("Status() before WriteHeader = %d, want %d", got, http.StatusOK)
	}

	wrapped.WriteHeader(http.StatusTeapot)

	if got := wrapped.Status(); got != http.StatusTeapot {
		t.Errorf("Status() = %d, want %d", got, http.StatusTeapot)
	}
	if inner.written != http.StatusTeapot {
		t.Errorf("writer underneath saw %d, want %d", inner.written, http.StatusTeapot)
	}
}
