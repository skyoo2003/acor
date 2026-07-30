// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"io"

	"github.com/rs/zerolog"
)

type Logger struct {
	zerolog.Logger
}

// NewLogger returns a JSON logger writing to w at any level
// zerolog.ParseLevel accepts. ParseLevel also parses numeric strings, so the
// result is range-checked: "99" would otherwise silence every event rather
// than fall back to info like any other unrecognized value.
func NewLogger(w io.Writer, level string) *Logger {
	zl := zerolog.New(w).With().Timestamp().Logger()

	parsed, err := zerolog.ParseLevel(level)
	if err != nil || parsed == zerolog.NoLevel ||
		parsed < zerolog.TraceLevel || parsed > zerolog.Disabled {
		parsed = zerolog.InfoLevel
	}

	return &Logger{zl.Level(parsed)}
}

func (l *Logger) WithTraceID(traceID, spanID string) *Logger {
	return &Logger{l.Logger.With().Str("trace_id", traceID).Str("span_id", spanID).Logger()}
}
