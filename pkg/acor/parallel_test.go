package acor

import (
	"reflect"
	"testing"
)

func TestSplitChunksByWord(t *testing.T) {
	opts := &ParallelOptions{
		ChunkSize: 10,
		Boundary:  ChunkBoundaryWord,
		Overlap:   2,
	}

	text := "hello world this is a test"
	chunks := splitChunks(text, opts)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.textOffset < 0 {
			t.Errorf("chunk %d has invalid textOffset: %d", i, c.textOffset)
		}
	}
}

func TestSplitChunksByLine(t *testing.T) {
	opts := &ParallelOptions{
		ChunkSize: 10,
		Boundary:  ChunkBoundaryLine,
		Overlap:   0,
	}

	text := "line one\nline two\nline three"
	chunks := splitChunks(text, opts)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.textOffset < 0 {
			t.Errorf("chunk %d has invalid textOffset: %d", i, c.textOffset)
		}
	}
}

func TestSplitChunksBySentence(t *testing.T) {
	opts := &ParallelOptions{
		ChunkSize: 10,
		Boundary:  ChunkBoundarySentence,
		Overlap:   0,
	}

	text := "First sentence. Second sentence! Third?"
	chunks := splitChunks(text, opts)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, c := range chunks {
		if c.textOffset < 0 {
			t.Errorf("chunk %d has invalid textOffset: %d", i, c.textOffset)
		}
	}
}

func TestFindParallel(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "her", "him", "test"}
	for _, k := range keywords {
		if _, err := ac.Add(k); err != nil {
			t.Fatal(err)
		}
	}

	text := "he is here with him. this is a test of the system."
	opts := &ParallelOptions{
		Workers:   2,
		ChunkSize: 20,
		Boundary:  ChunkBoundaryWord,
		Overlap:   3,
	}

	results, err := ac.FindParallel(text, opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Error("expected some matches")
	}
}

func TestFindParallelFallsBackToSequential(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}

	text := "test"
	results, err := ac.FindParallel(text, &ParallelOptions{ChunkSize: 100})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 || results[0] != "test" {
		t.Errorf("expected ['test'], got %v", results)
	}
}

func TestBatchAddAndParallelFind(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"apple", "application", "apply", "approach", "approximate"}
	result, err := ac.AddMany(keywords, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != len(keywords) {
		t.Errorf("expected %d added, got %d", len(keywords), len(result.Added))
	}

	text := "I want to apply for an application. The approach is approximate."
	sequentialResults, err := ac.Find(text)
	if err != nil {
		t.Fatal(err)
	}
	parallelResults, err := ac.FindParallel(text, &ParallelOptions{
		Workers:   2,
		ChunkSize: 50,
		Boundary:  ChunkBoundaryWord,
		Overlap:   5,
	})
	if err != nil {
		t.Fatal(err)
	}

	seqSet := make(map[string]struct{})
	for _, r := range sequentialResults {
		seqSet[r] = struct{}{}
	}
	parSet := make(map[string]struct{})
	for _, r := range parallelResults {
		parSet[r] = struct{}{}
	}

	if !reflect.DeepEqual(seqSet, parSet) {
		t.Errorf("parallel results %v != sequential results %v", parSet, seqSet)
	}
}

func TestFindIndexParallel(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "her"}
	for _, k := range keywords {
		if _, err := ac.Add(k); err != nil {
			t.Fatal(err)
		}
	}

	text := "he is her friend"
	opts := &ParallelOptions{
		Workers:   2,
		ChunkSize: 10,
		Boundary:  ChunkBoundaryWord,
		Overlap:   3,
	}

	results, err := ac.FindIndexParallel(text, opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Error("expected some matches")
	}

	for keyword, indices := range results {
		for _, idx := range indices {
			if idx < 0 {
				t.Errorf("negative index for %s: %d", keyword, idx)
			}
		}
	}
}
