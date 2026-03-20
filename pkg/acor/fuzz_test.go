package acor

import (
	"errors"
	"strings"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

func FuzzFind(f *testing.F) {
	seeds := []string{
		"hello world",
		"한글 테스트",
		"日本語テスト",
		"",
		strings.Repeat("a", 1000),
		"foo bar baz\n\t\r",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	mr, err := miniredis.Run()
	if err != nil {
		f.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:     mr.Addr(),
		Password: "",
		DB:       0,
		Name:     "test",
		Debug:    false,
	})
	if err != nil {
		f.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	_, _ = ac.Add("foo")
	_, _ = ac.Add("bar")

	f.Fuzz(func(t *testing.T, text string) {
		_, err := ac.Find(text)
		if err != nil {
			t.Errorf("Find(%q) error: %v", text, err)
		}
	})
}

func FuzzAdd(f *testing.F) {
	seeds := []string{
		"keyword",
		"한글",
		"",
		strings.Repeat("x", 1000),
		"spécial çhars",
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	mr, err := miniredis.Run()
	if err != nil {
		f.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:     mr.Addr(),
		Password: "",
		DB:       0,
		Name:     "test",
		Debug:    false,
	})
	if err != nil {
		f.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	f.Fuzz(func(t *testing.T, keyword string) {
		_, err := ac.Add(keyword)
		if err != nil && !errors.Is(err, ErrEmptyKeyword) {
			t.Logf("Add(%q) error: %v", keyword, err)
		}
	})
}
