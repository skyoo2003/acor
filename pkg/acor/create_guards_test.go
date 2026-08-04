// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"testing"
)

func newPresetAC(t *testing.T, name string) *AhoCorasick {
	t.Helper()
	mr := createTestRedisServer(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   name,
		Preset: PresetBalanced,
	})
	if err != nil {
		t.Fatalf("Create() error: %v", err)
	}
	t.Cleanup(func() { _ = ac.Close() })
	return ac
}

// Preset mode holds no Redis client, so both migration entry points used to
// dereference a nil interface instead of reporting that they do not apply.
func TestMigrationRejectsPresetMode(t *testing.T) {
	t.Run("MigrateV1ToV2", func(t *testing.T) {
		ac := newPresetAC(t, "migrate-preset")
		result, err := ac.MigrateV1ToV2(nil)
		if !errors.Is(err, ErrMigrationRequiresRedis) {
			t.Fatalf("expected ErrMigrationRequiresRedis, got %v", err)
		}
		if result != nil {
			t.Fatalf("expected no result, got %+v", result)
		}
	})

	t.Run("RollbackToV1", func(t *testing.T) {
		ac := newPresetAC(t, "rollback-preset")
		if err := ac.RollbackToV1(); !errors.Is(err, ErrMigrationRequiresRedis) {
			t.Fatalf("expected ErrMigrationRequiresRedis, got %v", err)
		}
	})
}

// The Redis-backed modes must keep reaching the real migration logic: the guard
// above must not reject them. A V2 collection reports ErrAlreadyV2 instead.
func TestMigrationAllowsRedisBackedMode(t *testing.T) {
	ac, _ := createAhoCorasick(t)
	defer ac.Close()

	_, err := ac.MigrateV1ToV2(nil)
	if errors.Is(err, ErrMigrationRequiresRedis) {
		t.Fatalf("guard rejected a Redis-backed instance: %v", err)
	}
	if !errors.Is(err, ErrAlreadyV2) {
		t.Fatalf("expected ErrAlreadyV2, got %v", err)
	}
}

// EnableCache is never read in the preset path, so accepting the pair silently
// dropped the caching the caller asked for.
func TestCreateRejectsCacheWithPreset(t *testing.T) {
	mr := createTestRedisServer(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "cache-and-preset",
		Preset:      PresetBalanced,
		EnableCache: true,
	})
	if !errors.Is(err, ErrCacheWithPreset) {
		t.Fatalf("expected ErrCacheWithPreset, got %v", err)
	}
	if ac != nil {
		_ = ac.Close()
		t.Fatal("expected no instance when the config is rejected")
	}
}

// Both constructors read args.Name before anything else, so a nil args used to
// panic on the very first line of the public entry point.
func TestCreateRejectsNilArgs(t *testing.T) {
	if _, err := Create(nil); !errors.Is(err, ErrNilArgs) {
		t.Fatalf("Create(nil): expected ErrNilArgs, got %v", err)
	}
	if _, err := CreateContext(context.Background(), nil); !errors.Is(err, ErrNilArgs) {
		t.Fatalf("CreateContext(nil): expected ErrNilArgs, got %v", err)
	}
}

func TestCreateContextHonorsCanceledContext(t *testing.T) {
	for _, tc := range []struct {
		name string
		args func(addr string) *AhoCorasickArgs
	}{
		{
			name: "original",
			args: func(addr string) *AhoCorasickArgs {
				return &AhoCorasickArgs{Addr: addr, Name: "ctx-original"}
			},
		},
		{
			name: "preset",
			args: func(addr string) *AhoCorasickArgs {
				return &AhoCorasickArgs{Addr: addr, Name: "ctx-preset", Preset: PresetBalanced}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mr := createTestRedisServer(t)
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			ac, err := CreateContext(ctx, tc.args(mr.Addr()))
			if err == nil {
				_ = ac.Close()
				t.Fatal("expected CreateContext to fail on a canceled context")
			}
			if ac != nil {
				_ = ac.Close()
				t.Fatal("expected no instance when construction fails")
			}
		})
	}
}

// The construction context must not become the instance's lifetime: canceling it
// after a successful CreateContext would otherwise stop the preset invalidation
// listener and leave a live instance that never sees another node's writes.
func TestCreateContextDoesNotBindInstanceLifetime(t *testing.T) {
	mr := createTestRedisServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // idempotent; keeps the ctx from leaking if the test fails early

	ac, err := CreateContext(ctx, &AhoCorasickArgs{
		Addr:   mr.Addr(),
		Name:   "ctx-lifetime",
		Preset: PresetBalanced,
	})
	if err != nil {
		t.Fatalf("CreateContext() error: %v", err)
	}
	defer ac.Close()

	cancel()

	if _, addErr := ac.Add("he"); addErr != nil {
		t.Fatalf("Add() after construction ctx cancel: %v", addErr)
	}
	matches, findErr := ac.Find("she")
	if findErr != nil {
		t.Fatalf("Find() after construction ctx cancel: %v", findErr)
	}
	if len(matches) != 1 || matches[0] != "he" {
		t.Fatalf("expected [he], got %v", matches)
	}
}
