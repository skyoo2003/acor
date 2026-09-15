// SPDX-License-Identifier: Apache-2.0

// Command basic demonstrates basic ACOR usage: create a collection, add
// keywords, and find matches in a text. Requires a Redis server on
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
		Name: "example-basic",
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to create: %w", err))
		return
	}
	defer func() { _ = ac.Close() }()

	keywords := []string{"he", "she", "his", "hers"}
	for _, kw := range keywords {
		if _, addErr := ac.AddContext(ctx, kw); addErr != nil {
			fmt.Fprintln(os.Stderr, fmt.Errorf("failed to add keyword: %w", addErr))
			return
		}
	}

	matches, err := ac.FindContext(ctx, "ushers")
	if err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to find: %w", err))
		return
	}

	fmt.Println(matches)

	if err := ac.FlushContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, fmt.Errorf("failed to flush: %w", err))
	}
}
