// SPDX-License-Identifier: Apache-2.0

package acor //nolint:errcheck // memoization tests focus on engine identity

import (
	"fmt"
	"testing"
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

	o, ok := ac.ops.(*v1Operations)
	if !ok {
		t.Fatalf("expected *v1Operations, got %T", ac.ops)
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
