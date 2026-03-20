package acor_test

import (
	"fmt"
	"os"

	"github.com/skyoo2003/acor/pkg/acor"
)

func Example_batchOperations() {
	ac, err := acor.Create(&acor.AhoCorasickArgs{
		Addr: "localhost:6379",
		Name: "example-batch",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create: %v\n", err)
		return
	}
	defer func() { _ = ac.Close() }()

	result, err := ac.AddMany([]string{"foo", "bar", "baz"}, &acor.BatchOptions{
		Mode: acor.BatchModeTransactional,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to add many: %v\n", err)
		return
	}
	fmt.Printf("Added: %d, Failed: %d\n", len(result.Added), len(result.Failed))

	matches, err := ac.FindMany([]string{"foo bar", "baz qux"})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find many: %v\n", err)
		return
	}
	for text, m := range matches {
		fmt.Printf("Text %q: %v\n", text, m)
	}

	_ = ac.Flush()
}
