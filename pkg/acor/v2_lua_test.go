// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

// largeVersionAbove2to53 is a version value above 2^53 used by int64 safety tests.
// 2^53 = 9007199254740992. We use 2^53+1 to prove no truncation occurs while
// staying safely below math.MaxInt64 to avoid overflow when incrementing.
const largeVersionAbove2to53 = int64(9007199254740993)

// TestLuaScriptInt64SafetyAdd documents the contract that add script results
// should be read as int64 to avoid silent truncation on platforms where int < 64-bit.
func TestLuaScriptInt64SafetyAdd(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()
	seedV2Trie(t, mr, []string{"he", "she"})

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()
	if err := client.HSet(ctx, trieKey("test"), "version", largeVersionAbove2to53).Err(); err != nil {
		t.Fatal(err)
	}

	snap, err := readTrieSnapshot(ctx, ops.storage, ops.name)
	if err != nil {
		t.Fatal(err)
	}
	snap.Version = largeVersionAbove2to53

	args, err := marshalTrieArgs("test", snap, map[string]string{}, largeVersionAbove2to53+1)
	if err != nil {
		t.Fatal(err)
	}

	val, err := runV2Script(ctx, ops.client, args)
	if err != nil {
		t.Fatalf("addV2Script failed: %v", err)
	}
	result := val
	if result != 1 {
		t.Errorf("addV2Script Int64 result = %d, want 1", result)
	}
}

// TestLuaScriptInt64SafetyRemove documents the contract that remove script results
// should be read as int64 to avoid silent truncation on platforms where int < 64-bit.
func TestLuaScriptInt64SafetyRemove(t *testing.T) {
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()
	seedV2Trie(t, mr, []string{"he", "she"})

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()
	if err := client.HSet(ctx, trieKey("test"), "version", largeVersionAbove2to53).Err(); err != nil {
		t.Fatal(err)
	}

	snap, err := readTrieSnapshot(ctx, ops.storage, ops.name)
	if err != nil {
		t.Fatal(err)
	}
	snap.Version = largeVersionAbove2to53

	args, err := marshalTrieArgs("test", snap, map[string]string{}, largeVersionAbove2to53+1)
	if err != nil {
		t.Fatal(err)
	}

	args.ClearOutputs = true

	val, err := runV2Script(ctx, ops.client, args)
	if err != nil {
		t.Fatalf("removeV2Script failed: %v", err)
	}
	result := val
	if result != 1 {
		t.Errorf("removeV2Script Int64 result = %d, want 1", result)
	}

	// Verify the stored version in Redis is the exact int64 value
	stored, _ := client.HGet(ctx, trieKey("test"), "version").Result()
	expectedVersion := largeVersionAbove2to53 + 1
	if stored != fmt.Sprintf("%d", expectedVersion) {
		t.Errorf("stored version = %s, want %d", stored, expectedVersion)
	}
}
