package tracing

import (
	"testing"
)

func TestNewTracer(t *testing.T) {
	cfg := &Config{
		Enabled:     false,
		ServiceName: "acor",
	}

	tracer, err := NewTracer(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}
}

func TestNewTracerDisabled(t *testing.T) {
	cfg := &Config{
		Enabled:     false,
		ServiceName: "acor",
	}

	tracer, err := NewTracer(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := tracer.Shutdown(); err != nil {
		t.Fatalf("unexpected shutdown error: %v", err)
	}
}
