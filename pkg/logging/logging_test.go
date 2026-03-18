package logging

import (
	"bytes"
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
