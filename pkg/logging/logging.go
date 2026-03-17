package logging

import (
	"io"

	"github.com/rs/zerolog"
)

type Logger struct {
	zerolog.Logger
}

func NewLogger(w io.Writer, level string) *Logger {
	zl := zerolog.New(w).With().Timestamp().Logger()

	switch level {
	case "debug":
		zl = zl.Level(zerolog.DebugLevel)
	case "info":
		zl = zl.Level(zerolog.InfoLevel)
	case "warn":
		zl = zl.Level(zerolog.WarnLevel)
	case "error":
		zl = zl.Level(zerolog.ErrorLevel)
	default:
		zl = zl.Level(zerolog.InfoLevel)
	}

	return &Logger{zl}
}

func (l *Logger) WithTraceID(traceID, spanID string) *Logger {
	return &Logger{l.Logger.With().Str("trace_id", traceID).Str("span_id", spanID).Logger()}
}
