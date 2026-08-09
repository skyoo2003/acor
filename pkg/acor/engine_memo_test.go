// SPDX-License-Identifier: Apache-2.0

package acor //nolint:errcheck // memoization tests focus on engine identity

import (
	"errors"
	"fmt"
	"testing"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// The memo skips rebuilding the automaton when the fetched data is unchanged.
// It must never skip noticing that the data *did* change: without EnableCache
// there is no invalidation listener, so correctness rests entirely on the fetch
// still happening every read and the digest actually tracking the payload.

func v2Ops(t *testing.T, ac *AhoCorasick) *v2Operations {
	t.Helper()
	o, ok := ac.ops.(*v2Operations)
	if !ok {
		t.Fatalf("expected *v2Operations, got %T", ac.ops)
	}
	return o
}

// TestV2UncachedEngineIsMemoized pins the fix: repeated reads over unchanged
// data reuse one automaton instead of rebuilding it per call.
func TestV2UncachedEngineIsMemoized(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	for _, kw := range []string{"he", "her", "him"} {
		if _, err := ac.Add(kw); err != nil {
			t.Fatalf("Add(%q) error: %v", kw, err)
		}
	}

	o := v2Ops(t, ac)
	if o.cache != nil {
		t.Fatal("expected the uncached path; this test is meaningless with EnableCache")
	}

	first, err := o.loadEngine(ac.ctx)
	if err != nil {
		t.Fatalf("loadEngine() error: %v", err)
	}
	second, err := o.loadEngine(ac.ctx)
	if err != nil {
		t.Fatalf("loadEngine() error: %v", err)
	}

	if first != second {
		t.Fatal("uncached V2 rebuilt the engine for unchanged data; the memo is not working")
	}
}

// TestV2UncachedEngineRebuildsOnChange is the half that matters for
// correctness. A memo that never invalidates would pass the test above and
// serve stale results forever.
func TestV2UncachedEngineRebuildsOnChange(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	if _, err := ac.Add("he"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	o := v2Ops(t, ac)
	before, err := o.loadEngine(ac.ctx)
	if err != nil {
		t.Fatalf("loadEngine() error: %v", err)
	}

	if _, addErr := ac.Add("him"); addErr != nil {
		t.Fatalf("Add() error: %v", addErr)
	}

	after, err := o.loadEngine(ac.ctx)
	if err != nil {
		t.Fatalf("loadEngine() error: %v", err)
	}

	if before == after {
		t.Fatal("uncached V2 reused a stale engine after the collection changed")
	}

	matched, err := ac.Find("he is him")
	if err != nil {
		t.Fatalf("Find() error: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("Find() = %v, want both 'he' and 'him'; memoization dropped a keyword", matched)
	}
}

// TestV2UncachedSeesExternalWrites guards the case the memo could plausibly
// break: another instance writes to the same collection, and this instance must
// still notice on its next read. There is no Pub/Sub listener on the uncached
// path, so this works only because the fetch is not memoized alongside the
// rebuild.
func TestV2UncachedSeesExternalWrites(t *testing.T) {
	mr := createTestRedisServer(t)
	defer mr.Close()

	reader, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "shared"})
	if err != nil {
		t.Fatalf("Create(reader) error: %v", err)
	}
	defer reader.Close()

	writer, err := Create(&AhoCorasickArgs{Addr: mr.Addr(), Name: "shared"})
	if err != nil {
		t.Fatalf("Create(writer) error: %v", err)
	}
	defer writer.Close()

	if _, addErr := writer.Add("alpha"); addErr != nil {
		t.Fatalf("writer.Add() error: %v", addErr)
	}
	if matched, findErr := reader.Find("alpha beta"); findErr != nil {
		t.Fatalf("reader.Find() error: %v", findErr)
	} else if len(matched) != 1 {
		t.Fatalf("reader.Find() = %v, want [alpha]", matched)
	}

	// Second instance writes; the reader's memo must not mask it.
	if _, addErr := writer.Add("beta"); addErr != nil {
		t.Fatalf("writer.Add() error: %v", addErr)
	}
	matched, err := reader.Find("alpha beta")
	if err != nil {
		t.Fatalf("reader.Find() error: %v", err)
	}
	if len(matched) != 2 {
		t.Fatalf("reader.Find() = %v, want both keywords; the memo masked a peer's write", matched)
	}
}

// TestDigestRawOutputsDetectsChanges checks the digest directly, since every
// staleness guarantee above reduces to it. Inputs are raw Redis hash values —
// per-state JSON arrays, unparsed.
func TestDigestRawOutputsDetectsChanges(t *testing.T) {
	base := map[string]string{"1": `["he"]`, "2": `["him"]`}

	cases := []struct {
		name string
		raw  map[string]string
		same bool
	}{
		{"identical", map[string]string{"1": `["he"]`, "2": `["him"]`}, true},
		{"reordered states", map[string]string{"2": `["him"]`, "1": `["he"]`}, true},
		{"added state", map[string]string{"1": `["he"]`, "2": `["him"]`, "3": `["her"]`}, false},
		{"removed state", map[string]string{"1": `["he"]`}, false},
		{"changed keyword", map[string]string{"1": `["he"]`, "2": `["her"]`}, false},
		{"keyword appended to a state", map[string]string{"1": `["he"]`, "2": `["him","her"]`}, false},
	}

	want := digestRawOutputs(base)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := digestRawOutputs(tc.raw)
			if (got == want) != tc.same {
				t.Fatalf("digestRawOutputs(%v) == digestRawOutputs(base) is %v, want %v", tc.raw, got == want, tc.same)
			}
		})
	}
}

// TestEngineMemoPropagatesBuildError pins the failure behavior of engineFor.
//
// The build callback can fail for real: the V2 uncached path parses JSON inside
// it. Two things must hold, and neither is visible through the public API —
// the error has to reach the caller rather than being swallowed into a nil
// engine, and a failed build must not be memoized, or one corrupt read would
// poison every later read of the same data.
func TestEngineMemoPropagatesBuildError(t *testing.T) {
	buildErr := errors.New("synthetic build failure")
	engineFor := func(m *engineMemo, digest uint64, e *matchengine.Engine, err error) (*matchengine.Engine, error) {
		return m.engineFor(digest, func() (*matchengine.Engine, error) { return e, err })
	}

	t.Run("error reaches the caller", func(t *testing.T) {
		var m engineMemo
		engine, err := engineFor(&m, 1, nil, buildErr)
		if !errors.Is(err, buildErr) {
			t.Fatalf("engineFor() error = %v, want %v", err, buildErr)
		}
		if engine != nil {
			t.Fatalf("engineFor() = %v, want nil engine alongside the error", engine)
		}
	})

	t.Run("failure is not memoized", func(t *testing.T) {
		var m engineMemo
		if _, err := engineFor(&m, 1, nil, buildErr); err == nil {
			t.Fatal("expected the first build to fail")
		}

		want := buildEngine(PresetBalanced, map[string]struct{}{"hello": {}})
		got, err := engineFor(&m, 1, want, nil)
		if err != nil {
			t.Fatalf("engineFor() after a failed build = %v, want the retry to succeed", err)
		}
		if got != want {
			t.Fatal("engineFor() did not memoize the successful retry for a digest that previously failed")
		}
	})

	t.Run("a failed build leaves the previous engine intact", func(t *testing.T) {
		var m engineMemo
		first := buildEngine(PresetBalanced, map[string]struct{}{"hello": {}})
		if _, err := engineFor(&m, 1, first, nil); err != nil {
			t.Fatalf("engineFor() error: %v", err)
		}

		// A different digest fails to build.
		if _, err := engineFor(&m, 2, nil, buildErr); !errors.Is(err, buildErr) {
			t.Fatalf("engineFor() error = %v, want %v", err, buildErr)
		}

		// The original digest must still be served from the memo, without
		// rebuilding — the callback below would fail the test if it ran.
		got, err := m.engineFor(1, func() (*matchengine.Engine, error) {
			t.Error("engineFor rebuilt digest 1; the failed build for digest 2 evicted a good engine")
			return nil, nil
		})
		if err != nil {
			t.Fatalf("engineFor() error: %v", err)
		}
		if got != first {
			t.Fatal("engineFor() no longer returns the engine memoized before the failed build")
		}
	})
}

// TestV2UncachedFindSurfacesParseError is the production shape of the same
// concern: unparseable data in Redis must surface as an error from Find, not as
// an empty match list that a caller would read as "no keywords present".
func TestV2UncachedFindSurfacesParseError(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}
	if _, err := ac.Find("hello world"); err != nil {
		t.Fatalf("Find() before corruption error: %v", err)
	}

	// Corrupt one state's output list behind ACOR's back.
	mr.HSet(outputsKey("test"), "1", "{not-json")

	if _, err := ac.Find("hello world"); err == nil {
		t.Fatal("Find() returned no error over unparseable outputs; a corrupt read must not look like an empty dictionary")
	}
}

// TestDigestKeywordsIsOrderIndependent pins the property V1 relies on: SMEMBERS
// makes no ordering promise.
func TestDigestKeywordsIsOrderIndependent(t *testing.T) {
	if digestKeywords([]string{"he", "her", "him"}) != digestKeywords([]string{"him", "he", "her"}) {
		t.Fatal("digestKeywords depends on order; SMEMBERS does not guarantee one")
	}
	if digestKeywords([]string{"he", "her"}) == digestKeywords([]string{"he", "her", "him"}) {
		t.Fatal("digestKeywords did not change when a keyword was added")
	}
}

// TestV1EngineStillMemoized guards the refactor: V1 shared its memo with V2 and
// must keep the behavior it already had.
func TestV1EngineStillMemoized(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer ac.Close()

	if _, err := ac.Add("he"); err != nil {
		t.Fatalf("Add() error: %v", err)
	}

	o, ok := ac.ops.(*v1WritableOps)
	if !ok {
		t.Fatalf("expected v1WritableOps, got %T", ac.ops)
	}

	first, err := o.loadEngine(ac.ctx)
	if err != nil {
		t.Fatalf("loadEngine() error: %v", err)
	}
	second, err := o.loadEngine(ac.ctx)
	if err != nil {
		t.Fatalf("loadEngine() error: %v", err)
	}
	if first != second {
		t.Fatal("V1 rebuilt the engine for an unchanged keyword set")
	}

	if _, addErr := ac.Add("him"); addErr != nil {
		t.Fatalf("Add() error: %v", addErr)
	}
	third, err := o.loadEngine(ac.ctx)
	if err != nil {
		t.Fatalf("loadEngine() error: %v", err)
	}
	if third == second {
		t.Fatal("V1 reused a stale engine after the keyword set changed")
	}
}

// TestV2UncachedConcurrentReads exercises the shared memo under -race, since
// callers now receive the same engine pointer rather than a private one.
func TestV2UncachedConcurrentReads(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer ac.Close()

	for i := range 50 {
		if _, err := ac.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			t.Fatalf("Add() error: %v", err)
		}
	}

	done := make(chan error, 8)
	for range 8 {
		go func() {
			for range 20 {
				if _, err := ac.Find("keyword7 and keyword42"); err != nil {
					done <- err
					return
				}
			}
			done <- nil
		}()
	}
	for range 8 {
		if err := <-done; err != nil {
			t.Fatalf("concurrent Find() error: %v", err)
		}
	}
}
