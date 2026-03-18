package acor

import "runtime"

// BatchMode defines the batch operation mode.
type BatchMode int

const (
	// BatchModeBestEffort continues on errors and returns partial results.
	BatchModeBestEffort BatchMode = iota
	// BatchModeTransactional rolls back on any error.
	BatchModeTransactional
)

// BatchOptions configures batch operation behavior.
type BatchOptions struct {
	Mode BatchMode
}

// ChunkBoundary defines how text is split into chunks for parallel processing.
type ChunkBoundary int

const (
	// ChunkBoundaryWord splits at word boundaries.
	ChunkBoundaryWord ChunkBoundary = iota
	// ChunkBoundarySentence splits at sentence boundaries.
	ChunkBoundarySentence
	// ChunkBoundaryLine splits at line boundaries.
	ChunkBoundaryLine
)

const (
	// DefaultChunkSize is the default chunk size for parallel processing.
	DefaultChunkSize = 1000
	// DefaultOverlap is the default overlap between chunks.
	DefaultOverlap             = 50
	defaultMaxBacktrackDivisor = 2
)

// ParallelOptions configures parallel text processing.
type ParallelOptions struct {
	Workers   int
	ChunkSize int
	Boundary  ChunkBoundary
	Overlap   int
}

// DefaultParallelOptions returns default parallel processing options.
func DefaultParallelOptions() *ParallelOptions {
	return &ParallelOptions{
		Workers:   runtime.NumCPU(),
		ChunkSize: DefaultChunkSize,
		Boundary:  ChunkBoundaryWord,
		Overlap:   DefaultOverlap,
	}
}

// KeywordError represents an error for a specific keyword in batch operations.
type KeywordError struct {
	Keyword string
	Error   error
}

// BatchResult contains the results of a batch operation.
type BatchResult struct {
	Added   []string
	Removed []string
	Failed  []KeywordError
	Skipped []string
}
