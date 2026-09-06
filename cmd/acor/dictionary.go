// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/skyoo2003/acor/pkg/acor"
)

const dictionaryCopyV2 = "copy-v2"
const dictionaryReplace = "replace"
const dictionaryDiff = "diff"
const dictionaryList = "list"
const dictionaryPageSize = 1000

type dictionaryConfig struct {
	command    string
	expected   string
	allowEmpty bool
	cursor     string
	limit      int
	source     string
	sensitive  bool
	words      []string
}

func dispatchDictionary(connection *acor.AhoCorasickArgs, opts *commandOptions, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if opts.dryRun || opts.keepOldKeys {
		_, _ = fmt.Fprintln(stderr, "migration flags do not apply to dictionary")
		return exitCodeUsage
	}
	return runDictionary(connection, args, stdin, stdout, stderr)
}

func parseDictionary(args []string, stdin io.Reader, stderr io.Writer) (*dictionaryConfig, error) {
	if len(args) == 0 {
		return nil, errors.New("dictionary requires list, diff, replace, status, copy-v2 or prune")
	}
	c := &dictionaryConfig{command: args[0]}
	fs := flag.NewFlagSet("dictionary "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&c.expected, "expected-version", "", "required opaque version for replace/copy-v2")
	fs.BoolVar(&c.allowEmpty, "allow-empty", false, "allow an empty replacement")
	fs.StringVar(&c.cursor, "cursor", "", "page cursor (version must still be active)")
	fs.IntVar(&c.limit, "limit", dictionaryPageSize, "page size")
	fs.StringVar(&c.source, "source", "", "source V2 collection name")
	fs.BoolVar(&c.sensitive, "case-sensitive", false, "persisted case policy")
	if err := fs.Parse(args[1:]); err != nil {
		return nil, err
	}
	if fs.NArg() != 0 {
		return nil, errors.New("dictionary input is a JSON string array on stdin")
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	if c.command == dictionaryReplace || c.command == dictionaryDiff {
		if err := c.readInput(stdin); err != nil {
			return nil, err
		}
	}
	return c, nil
}
func (c *dictionaryConfig) validate() error {
	switch c.command {
	case dictionaryList, dictionaryDiff, dictionaryReplace, "status", dictionaryCopyV2, "prune":
	default:
		return errors.New("unknown dictionary command")
	}
	if (c.command == dictionaryReplace || c.command == dictionaryCopyV2) && c.expected == "" {
		return errors.New("--expected-version is required")
	}
	if c.command == dictionaryCopyV2 && c.source == "" {
		return errors.New("--source is required")
	}
	if c.limit <= 0 {
		return errors.New("--limit must be positive")
	}
	return nil
}
func (c *dictionaryConfig) readInput(stdin io.Reader) error {
	decoder := json.NewDecoder(stdin)
	if err := decoder.Decode(&c.words); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errors.New("expected one JSON array")
	}
	if c.command == dictionaryReplace && len(c.words) == 0 && !c.allowEmpty {
		return errors.New("empty replacement requires --allow-empty")
	}
	return nil
}
func runDictionary(connection *acor.AhoCorasickArgs, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	c, err := parseDictionary(args, stdin, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return exitCodeUsage
	}
	ctx := context.Background()
	v, err := acor.OpenVersioned(ctx, &acor.VersionedOptions{Redis: *connection, CaseSensitive: c.sensitive})
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	defer func() { _ = v.Close() }()
	result, err := c.execute(ctx, v)
	if err != nil {
		if result != nil {
			_ = json.NewEncoder(stdout).Encode(result)
		}
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	if err = json.NewEncoder(stdout).Encode(result); err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
func (c *dictionaryConfig) execute(ctx context.Context, v *acor.VersionedCollection) (interface{}, error) {
	switch c.command {
	case "status":
		return v.Status(), nil
	case dictionaryReplace:
		return v.Replace(ctx, acor.Version(c.expected), c.words)
	case "prune":
		return v.Prune(ctx)
	case dictionaryCopyV2:
		return v.CopyV2(ctx, c.source, acor.Version(c.expected), &acor.V2CopyOptions{RejectEmpty: !c.allowEmpty})
	case dictionaryList, dictionaryDiff:
		s, err := v.Snapshot(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = s.Close(ctx) }()
		if c.command == dictionaryList {
			return s.List(ctx, c.cursor, c.limit)
		}
		return s.Diff(ctx, c.words)
	}
	return nil, errors.New("unknown dictionary command")
}
