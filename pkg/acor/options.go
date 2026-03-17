package acor

import "runtime"

type BatchMode int

const (
	BatchModeBestEffort BatchMode = iota
	BatchModeTransactional
)

type BatchOptions struct {
	Mode BatchMode
}

type ChunkBoundary int

const (
	ChunkBoundaryWord ChunkBoundary = iota
	ChunkBoundarySentence
	ChunkBoundaryLine
)

const (
	DefaultChunkSize           = 1000
	DefaultOverlap             = 0
	defaultMaxBacktrackDivisor = 2
)

type ParallelOptions struct {
	Workers   int
	ChunkSize int
	Boundary  ChunkBoundary
	Overlap   int
}

func DefaultParallelOptions() *ParallelOptions {
	return &ParallelOptions{
		Workers:   runtime.NumCPU(),
		ChunkSize: DefaultChunkSize,
		Boundary:  ChunkBoundaryWord,
		Overlap:   DefaultOverlap,
	}
}

type KeywordError struct {
	Keyword string
	Error   error
}

type BatchResult struct {
	Added   []string
	Removed []string
	Failed  []KeywordError
	Skipped []string
}
