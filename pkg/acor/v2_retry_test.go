// SPDX-License-Identifier: Apache-2.0

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

// TestConflictBackoffBounds pins the backoff window per attempt: a linear ramp
// with jitter that never reaches into the next attempt's slot, so the schedule
// stays ordered while still spreading concurrent writers.
func TestConflictBackoffBounds(t *testing.T) {
	for attempt := range maxRetries {
		lo := time.Duration(attempt+1) * retryBackoffBase
		hi := lo + retryBackoffBase

		// Sample enough times to catch a jitter range that is off by a factor.
		var sawJitter bool
		for range 200 {
			got := conflictBackoff(attempt)
			if got < lo || got >= hi {
				t.Fatalf("conflictBackoff(%d) = %v, want [%v, %v)", attempt, got, lo, hi)
			}
			if got != lo {
				sawJitter = true
			}
		}
		if !sawJitter {
			t.Errorf("conflictBackoff(%d) never varied from %v; jitter is not being applied", attempt, lo)
		}
	}
}
