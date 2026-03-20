package acor_test

import (
	"fmt"
	"os"

	"github.com/skyoo2003/acor/pkg/acor"
)

func Example_parallelMatching() {
	ac, err := acor.Create(&acor.AhoCorasickArgs{
		Addr: "localhost:6379",
		Name: "example-parallel",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create: %v\n", err)
		return
	}
	defer func() { _ = ac.Close() }()

	_, err = ac.AddMany([]string{"foo", "bar", "baz"}, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to add keywords: %v\n", err)
		return
	}

	largeText := "foo bar baz "
	matches, err := ac.FindParallel(largeText, &acor.ParallelOptions{
		Workers:   4,
		Boundary:  acor.ChunkBoundaryWord,
		ChunkSize: 1000,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find parallel: %v\n", err)
		return
	}

	fmt.Printf("Found %d matches\n", len(matches))

	_ = ac.Flush()
}
