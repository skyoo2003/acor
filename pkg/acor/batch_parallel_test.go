// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"strings"
	"testing"
)

func TestParallelContextCancellation(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longText := strings.Repeat("he is here ", 100)
	_, _ = ac.FindParallelContext(ctx, longText, &ParallelOptions{
		ChunkSize: 20,
		Workers:   2,
	})
}

func TestParallelIndexContextCancellation(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	longText := strings.Repeat("he is here ", 100)
	_, _ = ac.FindIndexParallelContext(ctx, longText, &ParallelOptions{
		ChunkSize: 20,
		Workers:   2,
	})
}

func TestFindParallelInvalidChunkSize(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	_, err := ac.FindParallel("test", &ParallelOptions{ChunkSize: 0})
	if err == nil {
		t.Error("expected error for ChunkSize=0")
	}
}

func TestFindIndexParallelInvalidChunkSize(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	_, err := ac.FindIndexParallel("test", &ParallelOptions{ChunkSize: -1})
	if err == nil {
		t.Error("expected error for negative ChunkSize")
	}
}
