// SPDX-License-Identifier: Apache-2.0

// Command batch demonstrates ACOR batch operations: AddMany with transactional
// semantics and FindMany across multiple texts. Requires a Redis server on
// localhost:6379.
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
		Name: "example-batch",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to create: %w", err))
		return
	}
	defer func() { _ = ac.Close() }()

	result, err := ac.AddManyContext(ctx, []string{"foo", "bar", "baz"}, &acor.BatchOptions{
		Mode: acor.BatchModeTransactional,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to add many: %w", err))
		return
	}
	fmt.Printf("Added: %d, Failed: %d, Skipped: %d\n", len(result.Added), len(result.Failed), len(result.Skipped))

	matches, err := ac.FindManyContext(ctx, []string{"foo bar", "baz qux"})
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to find many: %w", err))
		return
	}
	for text, m := range matches {
		fmt.Printf("Text %q: %v\n", text, m)
	}

	_ = ac.Flush()
}
