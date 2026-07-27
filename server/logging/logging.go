// SPDX-License-Identifier: Apache-2.0

package logging

import (
	"io"
	"strings"

	"github.com/rs/zerolog"
)

type Logger struct {
	zerolog.Logger
}

// NewLogger returns a JSON logger writing to w at the named level, accepting
// any level zerolog.ParseLevel knows ("trace", "debug", "info", "warn",
// "error", "fatal", "panic", "disabled"). An unrecognized or empty level falls
// back to info.
func NewLogger(w io.Writer, level string) *Logger {
	zl := zerolog.New(w).With().Timestamp().Logger()

	parsed, err := zerolog.ParseLevel(strings.ToLower(level))
	if err != nil || parsed == zerolog.NoLevel {
		parsed = zerolog.InfoLevel
	}

	return &Logger{zl.Level(parsed)}
}

func (l *Logger) WithTraceID(traceID, spanID string) *Logger {
	return &Logger{l.Logger.With().Str("trace_id", traceID).Str("span_id", spanID).Logger()}
}
