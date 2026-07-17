// SPDX-License-Identifier: Apache-2.0

package acor //nolint:errcheck // integration test focuses on end-to-end behavior

import (
	"context"
	"os"
	"testing"
	"time"

	redis "github.com/redis/go-redis/v9"
)

// Integration tests run against a real Redis- or Valkey-compatible server.
// They are skipped unless ACOR_INTEGRATION_ADDR points at a reachable server,
// so the default `go test ./...` (miniredis, in-process) is unaffected. CI runs
// these against both redis and valkey service containers (see ci.yaml).
//
// Example:
//
//	ACOR_INTEGRATION_ADDR=localhost:6379 go test -run Integration ./pkg/acor

func integrationAddr(t *testing.T) string {
	t.Helper()
	addr := os.Getenv("ACOR_INTEGRATION_ADDR")
	if addr == "" {
		t.Skip("ACOR_INTEGRATION_ADDR not set; skipping real-server integration test")
	}
	return addr
}

// newIntegrationAC creates an instance against the real server under a unique
// collection name and flushes it clean before use.
func newIntegrationAC(t *testing.T, name string, args *AhoCorasickArgs) *AhoCorasick {
	t.Helper()
	args.Addr = integrationAddr(t)
	args.Name = name
	ac, err := Create(args)
	if err != nil {
		t.Fatalf("Create(%s) error: %v", name, err)
	}
	if err := ac.Flush(); err != nil {
		_ = ac.Close()
		t.Fatalf("Flush(%s) error: %v", name, err)
	}
	t.Cleanup(func() {
		_ = ac.Flush()
		_ = ac.Close()
	})
	return ac
}

func TestIntegrationCRUD(t *testing.T) {
	ac := newIntegrationAC(t, "acor-it-crud", &AhoCorasickArgs{})

	for _, kw := range []string{"he", "her", "him"} {
		if _, err := ac.Add(kw); err != nil {
			t.Fatalf("Add(%q) error: %v", kw, err)
		}
	}

	matched, err := ac.Find("he is him")
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}
	if !containsAll(matched, "he", "him") {
		t.Errorf("Find() = %v, want to contain he, him", matched)
	}

	idx, err := ac.FindIndex("him")
	if err != nil {
		t.Fatalf("FindIndex() error: %v", err)
	}
	if _, ok := idx["him"]; !ok {
		t.Errorf("FindIndex() = %v, want key him", idx)
	}

	suggestions, err := ac.Suggest("h")
	if err != nil {
		t.Fatalf("Suggest() error: %v", err)
	}
	if !containsAll(suggestions, "he", "her", "him") {
		t.Errorf("Suggest(h) = %v, want he, her, him", suggestions)
	}

	if _, remErr := ac.Remove("her"); remErr != nil {
		t.Fatalf("Remove() error: %v", remErr)
	}
	matched, err = ac.Find("her")
	if err != nil {
		t.Fatalf("Find() after remove error: %v", err)
	}
	if containsAll(matched, "her") {
		t.Errorf("Find(her) after Remove = %v, should not contain her", matched)
	}
}

func TestIntegrationBatch(t *testing.T) {
	ac := newIntegrationAC(t, "acor-it-batch", &AhoCorasickArgs{})

	if _, err := ac.AddMany([]string{"he", "her", "him", "his"}, &BatchOptions{Mode: BatchModeTransactional}); err != nil {
		t.Fatalf("AddMany() error: %v", err)
	}

	results, err := ac.FindMany([]string{"he is him", "this is hers"})
	if err != nil {
		t.Fatalf("FindMany() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("FindMany() returned %d entries, want 2", len(results))
	}

	if _, err := ac.RemoveMany([]string{"he", "her"}); err != nil {
		t.Fatalf("RemoveMany() error: %v", err)
	}
}

func TestIntegrationPreset(t *testing.T) {
	ac := newIntegrationAC(t, "acor-it-preset", &AhoCorasickArgs{Preset: PresetBalanced})

	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	matched, err := ac.Find("say hello world")
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}
	if !containsAll(matched, "hello") {
		t.Errorf("Find() = %v, want to contain hello", matched)
	}
}

// TestIntegrationCacheInvalidation validates that a write on one instance
// invalidates the local cache of a second instance via Redis/Valkey Pub/Sub.
func TestIntegrationCacheInvalidation(t *testing.T) {
	addr := integrationAddr(t)
	const name = "acor-it-cache"

	writer, err := Create(&AhoCorasickArgs{Addr: addr, Name: name, EnableCache: true})
	if err != nil {
		t.Fatalf("Create(writer) error: %v", err)
	}
	defer func() { _ = writer.Flush(); _ = writer.Close() }()
	if flushErr := writer.Flush(); flushErr != nil {
		t.Fatalf("Flush() error: %v", flushErr)
	}

	reader, err := Create(&AhoCorasickArgs{Addr: addr, Name: name, EnableCache: true})
	if err != nil {
		t.Fatalf("Create(reader) error: %v", err)
	}
	defer func() { _ = reader.Close() }()

	// Prime the reader's cache (empty collection → no match).
	if _, err := reader.Find("hello"); err != nil {
		t.Fatalf("reader.Find() warm-up error: %v", err)
	}

	if _, err := writer.Add("hello"); err != nil {
		t.Fatalf("writer.Add() error: %v", err)
	}

	// Pub/Sub delivery is async; poll until the reader observes the write.
	if !eventually(t, 3*time.Second, func() bool {
		matched, findErr := reader.Find("say hello")
		return findErr == nil && containsAll(matched, "hello")
	}) {
		t.Error("reader did not observe cross-instance write within timeout")
	}
}

func TestIntegrationMigration(t *testing.T) {
	addr := integrationAddr(t)
	const name = "acor-it-migrate"

	client := redis.NewClient(&redis.Options{Addr: addr})
	defer func() { _ = client.Close() }()
	ctx := context.Background()

	// Clean any prior run and seed the V1 root prefix so Create(V1) succeeds.
	client.Del(ctx, "{"+name+"}:prefix", "{"+name+"}:keyword", "{"+name+"}:trie", "{"+name+"}:outputs", "{"+name+"}:nodes")
	client.ZAdd(ctx, "{"+name+"}:prefix", redis.Z{Score: 0, Member: ""})

	v1, err := Create(&AhoCorasickArgs{Addr: addr, Name: name, SchemaVersion: SchemaV1})
	if err != nil {
		t.Fatalf("Create(V1) error: %v", err)
	}
	for _, kw := range []string{"he", "she", "his"} {
		if _, addErr := v1.Add(kw); addErr != nil {
			_ = v1.Close()
			t.Fatalf("V1 Add(%q) error: %v", kw, addErr)
		}
	}

	result, err := v1.MigrateV1ToV2(&MigrationOptions{KeepOldKeys: false})
	if err != nil {
		_ = v1.Close()
		t.Fatalf("MigrateV1ToV2() error: %v", err)
	}
	if result.ToSchema != SchemaV2 {
		t.Errorf("ToSchema = %d, want %d", result.ToSchema, SchemaV2)
	}
	_ = v1.Close()

	// A fresh instance must detect V2 and match the migrated keywords.
	v2, err := Create(&AhoCorasickArgs{Addr: addr, Name: name})
	if err != nil {
		t.Fatalf("Create(V2) error: %v", err)
	}
	t.Cleanup(func() {
		_ = v2.Flush()
		_ = v2.Close()
	})
	if v2.SchemaVersion() != SchemaV2 {
		t.Errorf("SchemaVersion() = %d, want %d", v2.SchemaVersion(), SchemaV2)
	}
	matched, err := v2.Find("she")
	if err != nil {
		t.Fatalf("Find() after migration error: %v", err)
	}
	if !containsAll(matched, "he", "she") {
		t.Errorf("Find(she) after migration = %v, want he, she", matched)
	}
}

func eventually(t *testing.T, timeout time.Duration, fn func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fn()
}
