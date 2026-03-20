package acor

import "runtime"

// BatchMode defines the behavior when errors occur during batch operations.
type BatchMode int

const (
	// BatchModeBestEffort continues processing on errors and returns partial results.
	// Failed operations are recorded in BatchResult.Failed for later handling.
	// This is the default mode when BatchOptions.Mode is nil.
	BatchModeBestEffort BatchMode = iota
	// BatchModeTransactional stops and rolls back on the first error.
	// All successfully added keywords are removed before returning the error.
	// Use this when you need all-or-nothing semantics.
	BatchModeTransactional
)

// BatchOptions configures batch operation behavior for AddMany and RemoveMany.
type BatchOptions struct {
	// Mode determines how errors are handled during batch operations.
	// Defaults to BatchModeBestEffort if nil.
	Mode BatchMode
}

// ChunkBoundary defines how text is split into chunks for parallel processing.
// Choosing the right boundary can improve match accuracy across chunk boundaries.
type ChunkBoundary int

const (
	// ChunkBoundaryWord splits text at word boundaries (whitespace).
	// Best for natural language text. This is the default.
	ChunkBoundaryWord ChunkBoundary = iota
	// ChunkBoundarySentence splits text at sentence boundaries (., !, ? followed by space).
	// Best for paragraph matching where sentence context matters.
	ChunkBoundarySentence
	// ChunkBoundaryLine splits text at line boundaries (newlines).
	// Best for log files or line-oriented data.
	ChunkBoundaryLine
)

const (
	// DefaultChunkSize is the default chunk size in characters for parallel processing.
	DefaultChunkSize = 1000
	// DefaultOverlap is the default number of characters that overlap between chunks.
	// Overlap ensures keywords spanning chunk boundaries are still matched.
	DefaultOverlap             = 50
	defaultMaxBacktrackDivisor = 2
)

// ParallelOptions configures parallel text processing for FindParallel and FindIndexParallel.
// Parallel processing splits text into chunks processed by multiple workers.
type ParallelOptions struct {
	// Workers is the number of concurrent goroutines for processing.
	// Defaults to runtime.NumCPU() if <= 0.
	Workers int
	// ChunkSize is the target size of each text chunk in characters.
	// Smaller chunks increase parallelism but may reduce accuracy at boundaries.
	// Defaults to DefaultChunkSize (1000).
	ChunkSize int
	// Boundary determines how chunks are split. Defaults to ChunkBoundaryWord.
	Boundary ChunkBoundary
	// Overlap is the number of characters that overlap between adjacent chunks.
	// Larger overlap improves accuracy for keywords spanning boundaries but increases work.
	// Defaults to DefaultOverlap (50).
	Overlap int
}

// DefaultParallelOptions returns parallel processing options with sensible defaults:
// Workers set to CPU count, ChunkSize of 1000, word boundaries, and 50-character overlap.
func DefaultParallelOptions() *ParallelOptions {
	return &ParallelOptions{
		Workers:   runtime.NumCPU(),
		ChunkSize: DefaultChunkSize,
		Boundary:  ChunkBoundaryWord,
		Overlap:   DefaultOverlap,
	}
}

// KeywordError represents an error that occurred for a specific keyword
// during a batch operation. It pairs the keyword with its associated error.
type KeywordError struct {
	// Keyword is the word that caused the error.
	Keyword string
	// Error is the error that occurred while processing the keyword.
	Error error
}

// BatchResult contains the results of a batch operation (AddMany or RemoveMany).
// It provides detailed information about successful and failed operations.
type BatchResult struct {
	// Added contains keywords that were successfully added.
	Added []string
	// Removed contains keywords that were successfully removed.
	Removed []string
	// Failed contains keywords that could not be processed with their errors.
	Failed []KeywordError
	// Skipped contains keywords that were skipped (e.g., duplicates in input).
	Skipped []string
}
