package acor

import "errors"

var (
	// ErrEmptyKeyword is returned when an empty keyword is provided.
	ErrEmptyKeyword = errors.New("keyword cannot be empty")
	// ErrInvalidChunkSize is returned when an invalid chunk size is provided.
	ErrInvalidChunkSize = errors.New("chunk size must be positive")
	// ErrInvalidWorkerCount is returned when an invalid worker count is provided.
	ErrInvalidWorkerCount = errors.New("worker count must be positive")
	// ErrNoBoundariesFound is returned when no suitable chunk boundaries are found.
	ErrNoBoundariesFound = errors.New("could not find suitable chunk boundaries")
	// ErrStreamInterrupted is returned when stream processing is interrupted.
	ErrStreamInterrupted = errors.New("stream processing was interrupted")
)
