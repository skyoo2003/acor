// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

func TestGenerateVersionBasic(t *testing.T) {
	t.Run("returns valid int64", func(t *testing.T) {
		v, err := generateVersion()
		if err != nil {
			t.Fatalf("generateVersion() error: %v", err)
		}
		if v == 0 {
			t.Error("generateVersion() = 0, want nonzero")
		}
	})

	t.Run("consecutive calls return different values", func(t *testing.T) {
		seen := make(map[int64]struct{}, 100)
		for i := 0; i < 100; i++ {
			v, err := generateVersion()
			if err != nil {
				t.Fatalf("generateVersion() error: %v", err)
			}
			if _, exists := seen[v]; exists {
				t.Errorf("generateVersion() returned duplicate value %d at iteration %d", v, i)
			}
			seen[v] = struct{}{}
		}
	})
}

func TestLuaVersionComparisonAbove2to53(t *testing.T) {
	// This test verifies that Lua version comparison works correctly for
	// int64 values exceeding 2^53 (9007199254740992), where Lua's tonumber()
	// loses precision.
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()
	seedV2Trie(t, mr, []string{"he"})

	snap, err := readTrieSnapshot(ctx, ops.storage, ops.name)
	if err != nil {
		t.Fatal(err)
	}

	// Use version values that exceed 2^53 where tonumber loses precision.
	// 2^53 = 9007199254740992
	// tonumber cannot distinguish 9007199254740993 from 9007199254740994.
	largeOldVersion := int64(9007199254740993) // 2^53 + 1
	largeNewVersion := int64(9007199254740994) // 2^53 + 2

	// Set the trie version to largeOldVersion in Redis directly
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()
	if hErr := client.HSet(ctx, trieKey("test"), "version", largeOldVersion).Err(); hErr != nil {
		t.Fatal(hErr)
	}

	// Build args with the matching large oldVersion — should succeed
	snap.Version = largeOldVersion
	args, err := marshalTrieArgs(snap, map[string]string{}, largeNewVersion)
	if err != nil {
		t.Fatal(err)
	}
	args["trieKey"] = trieKey("test")
	args["outputsKey"] = outputsKey("test")

	cmd, err := ops.runAddV2Script(ctx, ops.client, args)
	if err != nil {
		t.Fatalf("addV2Script with matching large version failed: %v", err)
	}
	result, err := cmd.Int()
	if err != nil {
		t.Fatalf("addV2Script with matching large version failed: %v", err)
	}
	if result != 1 {
		t.Errorf("addV2Script = %d, want 1 (version should match for %d)", result, largeOldVersion)
	}

	// Now try with oldVersion that no longer matches — should detect conflict
	snap.Version = largeOldVersion // trie now has largeNewVersion
	args2, err := marshalTrieArgs(snap, map[string]string{}, largeNewVersion+1)
	if err != nil {
		t.Fatal(err)
	}
	args2["trieKey"] = trieKey("test")
	args2["outputsKey"] = outputsKey("test")

	cmd2, err := ops.runAddV2Script(ctx, ops.client, args2)
	if err != nil {
		t.Fatalf("addV2Script conflict check failed: %v", err)
	}
	result2, err := cmd2.Int()
	if err != nil {
		t.Fatalf("addV2Script conflict check failed: %v", err)
	}
	if result2 != 0 {
		t.Errorf("addV2Script = %d, want 0 (conflict between %d and %d should be detected)",
			result2, largeOldVersion, largeNewVersion)
	}
}

func TestLuaVersionStringComparison(t *testing.T) {
	tests := []struct {
		name          string
		storedVersion int64
		oldVersion    int64 // what we send as oldVersion in the script
		wantConflict  bool
	}{
		{
			name:          "exact match above 2^53",
			storedVersion: 9007199254740993,
			oldVersion:    9007199254740993,
			wantConflict:  false,
		},
		{
			name:          "adjacent values above 2^53",
			storedVersion: 9007199254740993,
			oldVersion:    9007199254740994, // stale: Redis has X, we send X+1
			wantConflict:  true,
		},
		{
			name:          "straddling 2^53 boundary",
			storedVersion: 9007199254740992,
			oldVersion:    9007199254740993, // stale: Redis has X, we send X+1
			wantConflict:  true,
		},
		{
			name:          "zero version",
			storedVersion: 0,
			oldVersion:    0,
			wantConflict:  false,
		},
		{
			name:          "large negative-ish via high bit",
			storedVersion: -1,
			oldVersion:    -1,
			wantConflict:  false,
		},
		{
			name:          "max int64 values",
			storedVersion: 9223372036854775800,
			oldVersion:    9223372036854775801, // stale
			wantConflict:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mr := miniredis.RunT(t)
			defer mr.Close()

			ops := newTestV2Ops(t, mr)
			defer func() { _ = ops.client.Close() }()

			ctx := context.Background()
			seedV2Trie(t, mr, []string{"he"})

			// Set version in Redis
			client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
			defer func() { _ = client.Close() }()
			if err := client.HSet(ctx, trieKey("test"), "version", tt.storedVersion).Err(); err != nil {
				t.Fatal(err)
			}

			snap, err := readTrieSnapshot(ctx, ops.storage, ops.name)
			if err != nil {
				t.Fatal(err)
			}

			// Set oldVersion in snapshot to what we'll send to the Lua script.
			// For no-conflict: this matches Redis → script succeeds.
			// For conflict: this differs from Redis → script rejects.
			snap.Version = tt.oldVersion

			newVersion := tt.oldVersion + 1

			args, err := marshalTrieArgs(snap, map[string]string{}, newVersion)
			if err != nil {
				t.Fatal(err)
			}
			args["trieKey"] = trieKey("test")
			args["outputsKey"] = outputsKey("test")

			cmd, err := ops.runAddV2Script(ctx, ops.client, args)
			if err != nil {
				t.Fatalf("addV2Script error: %v", err)
			}
			result, err := cmd.Int()
			if err != nil {
				t.Fatalf("addV2Script error: %v", err)
			}

			if tt.wantConflict {
				if result != 0 {
					t.Errorf("addV2Script = %d, want 0 (conflict expected: stored=%d, sent=%d)",
						result, tt.storedVersion, tt.oldVersion)
				}
			} else {
				if result != 1 {
					t.Errorf("addV2Script = %d, want 1 (match expected for version=%d)",
						result, tt.storedVersion)
				}
			}
		})
	}
}

func TestLuaVersionRemoveScriptPrecision(t *testing.T) {
	// Same precision test for the removeV2Script
	mr := miniredis.RunT(t)
	defer mr.Close()

	ops := newTestV2Ops(t, mr)
	defer func() { _ = ops.client.Close() }()

	ctx := context.Background()
	seedV2Trie(t, mr, []string{"he", "she"})

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	largeVersion := int64(9007199254740993)
	if err := client.HSet(ctx, trieKey("test"), "version", largeVersion).Err(); err != nil {
		t.Fatal(err)
	}

	snap, err := readTrieSnapshot(ctx, ops.storage, ops.name)
	if err != nil {
		t.Fatal(err)
	}
	snap.Version = largeVersion

	newVersion := largeVersion + 1
	args, err := marshalTrieArgs(snap, map[string]string{}, newVersion)
	if err != nil {
		t.Fatal(err)
	}
	args["trieKey"] = trieKey("test")
	args["outputsKey"] = outputsKey("test")

	cmd, err := ops.runRemoveV2Script(ctx, ops.client, args)
	if err != nil {
		t.Fatalf("removeV2Script error: %v", err)
	}
	result, err := cmd.Int()
	if err != nil {
		t.Fatalf("removeV2Script error: %v", err)
	}
	if result != 1 {
		t.Errorf("removeV2Script = %d, want 1 (version %d should match)", result, largeVersion)
	}

	// Stale version should be rejected
	args2, err := marshalTrieArgs(snap, map[string]string{}, newVersion+1)
	if err != nil {
		t.Fatal(err)
	}
	args2["trieKey"] = trieKey("test")
	args2["outputsKey"] = outputsKey("test")

	cmd2, err := ops.runRemoveV2Script(ctx, ops.client, args2)
	if err != nil {
		t.Fatalf("removeV2Script stale version error: %v", err)
	}
	result2, err := cmd2.Int()
	if err != nil {
		t.Fatalf("removeV2Script stale version error: %v", err)
	}
	if result2 != 0 {
		t.Errorf("removeV2Script = %d, want 0 (stale version should conflict)", result2)
	}

	// Verify: the version in Redis should be the new one
	stored, _ := client.HGet(ctx, trieKey("test"), "version").Result()
	if stored != fmt.Sprintf("%d", newVersion) {
		t.Errorf("stored version = %s, want %d", stored, newVersion)
	}
}
