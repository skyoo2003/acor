// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"testing"

	"github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

func TestValidateScriptArgs(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]interface{}
		wantErr bool
	}{
		{
			name:    "valid args with both keys",
			args:    map[string]interface{}{"trieKey": "key1", "outputsKey": "key2"},
			wantErr: false,
		},
		{
			name:    "nil args",
			args:    nil,
			wantErr: true,
		},
		{
			name:    "empty args",
			args:    map[string]interface{}{},
			wantErr: true,
		},
		{
			name:    "missing trieKey",
			args:    map[string]interface{}{"outputsKey": "key2"},
			wantErr: true,
		},
		{
			name:    "missing outputsKey",
			args:    map[string]interface{}{"trieKey": "key1"},
			wantErr: true,
		},
		{
			name:    "trieKey is wrong type (int)",
			args:    map[string]interface{}{"trieKey": 123, "outputsKey": "key2"},
			wantErr: true,
		},
		{
			name:    "outputsKey is wrong type (int)",
			args:    map[string]interface{}{"trieKey": "key1", "outputsKey": 456},
			wantErr: true,
		},
		{
			name:    "trieKey is wrong type (nil)",
			args:    map[string]interface{}{"trieKey": nil, "outputsKey": "key2"},
			wantErr: true,
		},
		{
			name:    "outputsKey is wrong type (nil)",
			args:    map[string]interface{}{"trieKey": "key1", "outputsKey": nil},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateScriptArgs(tt.args)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateScriptArgs() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

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

	args, err := marshalTrieArgs(snap, map[string]string{}, largeVersionAbove2to53+1)
	if err != nil {
		t.Fatal(err)
	}
	args["trieKey"] = trieKey("test")
	args["outputsKey"] = outputsKey("test")

	cmd, err := ops.runAddV2Script(ctx, ops.client, args)
	if err != nil {
		t.Fatalf("runAddV2Script failed: %v", err)
	}
	result, err := cmd.Int64()
	if err != nil {
		t.Fatalf("cmd.Int64() failed: %v", err)
	}
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

	args, err := marshalTrieArgs(snap, map[string]string{}, largeVersionAbove2to53+1)
	if err != nil {
		t.Fatal(err)
	}
	args["trieKey"] = trieKey("test")
	args["outputsKey"] = outputsKey("test")

	cmd, err := ops.runRemoveV2Script(ctx, ops.client, args)
	if err != nil {
		t.Fatalf("runRemoveV2Script failed: %v", err)
	}
	result, err := cmd.Int64()
	if err != nil {
		t.Fatalf("cmd.Int64() failed: %v", err)
	}
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
