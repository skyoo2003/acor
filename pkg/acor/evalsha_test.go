// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"testing"

	"github.com/alicebob/miniredis/v2"
)

func TestAddV2ScriptBehavioralEquivalence(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()
	seedV2Trie(t, mr, []string{"he"})

	// First add should succeed
	added, err := ops.add(ctx, "she")
	if err != nil {
		t.Fatalf("first add failed: %v", err)
	}
	if added != 1 {
		t.Errorf("first add = %d, want 1", added)
	}

	// Duplicate add should return 0
	dup, err := ops.add(ctx, "she")
	if err != nil {
		t.Fatalf("duplicate add failed: %v", err)
	}
	if dup != 0 {
		t.Errorf("duplicate add = %d, want 0", dup)
	}

	// Find should match keywords (note: "he" matches inside "she" too)
	matched, err := ops.find(ctx, "he she")
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if !containsAll(matched, "he", "she") {
		t.Errorf("find = %v, want he and she", matched)
	}
}

func TestRemoveV2ScriptBehavioralEquivalence(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()
	seedV2Trie(t, mr, []string{"he", "she", "his"})

	// Remove existing keyword
	removed, err := ops.remove(ctx, "she")
	if err != nil {
		t.Fatalf("remove failed: %v", err)
	}
	if removed != 1 {
		t.Errorf("remove = %d, want 1", removed)
	}

	// Remove nonexistent keyword
	removed, err = ops.remove(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("remove nonexistent failed: %v", err)
	}
	if removed != 0 {
		t.Errorf("remove nonexistent = %d, want 0", removed)
	}

	// Verify find no longer matches removed keyword directly
	// (note: "he" still matches as a prefix of other keywords)
	matched, err := ops.find(ctx, "she")
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	for _, m := range matched {
		if m == "she" {
			t.Errorf("find('she') still matches 'she' after removal, got %v", matched)
			break
		}
	}
}

func TestScriptMultipleInvocations(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()
	seedV2Trie(t, mr, []string{"a"})

	// Rapid sequential adds
	for i := 0; i < 20; i++ {
		kw := string(rune('b' + i))
		added, err := ops.add(ctx, kw)
		if err != nil {
			t.Fatalf("add(%q) at iteration %d failed: %v", kw, i, err)
		}
		if added != 1 {
			t.Errorf("add(%q) = %d, want 1 at iteration %d", kw, added, i)
		}
	}

	// Verify all keywords are findable
	matched, err := ops.find(ctx, "abcdefghijklmnopqrst")
	if err != nil {
		t.Fatalf("find failed: %v", err)
	}
	if len(matched) != 20 {
		t.Errorf("find returned %d matches, want 20", len(matched))
	}

	// Rapid sequential removes
	for i := 0; i < 20; i++ {
		kw := string(rune('a' + i))
		removed, err := ops.remove(ctx, kw)
		if err != nil {
			t.Fatalf("remove(%q) at iteration %d failed: %v", kw, i, err)
		}
		if removed != 1 {
			t.Errorf("remove(%q) = %d, want 1 at iteration %d", kw, removed, i)
		}
	}
}
