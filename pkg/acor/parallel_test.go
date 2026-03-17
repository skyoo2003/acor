package acor

import (
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

	for i, chunk := range chunks {
		if chunk.start > chunk.end {
			t.Errorf("chunk %d has invalid bounds: start=%d, end=%d", i, chunk.start, chunk.end)
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
}
