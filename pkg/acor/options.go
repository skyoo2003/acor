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

type ParallelOptions struct {
	Workers   int
	ChunkSize int
	Boundary  ChunkBoundary
	Overlap   int
}

func DefaultParallelOptions() *ParallelOptions {
	return &ParallelOptions{
		Workers:   runtime.NumCPU(),
		ChunkSize: 1000,
		Boundary:  ChunkBoundaryWord,
		Overlap:   0,
	}
}

type KeywordError struct {
	Keyword string
	Error   error
}

type BatchResult struct {
	Added   []string
	Failed  []KeywordError
	Skipped []string
}
