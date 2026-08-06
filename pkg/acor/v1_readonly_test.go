// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"errors"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

// newV1ReadOnly builds a V1 instance with the production operations in place.
// createAhoCorasickV1 installs the fixture-writable wrapper, which would defeat the
// point of these tests.
func newV1ReadOnly(t *testing.T, name string) *AhoCorasick {
	t.Helper()
	mr := miniredis.RunT(t)
	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: name, SchemaVersion: SchemaV1})
	if err != nil {
		t.Fatalf("Create V1: %v", err)
	}
	t.Cleanup(func() { _ = ac.Close() })
	return ac
}

// TestV1WritesAreClosed covers every public write path, not just Add. Batch writes
// reach ac.ops.add and ac.ops.remove through batchAddFns and batchRemoveFns, so this
// is what proves one guard on v1Operations closes all of them — and that a future
// caller cannot reopen a V1 write by taking a different route.
func TestV1WritesAreClosed(t *testing.T) {
	ac := newV1ReadOnly(t, "v1-closed")

	if _, err := ac.Add("he"); !errors.Is(err, ErrV1ReadOnly) {
		t.Errorf("Add: got %v, want ErrV1ReadOnly", err)
	}
	if _, err := ac.Remove("he"); !errors.Is(err, ErrV1ReadOnly) {
		t.Errorf("Remove: got %v, want ErrV1ReadOnly", err)
	}
	// Batch calls report per-keyword failures in result.Failed and reserve the
	// returned error for whole-batch problems, so that is where the refusal lands.
	added, err := ac.AddMany([]string{"he", "she"}, nil)
	assertBatchRefused(t, "AddMany", added, err)
	removed, err := ac.RemoveMany([]string{"he", "she"}, nil)
	assertBatchRefused(t, "RemoveMany", removed, err)
}

func assertBatchRefused(t *testing.T, op string, result *BatchResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: unexpected batch error: %v", op, err)
	}
	if len(result.Added) > 0 || len(result.Removed) > 0 {
		t.Errorf("%s: applied %v/%v, want nothing", op, result.Added, result.Removed)
	}
	if len(result.Failed) != 2 {
		t.Fatalf("%s: %d failures, want 2 (%+v)", op, len(result.Failed), result)
	}
	for _, f := range result.Failed {
		if !errors.Is(f.Error, ErrV1ReadOnly) {
			t.Errorf("%s: %q failed with %v, want ErrV1ReadOnly", op, f.Keyword, f.Error)
		}
	}
}

// TestV1ReadsStillOpen is the other half of the promise: closing writes must not
// close the paths that let an existing V1 collection be read. Flush is included
// because it only deletes — it cannot create or grow a collection, so closing it
// would strand a user with V1 keys they can no longer remove.
func TestV1ReadsStillOpen(t *testing.T) {
	ac := v1Writable(t, newV1ReadOnly(t, "v1-open"))
	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	got, err := ac.Find("say hello")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(got) != 1 || got[0] != "hello" {
		t.Errorf("Find = %v, want [hello]", got)
	}

	if _, err := ac.FindIndex("say hello"); err != nil {
		t.Errorf("FindIndex: %v", err)
	}
	if _, err := ac.Suggest("hel"); err != nil {
		t.Errorf("Suggest: %v", err)
	}
	if _, err := ac.Info(); err != nil {
		t.Errorf("Info: %v", err)
	}
	if err := ac.Flush(); err != nil {
		t.Errorf("Flush: %v", err)
	}
}

// TestV1MigrationStillWorksWithClosedWrites is the escape hatch the whole milestone
// rests on: a V1 collection that predates the upgrade must still be convertible. The
// instance doing the migrating uses the production, write-closed operations.
func TestV1MigrationStillWorksWithClosedWrites(t *testing.T) {
	mr := miniredis.RunT(t)
	args := &AhoCorasickArgs{Addr: mr.Addr(), Name: "v1-migrate", SchemaVersion: SchemaV1}

	seed, err := Create(args)
	if err != nil {
		t.Fatalf("Create V1 seed: %v", err)
	}
	writable := v1Writable(t, seed)
	for _, kw := range []string{"he", "she", "hers"} {
		if _, addErr := writable.Add(kw); addErr != nil {
			t.Fatalf("seed %q: %v", kw, addErr)
		}
	}
	if closeErr := seed.Close(); closeErr != nil {
		t.Fatalf("close seed: %v", closeErr)
	}

	ac, err := Create(args)
	if err != nil {
		t.Fatalf("Create V1: %v", err)
	}
	defer func() { _ = ac.Close() }()

	result, err := ac.MigrateV1ToV2(nil)
	if err != nil {
		t.Fatalf("MigrateV1ToV2: %v", err)
	}
	if result.Keywords != 3 {
		t.Errorf("migrated %d keywords, want 3", result.Keywords)
	}
	if ac.SchemaVersion() != SchemaV2 {
		t.Errorf("after migration SchemaVersion = %d, want %d", ac.SchemaVersion(), SchemaV2)
	}

	// Writes are open again on the other side of the migration: that is the point.
	if _, err := ac.Add("him"); err != nil {
		t.Errorf("Add after migration: %v", err)
	}
}
