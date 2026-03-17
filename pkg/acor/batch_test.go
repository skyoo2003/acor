package acor

import (
	"errors"
	"testing"
)

func TestAddManyBestEffort(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "her", "him"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Added) != 3 {
		t.Errorf("expected 3 added, got %d", len(result.Added))
	}
	if len(result.Failed) != 0 {
		t.Errorf("expected 0 failed, got %d", len(result.Failed))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
}

func TestAddManyBestEffortWithDuplicates(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "he", "her"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Added) != 2 {
		t.Errorf("expected 2 added, got %d", len(result.Added))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

func TestAddManyTransactional(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "her", "him"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeTransactional})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Added) != 3 {
		t.Errorf("expected 3 added, got %d", len(result.Added))
	}
}

func TestAddManyTransactionalWithDuplicates(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "he", "her"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeTransactional})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Added) != 2 {
		t.Errorf("expected 2 added, got %d", len(result.Added))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}

func TestAddManyTransactionalRollbackOnError(t *testing.T) {
	ac, mr := createAhoCorasickV1(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	ac.buildTrieHook = func(prefix string) error {
		if prefix == "her" {
			return errors.New("forced failure")
		}
		return nil
	}
	defer func() { ac.buildTrieHook = nil }()

	keywords := []string{"he", "her", "him"}
	_, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeTransactional})
	if err == nil {
		t.Fatal("expected error on transactional batch")
	}

	ac.buildTrieHook = nil

	results, err := ac.Find("he")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected rollback to remove all keywords, found %d", len(results))
	}
}

func TestAddManyTransactionalRollbackOnEmpty(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("existing"); err != nil {
		t.Fatal(err)
	}

	keywords := []string{"he", "", "him"}
	_, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeTransactional})
	if !errors.Is(err, ErrEmptyKeyword) {
		t.Fatalf("expected ErrEmptyKeyword, got %v", err)
	}

	results, err := ac.Find("he")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected rollback to remove he, found %d", len(results))
	}

	results, err = ac.Find("existing")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != "existing" {
		t.Error("expected 'existing' to remain after rollback")
	}
}

func TestRemoveMany(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
		t.Fatal(err)
	}

	result, err := ac.RemoveMany([]string{"he", "her"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(result.Removed))
	}

	results, err := ac.Find("him")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != "him" {
		t.Error("expected 'him' to remain")
	}
}

func TestRemoveManyWithDuplicates(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
		t.Fatal(err)
	}

	result, err := ac.RemoveMany([]string{"he", "he", "her"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Removed) != 2 {
		t.Errorf("expected 2 removed, got %d", len(result.Removed))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped duplicate, got %d", len(result.Skipped))
	}
}

func TestFindMany(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
		t.Fatal(err)
	}

	texts := []string{"he is here", "him and her", "nothing"}
	results, err := ac.FindMany(texts)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if len(results["he is here"]) < 2 {
		t.Errorf("expected at least 2 matches in 'he is here', got %d", len(results["he is here"]))
	}
}
