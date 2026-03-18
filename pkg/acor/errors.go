package acor

import "errors"

var (
	ErrEmptyKeyword       = errors.New("keyword cannot be empty")
	ErrInvalidChunkSize   = errors.New("chunk size must be positive")
	ErrInvalidWorkerCount = errors.New("worker count must be positive")
	ErrNoBoundariesFound  = errors.New("could not find suitable chunk boundaries")
	ErrStreamInterrupted  = errors.New("stream processing was interrupted")
)
