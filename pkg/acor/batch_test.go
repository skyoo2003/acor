package acor

import (
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
