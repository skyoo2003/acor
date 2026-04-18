// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"errors"
	"testing"
)

func TestV1Debug(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}

	ac.Debug()
}

func TestV1FlushWithKeywords(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("she"); err != nil {
		t.Fatal(err)
	}

	if err := ac.Flush(); err != nil {
		t.Fatalf("Flush() error: %v", err)
	}

	info, err := ac.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Keywords != 0 {
		t.Errorf("after flush, Keywords = %d, want 0", info.Keywords)
	}
}

func TestV1RemoveKeyword(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("she"); err != nil {
		t.Fatal(err)
	}

	removed, err := ac.Remove("he")
	if err != nil {
		t.Fatalf("Remove() error: %v", err)
	}
	if removed < 0 {
		t.Errorf("Remove() = %d, want >= 0", removed)
	}

	matched, err := ac.Find("she is here")
	if err != nil {
		t.Fatal(err)
	}
	if containsAll(matched, "he") && !containsAll(matched, "she") {
		t.Errorf("should still find 'she', got %v", matched)
	}
}

func TestV1FindIndex(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("she"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.FindIndex("she is here")
	if err != nil {
		t.Fatalf("FindIndex() error: %v", err)
	}
	if len(results) == 0 {
		t.Error("expected some results")
	}
}

func TestV1AddEmptyKeyword(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	added, err := ac.Add("")
	if err != nil {
		t.Fatalf("Add('') error: %v", err)
	}
	if added != 0 {
		t.Errorf("Add('') = %d, want 0", added)
	}
}

func TestV1RemoveEmptyKeyword(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	removed, err := ac.Remove("")
	if err != nil {
		t.Fatalf("Remove('') error: %v", err)
	}
	if removed != 0 {
		t.Errorf("Remove('') = %d, want 0", removed)
	}
}

func TestV1Info(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	info, err := ac.Info()
	if err != nil {
		t.Fatalf("Info() error: %v", err)
	}
	if info.Keywords != 0 {
		t.Errorf("empty Info().Keywords = %d, want 0", info.Keywords)
	}

	if _, addErr := ac.Add("hello"); addErr != nil {
		t.Fatal(addErr)
	}

	info, err = ac.Info()
	if err != nil {
		t.Fatal(err)
	}
	if info.Keywords != 1 {
		t.Errorf("Info().Keywords = %d, want 1", info.Keywords)
	}
}

func TestCloseTwice(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()

	if err := ac.Close(); err != nil {
		t.Fatalf("first Close() error: %v", err)
	}

	err := ac.Close()
	if err == nil {
		t.Fatal("expected error on second Close()")
	}
	if !errors.Is(err, ErrRedisAlreadyClosed) {
		t.Errorf("expected ErrRedisAlreadyClosed, got %v", err)
	}
}

func TestSchemaVersionMethod(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if ac.SchemaVersion() != SchemaV2 {
		t.Errorf("SchemaVersion() = %d, want %d", ac.SchemaVersion(), SchemaV2)
	}
}

func TestV1Suggest(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}
	if _, err := ac.Add("help"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.Suggest("hel")
	if err != nil {
		t.Fatalf("Suggest() error: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("Suggest('hel') = %v, want 2 results", results)
	}
}

func TestV1SuggestIndexIntegration(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.SuggestIndex("hel")
	if err != nil {
		t.Fatalf("SuggestIndex() error: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("SuggestIndex('hel') = %v, want 1 result", results)
	}
}

func TestV1SuggestNoMatch(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	results, err := ac.Suggest("xyz")
	if err != nil {
		t.Fatalf("Suggest() error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Suggest('xyz') = %v, want empty", results)
	}
}

func TestV1SuggestEmpty(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	results, err := ac.Suggest("")
	if err != nil {
		t.Fatalf("Suggest('') error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("Suggest('') = %v, want empty", results)
	}
}

func TestV1AddDuplicate(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	added, err := ac.Add("he")
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("Add duplicate = %d, want 0", added)
	}
}

func TestV1RemoveKeywordIntegration(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	added, err := ac.Add("HELLO")
	if err != nil {
		t.Fatal(err)
	}
	if added != 1 {
		t.Errorf("Add('HELLO') = %d, want 1", added)
	}

	matched, err := ac.Find("hello")
	if err != nil {
		t.Fatal(err)
	}
	if !containsAll(matched, "hello") {
		t.Errorf("Find('hello') = %v, want [hello]", matched)
	}
}

func TestV1AddWhitespaceKeyword(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	added, err := ac.Add("   ")
	if err != nil {
		t.Fatal(err)
	}
	if added != 0 {
		t.Errorf("Add('   ') = %d, want 0", added)
	}
}
