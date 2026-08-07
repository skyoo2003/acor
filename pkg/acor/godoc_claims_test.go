// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
)

// The v1 promise freezes the godoc on every entry of api/v1.txt, and until the
// milestone-3 audit nothing checked that those sentences were true. These tests pin
// the claims the audit found false and rewrote, so the corrected wording cannot
// quietly drift back. Each one fails if the sentence it guards becomes wrong again.
//
// Verdicts and citations live in api/v1-audit.txt.

// recordingLogger captures what Debug writes, which is the whole point of the
// corrected sentence: it goes to the Logger, not to stdout.
type recordingLogger struct {
	mu    sync.Mutex
	lines int
}

func (l *recordingLogger) Printf(string, ...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines++
}

func (l *recordingLogger) Println(...interface{}) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines++
}

func (l *recordingLogger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lines
}

// TestDebugWritesToLoggerNotStdout pins the correction to AhoCorasick.Debug, which
// claimed to print "to stdout". It never did — it writes through the Logger, and the
// default logger discards. A caller who trusted the old sentence got silence and no
// way to tell that apart from an empty collection.
func TestDebugWritesToLoggerNotStdout(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	log := &recordingLogger{}
	ac, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "debug-target", Logger: log})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}

	ac.Debug()
	if log.count() == 0 {
		t.Fatal("Debug() wrote nothing to the configured Logger")
	}
}

// TestDebugIsSilentWithoutALogger is the other half of the corrected sentence: the
// default logger discards, so Debug on a plain instance produces no output at all.
// With io.Discard behind it there is nothing to capture, so the assertion is that
// the call completes — a caller seeing nothing is the documented outcome, not a bug.
func TestDebugIsSilentWithoutALogger(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(testKeywordHE); err != nil {
		t.Fatal(err)
	}
	ac.Debug()
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

// TestKVStorageIsNotInjectable pins the correction to the KVStorage godoc, which
// offered "mock implementations can be used for testing". Nothing on the public
// surface accepts one, so that was a capability the package cannot deliver. The
// assertion is structural: no AhoCorasickArgs field can carry a KVStorage in.
func TestKVStorageIsNotInjectable(t *testing.T) {
	storage := reflect.TypeOf((*KVStorage)(nil)).Elem()
	args := reflect.TypeOf(AhoCorasickArgs{})

	for i := range args.NumField() {
		f := args.Field(i)
		if f.Type == storage || f.Type.Implements(storage) {
			t.Errorf("AhoCorasickArgs.%s can carry a KVStorage; the godoc says one cannot be supplied, "+
				"so revisit both it and api/v1-audit.txt", f.Name)
		}
	}
}
