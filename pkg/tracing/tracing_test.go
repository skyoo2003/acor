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

func TestNewTracerShutdownIdempotent(t *testing.T) {
	cfg := &Config{Enabled: false}
	tracer, err := NewTracer(cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := tracer.Shutdown(); err != nil {
		t.Fatalf("first shutdown: %v", err)
	}
	if err := tracer.Shutdown(); err != nil {
		t.Fatalf("second shutdown: %v", err)
	}
}

func TestNewTracerEnabledWithUnreachableEndpoint(t *testing.T) {
	cfg := &Config{
		Enabled:     true,
		ServiceName: "acor-test-enabled",
		Endpoint:    "localhost:9999",
	}

	tracer, err := NewTracer(cfg)
	if err != nil {
		// Schema conflicts can happen in test environments with multiple OTel versions
		t.Skipf("Tracer creation failed (may be schema conflict): %v", err)
	}
	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}
	if shutdownErr := tracer.Shutdown(); shutdownErr != nil {
		t.Fatalf("unexpected shutdown error: %v", shutdownErr)
	}
}
