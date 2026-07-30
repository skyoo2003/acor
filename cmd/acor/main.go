// SPDX-License-Identifier: Apache-2.0

package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/skyoo2003/acor/pkg/acor"
)

var errInvalidRingAddrs = errors.New("ring-addrs must be comma-separated shard=addr pairs")

const (
	exitCodeUsage = 2
	keyValueParts = 2
	commandsText  = `Usage:
  acor [global options] <command> [argument]

Commands:
  add <keyword>
  remove <keyword>
  find <input>
  find-index <input>
  suggest <input>
  suggest-index <input>
  info
  flush
  migrate [options]
  migrate-rollback
  schema-version

Options:
`
)

// writeUsage prints the command list followed by the flag set's own defaults,
// so a flag's description lives only where the flag is registered.
func writeUsage(w io.Writer) {
	_, _ = fmt.Fprint(w, commandsText)
	fs, _ := newFlagSet()
	fs.SetOutput(w)
	fs.PrintDefaults()
}

const (
	commandAdd             = "add"
	commandRemove          = "remove"
	commandFind            = "find"
	commandFindIndex       = "find-index"
	commandSuggest         = "suggest"
	commandSuggestIndex    = "suggest-index"
	commandInfo            = "info"
	commandFlush           = "flush"
	commandMigrate         = "migrate"
	commandMigrateRollback = "migrate-rollback"
	commandSchemaVersion   = "schema-version"

	defaultCollectionName = "default"

	jsonKeyCount   = "count"
	jsonKeyMatches = "matches"
	jsonKeyStatus  = "status"
)

type service interface {
	Add(string) (int, error)
	Remove(string) (int, error)
	Find(string) ([]string, error)
	FindIndex(string) (map[string][]int, error)
	Suggest(string) ([]string, error)
	SuggestIndex(string) (map[string][]int, error)
	Info() (*acor.AhoCorasickInfo, error)
	Flush() error
	MigrateV1ToV2(*acor.MigrationOptions) (*acor.MigrationResult, error)
	RollbackToV1() error
	SchemaVersion() int
	Close() error
}

type commandConfig struct {
	addr        string
	addrs       string
	masterName  string
	ringAddrs   string
	password    string
	db          int
	name        string
	debug       bool
	dryRun      bool
	keepOldKeys bool
}

type commandRunner func(io.Writer, service, string, *migrateOptions) error

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, createService))
}

type migrateOptions struct {
	dryRun      bool
	keepOldKeys bool
}

func run(args []string, stdout, stderr io.Writer, create func(*acor.AhoCorasickArgs) (service, error)) int {
	config, migrateOpts, remaining, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			writeUsage(stderr)
			return 0
		}
		_, _ = fmt.Fprintln(stderr, err.Error())
		return exitCodeUsage
	}

	if len(remaining) == 0 {
		writeUsage(stderr)
		return exitCodeUsage
	}

	command := remaining[0]
	runner, needsArg, err := commandHandler(command)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		writeUsage(stderr)
		return exitCodeUsage
	}

	// Both flags live on the single global flag set, so reject them rather than
	// ignore them: a silently dropped -dry-run turns a preview into a real run.
	if command != commandMigrate && (migrateOpts.dryRun || migrateOpts.keepOldKeys) {
		_, _ = fmt.Fprintf(stderr, "-dry-run and -keep-old-keys only apply to the %q command\n", commandMigrate)
		return exitCodeUsage
	}

	commandArg, err := commandArgument(command, remaining[1:], needsArg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return exitCodeUsage
	}

	ac, err := create(config)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer func() { _ = ac.Close() }()

	if err := runner(stdout, ac, commandArg, migrateOpts); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}

	return 0
}

// newFlagSet registers the global and migrate flags. It is also what writeUsage
// renders, so the flag list cannot drift from the help text.
func newFlagSet() (*flag.FlagSet, *commandConfig) {
	config := &commandConfig{}
	fs := flag.NewFlagSet("acor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&config.addr, "addr", "", "Redis server address for standalone mode")
	fs.StringVar(&config.addrs, "addrs", "", "Comma-separated Redis addresses for Sentinel or Cluster mode")
	fs.StringVar(&config.masterName, "master-name", "", "Redis Sentinel master name")
	fs.StringVar(&config.ringAddrs, "ring-addrs", "", "Comma-separated shard=addr pairs for Redis Ring mode")
	fs.StringVar(&config.password, "password", "", "Redis password")
	fs.IntVar(&config.db, "db", 0, "Redis DB number")
	fs.StringVar(&config.name, "name", defaultCollectionName, "Pattern collection name")
	fs.BoolVar(&config.debug, "debug", false, "Enable debug logging")
	fs.BoolVar(&config.dryRun, "dry-run", false, "migrate: preview migration without making changes")
	fs.BoolVar(&config.keepOldKeys, "keep-old-keys", false, "migrate: keep V1 keys after migration (for rollback)")
	fs.Usage = func() {}
	return fs, config
}

func parseArgs(args []string) (*acor.AhoCorasickArgs, *migrateOptions, []string, error) {
	fs, config := newFlagSet()

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, nil, nil, flag.ErrHelp
		}
		return nil, nil, nil, err
	}

	config.name = strings.TrimSpace(config.name)
	if config.name == "" {
		config.name = defaultCollectionName
	}

	ringAddrs, err := parseRingAddrs(config.ringAddrs)
	if err != nil {
		return nil, nil, nil, err
	}

	addrs := parseCSV(config.addrs)
	if strings.TrimSpace(config.addrs) != "" && len(addrs) == 0 {
		return nil, nil, nil, errors.New("addrs must contain at least one address")
	}

	migrateOpts := &migrateOptions{
		dryRun:      config.dryRun,
		keepOldKeys: config.keepOldKeys,
	}

	return &acor.AhoCorasickArgs{
		Addr:       strings.TrimSpace(config.addr),
		Addrs:      addrs,
		MasterName: strings.TrimSpace(config.masterName),
		RingAddrs:  ringAddrs,
		Password:   config.password,
		DB:         config.db,
		Name:       config.name,
		Debug:      config.debug,
	}, migrateOpts, fs.Args(), nil
}

func parseCSV(raw string) []string {
	parts := strings.Split(raw, ",")
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		values = append(values, trimmed)
	}
	return values
}

func parseRingAddrs(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}

	values := make(map[string]string)
	for _, part := range strings.Split(raw, ",") {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			return nil, errInvalidRingAddrs
		}

		pair := strings.SplitN(trimmed, "=", keyValueParts)
		if len(pair) != keyValueParts {
			return nil, errInvalidRingAddrs
		}
		name := strings.TrimSpace(pair[0])
		addr := strings.TrimSpace(pair[1])
		if name == "" || addr == "" {
			return nil, errInvalidRingAddrs
		}
		values[name] = addr
	}

	if len(values) == 0 {
		return nil, errInvalidRingAddrs
	}
	return values, nil
}

func commandHandler(command string) (commandRunner, bool, error) {
	switch command {
	case commandAdd:
		return runAdd, true, nil
	case commandRemove:
		return runRemove, true, nil
	case commandFind:
		return runFind, true, nil
	case commandFindIndex:
		return runFindIndex, true, nil
	case commandSuggest:
		return runSuggest, true, nil
	case commandSuggestIndex:
		return runSuggestIndex, true, nil
	case commandInfo:
		return runInfo, false, nil
	case commandFlush:
		return runFlush, false, nil
	case commandMigrate:
		return runMigrate, false, nil
	case commandMigrateRollback:
		return runMigrateRollback, false, nil
	case commandSchemaVersion:
		return runSchemaVersion, false, nil
	default:
		return nil, false, fmt.Errorf("unknown command %q", command)
	}
}

func commandArgument(command string, args []string, needsArg bool) (string, error) {
	if needsArg {
		if len(args) != 1 {
			return "", fmt.Errorf("command %q requires exactly one argument", command)
		}
		return args[0], nil
	}

	if len(args) != 0 {
		return "", fmt.Errorf("command %q does not accept arguments", command)
	}
	return "", nil
}

func createService(args *acor.AhoCorasickArgs) (service, error) {
	return acor.Create(args)
}

func runAdd(stdout io.Writer, ac service, input string, _ *migrateOptions) error {
	count, err := ac.Add(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{jsonKeyCount: count})
}

func runRemove(stdout io.Writer, ac service, input string, _ *migrateOptions) error {
	count, err := ac.Remove(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{jsonKeyCount: count})
}

func runFind(stdout io.Writer, ac service, input string, _ *migrateOptions) error {
	matches, err := ac.Find(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string][]string{jsonKeyMatches: matches})
}

func runFindIndex(stdout io.Writer, ac service, input string, _ *migrateOptions) error {
	matches, err := ac.FindIndex(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]map[string][]int{jsonKeyMatches: matches})
}

func runSuggest(stdout io.Writer, ac service, input string, _ *migrateOptions) error {
	matches, err := ac.Suggest(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string][]string{jsonKeyMatches: matches})
}

func runSuggestIndex(stdout io.Writer, ac service, input string, _ *migrateOptions) error {
	matches, err := ac.SuggestIndex(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]map[string][]int{jsonKeyMatches: matches})
}

func runInfo(stdout io.Writer, ac service, _ string, _ *migrateOptions) error {
	info, err := ac.Info()
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{
		"keywords": info.Keywords,
		"nodes":    info.Nodes,
	})
}

func runFlush(stdout io.Writer, ac service, _ string, _ *migrateOptions) error {
	if err := ac.Flush(); err != nil {
		return err
	}
	return writeJSON(stdout, map[string]string{jsonKeyStatus: "ok"})
}

func runMigrate(stdout io.Writer, ac service, _ string, opts *migrateOptions) error {
	result, err := ac.MigrateV1ToV2(&acor.MigrationOptions{
		DryRun:      opts.dryRun,
		KeepOldKeys: opts.keepOldKeys,
	})
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runMigrateRollback(stdout io.Writer, ac service, _ string, _ *migrateOptions) error {
	if err := ac.RollbackToV1(); err != nil {
		return err
	}
	return writeJSON(stdout, map[string]string{jsonKeyStatus: "rolled_back"})
}

func runSchemaVersion(stdout io.Writer, ac service, _ string, _ *migrateOptions) error {
	version := ac.SchemaVersion()
	return writeJSON(stdout, map[string]int{"schema_version": version})
}

func writeJSON(w io.Writer, value interface{}) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(value)
}
