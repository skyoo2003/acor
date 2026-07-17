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
	}{
		{"debug"},
		{"info"},
		{"warn"},
		{"error"},
		{"unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			buf := &bytes.Buffer{}
			l := NewLogger(buf, tt.level)
			if l == nil {
				t.Errorf("expected non-nil logger for level %q", tt.level)
			}
		})
	}
}
