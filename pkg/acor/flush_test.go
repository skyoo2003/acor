package acor

import (
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

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

	if _, err := ac.Add("hello"); err != nil {
		t.Fatal(err)
	}

	if _, err := ac.Find("hello world"); err != nil {
		t.Fatal(err)
	}
	_, _, valid := ac.cache.get()
	if !valid {
		t.Fatal("expected cache to be valid after Find")
	}

	if err := ac.Flush(); err != nil {
		t.Fatal(err)
	}

	_, _, valid = ac.cache.get()
	if valid {
		t.Error("expected cache to be invalidated after Flush")
	}
}
