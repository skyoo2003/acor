package acor

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
)

// TestInitAndFlushAndClose exercises the init/flush/close lifecycle without caching.
func TestInitAndFlushAndClose(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	if err := ac.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestCache_FlushInvalidatesCache(t *testing.T) {
	mr := miniredis.RunT(t)

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-cache-flush",
		EnableCache: true,
	})
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}
	defer func() { _ = ac.Close() }()

	if _, addErr := ac.Add("hello"); addErr != nil {
		t.Fatal(addErr)
	}

	if _, findErr := ac.Find("hello world"); findErr != nil {
		t.Fatal(findErr)
	}
	_, _, valid := ac.cache.get()
	if !valid {
		t.Fatal("expected cache to be valid after Find")
	}

	if flushErr := ac.Flush(); flushErr != nil {
		t.Fatal(flushErr)
	}

	_, _, valid = ac.cache.get()
	if valid {
		t.Error("expected cache to be invalidated after Flush")
	}

	results, findErr := ac.Find("hello world")
	if findErr != nil {
		t.Fatal(findErr)
	}
	if len(results) != 0 {
		t.Fatalf("expected no matches after Flush, got %v", results)
	}
}
