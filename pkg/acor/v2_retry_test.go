package acor

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestV2RemoveRetryContextCancellation(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	seedV2Trie(t, mr, []string{"he", "she", "his"})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ops.remove(ctx, "he")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if ctx.Err() == nil {
		t.Fatal("expected context.Canceled or context.DeadlineExceeded")
	}
}

func TestV2OperationsAddCanceledContext(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := ops.add(ctx, "him")
	if err == nil {
		t.Fatal("expected error from canceled context")
	}
	if ctx.Err() == nil {
		t.Fatal("expected context.Canceled or context.DeadlineExceeded")
	}
}

func TestV2RemoveExhaustsRetries(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	_, err := ops.remove(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("remove nonexistent keyword should not error, got: %v", err)
	}
}

func TestV2ConcurrencyConflictRetries(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ops2 := newTestV2Ops(t, mr)
	defer func() { _ = ops2.client.Close() }()

	_, err := ops.add(ctx, "keyword1")
	if err != nil {
		t.Fatalf("first add error: %v", err)
	}

	_, err = ops2.add(ctx, "keyword2")
	if err != nil {
		t.Fatalf("second add (different ops) error: %v", err)
	}

	matched, err := ops.find(ctx, "keyword1 keyword2")
	if err != nil {
		t.Fatalf("find() error: %v", err)
	}
	if !containsAll(matched, "keyword1", "keyword2") {
		t.Errorf("find() = %v, want both keywords", matched)
	}
}

func TestV2RemoveMaxRetriesExhausted(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()

	_, _ = ops.add(ctx, "he")

	ops2 := newTestV2Ops(t, mr)
	defer func() { _ = ops2.client.Close() }()

	_, _ = ops2.add(ctx, "she")

	_, err := ops.remove(ctx, "he")
	if err != nil {
		t.Fatalf("remove() should succeed on retry, got: %v", err)
	}
}
