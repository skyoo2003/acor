// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewLogger(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(buf, "info")
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}

	logger.Info().Msg("test")
	if buf.Len() == 0 {
		t.Error("expected log output")
	}
}

func TestLogLevel(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(buf, "warn")

	logger.Debug().Msg("should not appear")
	if buf.Len() > 0 {
		t.Error("debug log should not appear at warn level")
	}

	logger.Warn().Msg("should appear")
	if buf.Len() == 0 {
		t.Error("expected warn log output")
	}
}

func TestWithTraceID(t *testing.T) {
	buf := &bytes.Buffer{}
	logger := NewLogger(buf, "info")

	traceLogger := logger.WithTraceID("abc123", "def456")
	if traceLogger == nil {
		t.Fatal("expected non-nil logger")
	}

	traceLogger.Info().Msg("with trace")
	output := buf.String()
	if !strings.Contains(output, "abc123") {
		t.Error("expected trace_id in output")
	}
	if !strings.Contains(output, "def456") {
		t.Error("expected span_id in output")
	}
}

func TestNewLoggerAllLevels(t *testing.T) {
	tests := []struct {
		level string
		// wantDebug and wantError record whether an event at that level is
		// emitted, which pins the resolved level instead of just non-nilness.
		wantDebug bool
		wantError bool
	}{
		{level: "trace", wantDebug: true, wantError: true},
		{level: "debug", wantDebug: true, wantError: true},
		{level: "DEBUG", wantDebug: true, wantError: true}, // ParseLevel is case-insensitive
		{level: "info", wantDebug: false, wantError: true},
		{level: "warn", wantDebug: false, wantError: true},
		{level: "error", wantDebug: false, wantError: true},
		{level: "panic", wantDebug: false, wantError: false},
		{level: "disabled", wantDebug: false, wantError: false},
		{level: "unknown", wantDebug: false, wantError: true}, // falls back to info
		{level: "", wantDebug: false, wantError: true},        // falls back to info
		{level: "99", wantDebug: false, wantError: true},      // out of range, not "silence everything"
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			buf := &bytes.Buffer{}
			l := NewLogger(buf, tt.level)

			l.Debug().Msg("dbg")
			if got := buf.Len() > 0; got != tt.wantDebug {
				t.Errorf("level %q: debug emitted = %v, want %v", tt.level, got, tt.wantDebug)
			}

			buf.Reset()
			l.Error().Msg("err")
			if got := buf.Len() > 0; got != tt.wantError {
				t.Errorf("level %q: error emitted = %v, want %v", tt.level, got, tt.wantError)
			}
		})
	}
}
