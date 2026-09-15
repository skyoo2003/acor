// SPDX-License-Identifier: Apache-2.0

// Command parallel demonstrates ACOR parallel matching: splitting a large text
// across workers with FindParallel. Requires a Redis server on localhost:6379.
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/skyoo2003/acor/pkg/acor"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ac, err := acor.CreateContext(ctx, &acor.AhoCorasickArgs{
		Addr: "localhost:6379",
		Name: "example-parallel",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to create: %w", err))
		return
	}
	defer func() { _ = ac.Close() }()

	_, err = ac.AddManyContext(ctx, []string{"foo", "bar", "baz"}, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to add keywords: %w", err))
		return
	}

	largeText := "foo bar baz "
	matches, err := ac.FindParallelContext(ctx, largeText, &acor.ParallelOptions{
		Workers:     4,
		Boundary:    acor.ChunkBoundaryWord,
		ChunkSize:   1000,
		AutoOverlap: true,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to find parallel: %w", err))
		return
	}

	fmt.Printf("Found %d matches\n", len(matches))

	_ = ac.Flush()
}
