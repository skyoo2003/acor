// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"io"
	"log"
	"reflect"
	"testing"
)

// The v1 promise freezes the godoc on every entry of api/v1.txt, and until the
// milestone-3 audit nothing checked that those sentences were true. These tests pin
// the claims the audit found false and rewrote, so the corrected wording cannot
// quietly drift back. Each one fails if the sentence it guards becomes wrong again.
//
// Verdicts and citations live in api/v1-audit.txt.

// Debug's output is captured with recordingLogger from batch_rollback_test.go, which
// already records every line the Logger is handed.

// TestDebugWritesToLoggerNotStdout pins the correction to AhoCorasick.Debug, which
// claimed to print "to stdout". It never did — it writes through the Logger, and the
// default logger discards. A caller who trusted the old sentence got silence and no
// way to tell that apart from an empty collection.
func TestDebugWritesToLoggerNotStdout(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	rec := &recordingLogger{}
	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "debug-target", Logger: rec})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}

	ac.Debug()
	if len(rec.recorded()) == 0 {
		t.Fatal("Debug() wrote nothing to the configured Logger")
	}
}

// TestDebugIsSilentWithoutALogger is the other half of the corrected sentence: the
// default logger discards, so Debug on a plain instance produces no output at all.
// Asserting on the sink rather than on captured bytes is what makes this a guard —
// "the call completed" would still pass if Debug started writing to stdout, which is
// exactly the claim being pinned. Swapping os.Stdout to capture it would race every
// other test in the package.
func TestDebugIsSilentWithoutALogger(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}
	ac.Debug()

	std, ok := ac.logger.(*log.Logger)
	if !ok {
		t.Fatalf("default logger is %T, want the *log.Logger newLogger builds", ac.logger)
	}
	if std.Writer() != io.Discard {
		t.Error("the default logger does not discard; Debug's godoc says it produces no output at all")
	}
}

// TestDebugIsIgnoredWhenALoggerIsSet pins the correction to AhoCorasickArgs.Debug
// and .Logger. Debug redirects the default logger to stdout, but newLogger returns
// a custom Logger before the default is ever used (acor.go:490), so setting both
// silently drops Debug. Neither field said so.
func TestDebugIsIgnoredWhenALoggerIsSet(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	custom := &recordingLogger{}
	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "debug-and-logger",
		Debug: true, Logger: custom, MaxRetries: -1, PoolSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}
	ac.Debug()

	// Everything went to the custom Logger. Had Debug won, or added a second sink,
	// this logger would not be the only one receiving.
	if len(custom.recorded()) == 0 {
		t.Error("the custom Logger received nothing; Debug must not replace or bypass it")
	}
}

// TestInfoCarriesNoSchemaVersion pins the correction to AhoCorasick.Info, whose doc
// promised "the schema version" among what it returns. AhoCorasickInfo has no such
// field and never did; SchemaVersion is the call that answers it, without Redis I/O.
func TestInfoCarriesNoSchemaVersion(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	info, err := ac.Info()
	if err != nil {
		t.Fatal(err)
	}
	if _, found := reflect.TypeOf(*info).FieldByName("SchemaVersion"); found {
		t.Error("AhoCorasickInfo gained a SchemaVersion field; Info's godoc says it has none")
	}
	if got := ac.SchemaVersion(); got != SchemaV2 {
		t.Errorf("SchemaVersion() = %d, want %d — the documented way to get it", got, SchemaV2)
	}
}

// TestEmptyKeywordIsNotAnErrorOutsideBatch pins the correction to ErrEmptyKeyword,
// which claimed to be "returned when an empty or whitespace-only keyword is
// provided" without qualification. The single-keyword API reports (0, nil); only the
// batch forms surface the sentinel, so a caller who wrote errors.Is around Add was
// waiting for an error that never comes.
func TestEmptyKeywordIsNotAnErrorOutsideBatch(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	for _, kw := range []string{"", "   ", "\t\n"} {
		if n, err := ac.Add(kw); n != 0 || err != nil {
			t.Errorf("Add(%q) = (%d, %v), want (0, nil)", kw, n, err)
		}
		if n, err := ac.Remove(kw); n != 0 || err != nil {
			t.Errorf("Remove(%q) = (%d, %v), want (0, nil)", kw, n, err)
		}
	}

	// The batch form is where the sentinel does fire, which is what makes the
	// distinction worth documenting rather than deleting.
	_, err := ac.AddMany([]string{testKeywordHE, ""}, &BatchOptions{Mode: BatchModeTransactional})
	if !errors.Is(err, ErrEmptyKeyword) {
		t.Errorf("AddMany transactional with an empty keyword = %v, want ErrEmptyKeyword", err)
	}
}

// TestConflictSurfacesOnlyAfterRetriesAreSpent pins the correction to
// ErrConcurrencyConflict, which said it is returned "when an optimistic locking
// conflict occurs" and told the caller to retry. Both misled: a single lost race is
// retried internally, so the error escapes only once the whole ramp is spent, and by
// then an immediate caller-side retry is the least useful response.
func TestConflictSurfacesOnlyAfterRetriesAreSpent(t *testing.T) {
	// One conflict then success: the caller must never see the conflict.
	attempts := 0
	n, err := retryOnConflict(context.Background(), func() (int, error) {
		attempts++
		if attempts == 1 {
			return 0, ErrConcurrencyConflict
		}
		return 1, nil
	})
	if err != nil || n != 1 {
		t.Fatalf("a single lost race surfaced: got (%d, %v), want (1, nil)", n, err)
	}
	if attempts != 2 {
		t.Errorf("attempts = %d, want 2 — the internal retry the doc now describes", attempts)
	}

	// Conflicting throughout: only now does it escape, and only after maxRetries.
	attempts = 0
	_, err = retryOnConflict(context.Background(), func() (int, error) {
		attempts++
		return 0, ErrConcurrencyConflict
	})
	if !errors.Is(err, ErrConcurrencyConflict) {
		t.Fatalf("persistent conflict returned %v, want ErrConcurrencyConflict", err)
	}
	if attempts != maxRetries {
		t.Errorf("attempts = %d, want maxRetries (%d) before giving up", attempts, maxRetries)
	}
}

// TestParallelOptionsHaveNoImpliedDefaults pins the correction to
// ParallelOptions.ChunkSize and .Overlap, both of which claimed to default to the
// Default* constants. normalizeParallelOptions fills in neither: only
// DefaultParallelOptions supplies them. The ChunkSize half is the sharp one — the
// documented "defaults to 1000" was in fact an error return.
func TestParallelOptionsHaveNoImpliedDefaults(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}

	// A caller-built struct leaving ChunkSize unset fails rather than falling back.
	if _, err := ac.FindParallel("she", &ParallelOptions{Workers: 2}); !errors.Is(err, ErrInvalidChunkSize) {
		t.Errorf("FindParallel with an unset ChunkSize = %v, want ErrInvalidChunkSize", err)
	}

	// Overlap is left at zero rather than raised to DefaultOverlap. Asserting on the
	// struct the library normalized is what shows the field is untouched; asserting
	// on a missed match would depend on the dictionary instead.
	opts := &ParallelOptions{ChunkSize: 100}
	normalizeParallelOptions(opts)
	if opts.Overlap != 0 {
		t.Errorf("normalizeParallelOptions set Overlap to %d, want 0 — only DefaultParallelOptions supplies %d",
			opts.Overlap, DefaultOverlap)
	}
	if opts.ChunkSize != 100 {
		t.Errorf("normalizeParallelOptions changed ChunkSize to %d, want the caller's 100", opts.ChunkSize)
	}

	// DefaultParallelOptions is the one place the documented values come from.
	if d := DefaultParallelOptions(); d.ChunkSize != DefaultChunkSize || d.Overlap != DefaultOverlap ||
		d.Boundary != ChunkBoundaryWord {
		t.Errorf("DefaultParallelOptions = %+v, want ChunkSize %d, Overlap %d, ChunkBoundaryWord",
			d, DefaultChunkSize, DefaultOverlap)
	}
}

// TestBatchModeZeroValueIsBestEffort pins the correction to BatchOptions.Mode and
// BatchModeBestEffort, which both described Mode as defaulting "if nil". Mode is an
// int; it cannot be nil. Unset means the zero value, and a nil *BatchOptions is the
// separate case the old wording conflated it with.
func TestBatchModeZeroValueIsBestEffort(t *testing.T) {
	if BatchModeBestEffort != 0 {
		t.Fatalf("BatchModeBestEffort = %d, want 0 — the doc rests on it being the zero value", BatchModeBestEffort)
	}
	var unset BatchOptions
	if unset.Mode != BatchModeBestEffort {
		t.Errorf("an unset BatchOptions.Mode = %d, want BatchModeBestEffort", unset.Mode)
	}

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	// Nil options take best-effort too: an empty keyword is recorded, not returned,
	// which is the observable difference between the two modes.
	res, err := ac.AddMany([]string{testKeywordHE, ""}, nil)
	if err != nil {
		t.Fatalf("AddMany with nil options returned %v, want best-effort partial results", err)
	}
	if len(res.Failed) != 1 || !errors.Is(res.Failed[0].Error, ErrEmptyKeyword) {
		t.Errorf("Failed = %+v, want the empty keyword recorded rather than returned", res.Failed)
	}
}

// TestOneAddressInAddrsStillMeansCluster pins the correction to the
// AhoCorasickArgs topology list, which said cluster is chosen when "Addrs has
// multiple entries". client.go:40 asks only whether Addrs is non-empty, so a
// one-element list is a cluster client — and DB selection, which cluster does not
// have, is the observable proof.
func TestOneAddressInAddrsStillMeansCluster(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	_, err := Create(&AhoCorasickArgs{Addrs: []string{mr.Addr()}, DB: 1, Name: "one-addr",
		MaxRetries: -1, PoolSize: 1})
	if !errors.Is(err, ErrRedisClusterDB) {
		t.Errorf("a single-element Addrs with DB=1 = %v, want ErrRedisClusterDB — one address selects cluster", err)
	}

	// The same address in Addr is standalone, where DB is honored. This is the
	// contrast the corrected sentence rests on.
	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), DB: 1, Name: "one-addr", MaxRetries: -1, PoolSize: 1})
	if err != nil {
		t.Fatalf("Addr with DB=1 = %v, want success — standalone supports DB selection", err)
	}
	_ = ac.Close()
}

// TestAddrIsRejectedWithAddrsAndIgnoredWithRing pins the correction to
// AhoCorasickArgs.Addr, which claimed to be "Ignored if Addrs or RingAddrs is
// set". Only the RingAddrs half was true; the Addrs half is a Create error
// (client.go:46-48), which is the opposite of being ignored.
func TestAddrIsRejectedWithAddrsAndIgnoredWithRing(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	_, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Addrs: []string{mr.Addr()}, Name: "both"})
	if !errors.Is(err, ErrRedisConflictingTopology) {
		t.Errorf("Addr with Addrs = %v, want ErrRedisConflictingTopology rather than Addr being ignored", err)
	}

	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), RingAddrs: map[string]string{"s": mr.Addr()},
		Name: "ring", MaxRetries: -1, PoolSize: 1})
	if err != nil {
		t.Fatalf("Addr with RingAddrs = %v, want success with Addr ignored", err)
	}
	_ = ac.Close()
}

// TestAddrsWithRingAddrsIsRejectedAtEitherArity is the other half of "one address
// is still cluster". Addrs beside RingAddrs asks for two topologies, so it is
// ErrRedisConflictingTopology however long the list is. The guard used to test the
// merged address list for len > 1, which let a one-element Addrs through and built
// a ring client against shards the caller never named.
func TestAddrsWithRingAddrsIsRejectedAtEitherArity(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ring := map[string]string{"s": mr.Addr()}
	for _, addrs := range [][]string{{mr.Addr()}, {mr.Addr(), mr.Addr() + "9"}} {
		_, err := Create(&AhoCorasickArgs{Addrs: addrs, RingAddrs: ring, Name: "ring-and-addrs"})
		if !errors.Is(err, ErrRedisConflictingTopology) {
			t.Errorf("Addrs %v with RingAddrs = %v, want ErrRedisConflictingTopology", addrs, err)
		}
	}
}

// TestPresetStringNamesTheSentinel pins the correction to Preset.String, which
// promised "Unknown" for any value outside the set. Preset(-1) is the internal
// unset sentinel and reports "Default" (preset.go:41-42), so it is the one
// reachable exception.
func TestPresetStringNamesTheSentinel(t *testing.T) {
	for p, want := range map[Preset]string{
		PresetNone: "None", PresetSpeed: "Speed", PresetBalanced: "Balanced",
		PresetMemoryEfficient: "MemoryEfficient",
		Preset(-1):            "Default", Preset(99): "Unknown",
	} {
		if got := p.String(); got != want {
			t.Errorf("Preset(%d).String() = %q, want %q", p, got, want)
		}
	}
}

// TestV1FlushIgnoresItsContext pins the correction to FlushContext, whose doc
// offered "cancellation and timeout propagation" without qualification. V1's
// flush discards ctx and runs on a fresh RollbackTimeout-bounded one
// (v1_ops.go:219-225), so a canceled context flushes the collection anyway.
func TestV1FlushIgnoresItsContext(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	for _, kw := range []string{testKeywordHE, testKeywordHim} {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}

	dead, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ac.FlushContext(dead); err != nil {
		t.Fatalf("FlushContext with a canceled context = %v, want nil — V1 ignores ctx", err)
	}

	info, err := ac.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Keywords != 0 {
		t.Errorf("Keywords after a canceled FlushContext = %d, want 0 — the flush ran regardless", info.Keywords)
	}
}

// TestV2NeverWritesTheNodesKey pins the correction to SchemaV2 and to
// MigrationResult.KeysAfter, both of which said V2 is "3 Redis keys". Only
// MigrateV1ToV2 writes {name}:nodes (keys.go:54 has no other writer), so a
// natively built collection tops out at two.
func TestV2NeverWritesTheNodesKey(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if got := len(mr.Keys()); got != 1 {
		t.Errorf("a fresh V2 collection holds %d keys (%v), want 1 — only :trie", got, mr.Keys())
	}

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}
	for _, key := range mr.Keys() {
		if key == nodesKey("test") {
			t.Errorf("Add wrote %s; the doc says only MigrateV1ToV2 creates it", key)
		}
	}
	if got := len(mr.Keys()); got != 2 {
		t.Errorf("V2 after one Add holds %d keys (%v), want 2 — :trie and :outputs", got, mr.Keys())
	}
}

// TestKVStorageIsNotInjectable pins the correction to the kvStorage godoc, which
// offered "mock implementations can be used for testing". Nothing on the public
// surface accepts one, so that was a capability the package cannot deliver. The
// assertion is structural: no AhoCorasickArgs field can carry a kvStorage in.
func TestKVStorageIsNotInjectable(t *testing.T) {
	storage := reflect.TypeOf((*kvStorage)(nil)).Elem()
	args := reflect.TypeOf(AhoCorasickArgs{})

	for i := range args.NumField() {
		f := args.Field(i)
		if f.Type == storage || f.Type.Implements(storage) {
			t.Errorf("AhoCorasickArgs.%s can carry a kvStorage; the godoc says one cannot be supplied, "+
				"so revisit both it and api/v1-audit.txt", f.Name)
		}
	}
}

// seedV1 fills a fixture V1 collection with keywords whose prefixes overlap, so
// the gap between the counted keys and MigrationResult.KeysBefore is visible.
func seedV1(t *testing.T, ac *AhoCorasick) {
	t.Helper()
	for _, kw := range []string{"he", "her", "hers", "his", "she"} {
		if _, err := ac.Add(kw); err != nil {
			t.Fatal(err)
		}
	}
}

// TestMigrationResultCountsAreEstimates pins the corrections to KeysBefore,
// KeysAfter, RolledBack, and Progress. Three of the four were stated as counts of
// something real; none of them is one.
func TestMigrationResultCountsAreEstimates(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	seedV1(t, ac)

	actualV1Keys := len(mr.Keys())

	calls := 0
	dry, err := ac.MigrateV1ToV2(&MigrationOptions{DryRun: true, Progress: func(int, int, string) { calls++ }})
	if err != nil {
		t.Fatal(err)
	}
	// A dry run stops after the four collection phases, so it never reports 5/5.
	if calls != migrationTotalSteps-1 {
		t.Errorf("dry-run Progress fired %d times, want %d — the write phase never runs", calls, migrationTotalSteps-1)
	}
	if dry.KeysBefore == actualV1Keys {
		t.Errorf("KeysBefore = %d and the collection really holds %d; the doc now says it is an estimate, "+
			"so if they have been made to agree, say so there", dry.KeysBefore, actualV1Keys)
	}

	res, err := ac.MigrateV1ToV2(nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.RolledBack {
		t.Error("RolledBack is true; its doc says nothing ever sets it")
	}
	if res.KeysAfter != v2KeyCount {
		t.Errorf("KeysAfter = %d, want the constant %d — its doc says it is not a count", res.KeysAfter, v2KeyCount)
	}
	if res.DurationMs < 0 {
		t.Errorf("DurationMs = %d", res.DurationMs)
	}

	// Stats is a projection: the six size counters, none of the outcome fields.
	stats := res.Stats()
	if len(stats) != 6 {
		t.Errorf("Stats() has %d entries (%v), want the 6 its doc names", len(stats), stats)
	}
	for _, absent := range []string{"status", "duration_ms", "rolled_back", "dry_run"} {
		if _, found := stats[absent]; found {
			t.Errorf("Stats() carries %q; its doc says the outcome fields are not in it", absent)
		}
	}
}

// TestRollbackToV1LeavesTheCollectionReadOnly pins the correction to
// RollbackToV1, whose only stated cost was losing keywords added after the
// migration. The larger one is that V1 takes no writes, so the collection cannot
// be used afterwards — a caller reaching for rollback as a safety valve needs to
// know it is a one-way door.
func TestRollbackToV1LeavesTheCollectionReadOnly(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	seedV1(t, ac)

	if _, err := ac.MigrateV1ToV2(&MigrationOptions{KeepOldKeys: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("post-migration"); err != nil {
		t.Fatalf("Add after migrating = %v, want success — V2 is writable", err)
	}

	if err := ac.RollbackToV1(); err != nil {
		t.Fatalf("RollbackToV1 with KeepOldKeys = %v, want success", err)
	}
	if got := ac.SchemaVersion(); got != SchemaV1 {
		t.Errorf("SchemaVersion after rollback = %d, want %d", got, SchemaV1)
	}
	if _, err := ac.Add("after-rollback"); !errors.Is(err, ErrV1ReadOnly) {
		t.Errorf("Add after rollback = %v, want ErrV1ReadOnly — the collection is read-only again", err)
	}
}

// TestBatchDuplicatesAreJudgedNormalized pins the correction to AddMany, whose
// "duplicate keywords ... are skipped" read as exact duplicates. Screening is on
// the normalized form (batch.go:100-104), so case-insensitive collections treat
// two spellings as one keyword.
func TestBatchDuplicatesAreJudgedNormalized(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	res, err := ac.AddMany([]string{testKeywordHelloUpper, testKeywordHello}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Added) != 1 || len(res.Skipped) != 1 {
		t.Errorf("AddMany(%q, %q) = Added %v, Skipped %v; want one of each — they normalize to one keyword",
			testKeywordHelloUpper, testKeywordHello, res.Added, res.Skipped)
	}

	// Transactional failure returns no result at all, which is the other half of
	// the corrected paragraph.
	out, err := ac.AddMany([]string{testKeywordTest, ""}, &BatchOptions{Mode: BatchModeTransactional})
	if err == nil || out != nil {
		t.Errorf("transactional AddMany with a blank = (%v, %v), want (nil, error)", out, err)
	}
}

// TestSuggestIsUnavailableInPresetMode pins the sentence added to SuggestContext
// and SuggestIndexContext. Preset mode has no prefix index and refuses rather
// than quietly falling back to Redis.
func TestSuggestIsUnavailableInPresetMode(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "preset-suggest", Preset: PresetBalanced,
		MaxRetries: -1, PoolSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.SuggestContext(context.Background(), "he"); !errors.Is(err, ErrSuggestRequiresRedis) {
		t.Errorf("SuggestContext in preset mode = %v, want ErrSuggestRequiresRedis", err)
	}
	if _, err := ac.SuggestIndexContext(context.Background(), "he"); !errors.Is(err, ErrSuggestRequiresRedis) {
		t.Errorf("SuggestIndexContext in preset mode = %v, want ErrSuggestRequiresRedis", err)
	}

	// InfoContext reads the local engine, so a canceled context is not an error.
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ac.InfoContext(dead); err != nil {
		t.Errorf("InfoContext with a canceled context = %v, want nil — preset Info does no Redis I/O", err)
	}
}
