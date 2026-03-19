package acor_test

import (
	"fmt"
	"os"

	"github.com/skyoo2003/acor/pkg/acor"
)

func Example() {
	ac, err := acor.Create(&acor.AhoCorasickArgs{
		Addr: "localhost:6379",
		Name: "example-basic",
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to create: %v\n", err)
		return
	}
	defer func() { _ = ac.Close() }()

	keywords := []string{"he", "she", "his", "hers"}
	for _, kw := range keywords {
		if _, addErr := ac.Add(kw); addErr != nil {
			fmt.Fprintf(os.Stderr, "failed to add keyword: %v\n", addErr)
			return
		}
	}

	matches, err := ac.Find("ushers")
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to find: %v\n", err)
		return
	}

	fmt.Println(matches)

	if err := ac.Flush(); err != nil {
		fmt.Fprintf(os.Stderr, "failed to flush: %v\n", err)
	}
}
