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
	"time"

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
  add-many <keyword>... | -
  remove <keyword>
  remove-many <keyword>... | -
  find <input>
  find-index <input>
  find-parallel <input> | -
  find-index-parallel <input> | -
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
	commandAdd               = "add"
	commandAddMany           = "add-many"
	commandRemove            = "remove"
	commandRemoveMany        = "remove-many"
	commandFind              = "find"
	commandFindIndex         = "find-index"
	commandFindParallel      = "find-parallel"
	commandFindIndexParallel = "find-index-parallel"
	commandSuggest           = "suggest"
	commandSuggestIndex      = "suggest-index"
	commandInfo              = "info"
	commandFlush             = "flush"
	commandMigrate           = "migrate"
	commandMigrateRollback   = "migrate-rollback"
	commandSchemaVersion     = "schema-version"

	defaultCollectionName = "default"

	jsonKeyCount   = "count"
	jsonKeyMatches = "matches"
	jsonKeyStatus  = "status"
)

type service interface {
	Add(string) (int, error)
	AddMany([]string, *acor.BatchOptions) (*acor.BatchResult, error)
	Remove(string) (int, error)
	RemoveManyWithOptions([]string, *acor.BatchOptions) (*acor.BatchResult, error)
	Find(string) ([]string, error)
	FindIndex(string) (map[string][]int, error)
	FindParallel(string, *acor.ParallelOptions) ([]string, error)
	FindIndexParallel(string, *acor.ParallelOptions) (map[string][]int, error)
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
	addr         string
	addrs        string
	masterName   string
	ringAddrs    string
	password     string
	db           int
	name         string
	debug        bool
	cache        bool
	preset       string
	pollInterval time.Duration
	batchMode    string
	workers      int
	chunkSize    int
	boundary     string
	overlap      int
	dryRun       bool
	keepOldKeys  bool
}

type argumentMode int

const (
	argumentsNone argumentMode = iota
	argumentsOne
	argumentsOneOrMore
)

type commandRunner func(io.Reader, io.Writer, service, []string, *commandOptions) error

type commandSpec struct {
	runner commandRunner
	mode   argumentMode
}

var commandSpecs = map[string]commandSpec{
	commandAdd:               {runAdd, argumentsOne},
	commandAddMany:           {runAddMany, argumentsOneOrMore},
	commandRemove:            {runRemove, argumentsOne},
	commandRemoveMany:        {runRemoveMany, argumentsOneOrMore},
	commandFind:              {runFind, argumentsOne},
	commandFindIndex:         {runFindIndex, argumentsOne},
	commandFindParallel:      {runFindParallel, argumentsOne},
	commandFindIndexParallel: {runFindIndexParallel, argumentsOne},
	commandSuggest:           {runSuggest, argumentsOne},
	commandSuggestIndex:      {runSuggestIndex, argumentsOne},
	commandInfo:              {runInfo, argumentsNone},
	commandFlush:             {runFlush, argumentsNone},
	commandMigrate:           {runMigrate, argumentsNone},
	commandMigrateRollback:   {runMigrateRollback, argumentsNone},
	commandSchemaVersion:     {runSchemaVersion, argumentsNone},
}

func main() {
	os.Exit(runWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, createService))
}

type commandOptions struct {
	dryRun           bool
	keepOldKeys      bool
	batchMode        acor.BatchMode
	parallel         acor.ParallelOptions
	batchFlagsSet    bool
	parallelFlagsSet bool
	pollFlagSet      bool
}

func run(args []string, stdout, stderr io.Writer, create func(*acor.AhoCorasickArgs) (service, error)) int {
	return runWithInput(args, strings.NewReader(""), stdout, stderr, create)
}

func runWithInput(args []string, stdin io.Reader, stdout, stderr io.Writer,
	create func(*acor.AhoCorasickArgs) (service, error)) int {
	config, commandOpts, remaining, err := parseArgs(args)
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
	runner, argMode, err := commandHandler(command)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		writeUsage(stderr)
		return exitCodeUsage
	}

	// Both flags live on the single global flag set, so reject them rather than
	// ignore them: a silently dropped -dry-run turns a preview into a real run.
	if command != commandMigrate && (commandOpts.dryRun || commandOpts.keepOldKeys) {
		_, _ = fmt.Fprintf(stderr, "-dry-run and -keep-old-keys only apply to the %q command\n", commandMigrate)
		return exitCodeUsage
	}

	commandArgs, err := commandArguments(command, remaining[1:], argMode)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return exitCodeUsage
	}
	if validationErr := validateCommandOptions(command, config, commandOpts); validationErr != nil {
		_, _ = fmt.Fprintln(stderr, validationErr.Error())
		return exitCodeUsage
	}

	ac, err := create(config)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer func() { _ = ac.Close() }()

	if err := runner(stdin, stdout, ac, commandArgs, commandOpts); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}

	return 0
}

// newFlagSet registers the global and migrate flags. It is also what writeUsage
// renders, so the flag list cannot drift from the help text.
func newFlagSet() (*flag.FlagSet, *commandConfig) {
	config := &commandConfig{
		preset:    "none",
		batchMode: "best-effort",
		chunkSize: acor.DefaultChunkSize,
		boundary:  "word",
		overlap:   acor.DefaultOverlap,
	}
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
	fs.BoolVar(&config.cache, "cache", false, "Enable the local V2 matching cache")
	fs.StringVar(&config.preset, "preset", config.preset, "Local engine preset: none, speed, balanced, or memory-efficient")
	fs.DurationVar(&config.pollInterval, "invalidation-poll-interval", 0, "Preset mode: poll interval for missed invalidations (for example 30s)")
	fs.StringVar(&config.batchMode, "batch-mode", config.batchMode, "Batch mode: best-effort or transactional")
	fs.IntVar(&config.workers, "workers", 0, "Parallel matching workers (0 uses the CPU count)")
	fs.IntVar(&config.chunkSize, "chunk-size", config.chunkSize, "Parallel matching chunk size in runes")
	fs.StringVar(&config.boundary, "boundary", config.boundary, "Parallel chunk boundary: word, sentence, or line")
	fs.IntVar(&config.overlap, "overlap", config.overlap, "Parallel chunk overlap in runes")
	fs.BoolVar(&config.dryRun, "dry-run", false, "migrate: preview migration without making changes")
	fs.BoolVar(&config.keepOldKeys, "keep-old-keys", false, "migrate: keep V1 keys after migration (for rollback)")
	fs.Usage = func() {}
	return fs, config
}

func parseArgs(args []string) (*acor.AhoCorasickArgs, *commandOptions, []string, error) {
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

	preset, err := parsePreset(config.preset)
	if err != nil {
		return nil, nil, nil, err
	}
	batchMode, err := parseBatchMode(config.batchMode)
	if err != nil {
		return nil, nil, nil, err
	}
	boundary, err := parseBoundary(config.boundary)
	if err != nil {
		return nil, nil, nil, err
	}
	if validationErr := validateNumericOptions(config); validationErr != nil {
		return nil, nil, nil, validationErr
	}

	seen := make(map[string]bool)
	fs.Visit(func(f *flag.Flag) { seen[f.Name] = true })
	commandOpts := &commandOptions{
		dryRun:      config.dryRun,
		keepOldKeys: config.keepOldKeys,
		batchMode:   batchMode,
		parallel: acor.ParallelOptions{
			Workers:   config.workers,
			ChunkSize: config.chunkSize,
			Boundary:  boundary,
			Overlap:   config.overlap,
		},
		batchFlagsSet:    seen["batch-mode"],
		parallelFlagsSet: seen["workers"] || seen["chunk-size"] || seen["boundary"] || seen["overlap"],
		pollFlagSet:      seen["invalidation-poll-interval"],
	}

	return &acor.AhoCorasickArgs{
		Addr:                     strings.TrimSpace(config.addr),
		Addrs:                    addrs,
		MasterName:               strings.TrimSpace(config.masterName),
		RingAddrs:                ringAddrs,
		Password:                 config.password,
		DB:                       config.db,
		Name:                     config.name,
		Debug:                    config.debug,
		EnableCache:              config.cache,
		Preset:                   preset,
		InvalidationPollInterval: config.pollInterval,
	}, commandOpts, fs.Args(), nil
}

func parsePreset(raw string) (acor.Preset, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "none":
		return acor.PresetNone, nil
	case "speed":
		return acor.PresetSpeed, nil
	case "balanced":
		return acor.PresetBalanced, nil
	case "memory-efficient":
		return acor.PresetMemoryEfficient, nil
	default:
		return acor.PresetNone, fmt.Errorf("unknown preset %q", raw)
	}
}

func parseBatchMode(raw string) (acor.BatchMode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "best-effort":
		return acor.BatchModeBestEffort, nil
	case "transactional":
		return acor.BatchModeTransactional, nil
	default:
		return acor.BatchModeBestEffort, fmt.Errorf("unknown batch mode %q", raw)
	}
}

func parseBoundary(raw string) (acor.ChunkBoundary, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "word":
		return acor.ChunkBoundaryWord, nil
	case "sentence":
		return acor.ChunkBoundarySentence, nil
	case "line":
		return acor.ChunkBoundaryLine, nil
	default:
		return acor.ChunkBoundaryWord, fmt.Errorf("unknown boundary %q", raw)
	}
}

func validateNumericOptions(config *commandConfig) error {
	switch {
	case config.workers < 0:
		return errors.New("workers must be non-negative")
	case config.chunkSize <= 0:
		return errors.New("chunk-size must be positive")
	case config.overlap < 0 || config.overlap >= config.chunkSize:
		return errors.New("overlap must be non-negative and smaller than chunk-size")
	case config.pollInterval < 0:
		return errors.New("invalidation-poll-interval must be non-negative")
	default:
		return nil
	}
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

func commandHandler(command string) (commandRunner, argumentMode, error) {
	spec, ok := commandSpecs[command]
	if !ok {
		return nil, argumentsNone, fmt.Errorf("unknown command %q", command)
	}
	return spec.runner, spec.mode, nil
}

func commandArguments(command string, args []string, mode argumentMode) ([]string, error) {
	switch mode {
	case argumentsOne:
		if len(args) != 1 {
			return nil, fmt.Errorf("command %q requires exactly one argument", command)
		}
	case argumentsOneOrMore:
		if len(args) == 0 {
			return nil, fmt.Errorf("command %q requires at least one argument", command)
		}
	case argumentsNone:
		if len(args) != 0 {
			return nil, fmt.Errorf("command %q does not accept arguments", command)
		}
	}
	return args, nil
}

func validateCommandOptions(command string, config *acor.AhoCorasickArgs, opts *commandOptions) error {
	isBatch := command == commandAddMany || command == commandRemoveMany
	if opts.batchFlagsSet && !isBatch {
		return fmt.Errorf("-batch-mode only applies to %q and %q", commandAddMany, commandRemoveMany)
	}

	isParallel := command == commandFindParallel || command == commandFindIndexParallel
	if opts.parallelFlagsSet && !isParallel {
		return fmt.Errorf("parallel matching options only apply to %q and %q", commandFindParallel, commandFindIndexParallel)
	}

	if config.EnableCache && config.Preset != acor.PresetNone {
		return errors.New("-cache and -preset cannot be used together; preset mode already uses a local engine")
	}
	if opts.pollFlagSet && config.Preset == acor.PresetNone {
		return errors.New("-invalidation-poll-interval requires -preset")
	}
	if config.Preset != acor.PresetNone &&
		(command == commandMigrate || command == commandMigrateRollback) {
		return fmt.Errorf("%q is unavailable in preset mode", command)
	}
	return nil
}

func createService(args *acor.AhoCorasickArgs) (service, error) {
	return acor.Create(args)
}

func runAdd(_ io.Reader, stdout io.Writer, ac service, args []string, _ *commandOptions) error {
	count, err := ac.Add(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{jsonKeyCount: count})
}

func runAddMany(stdin io.Reader, stdout io.Writer, ac service, args []string, opts *commandOptions) error {
	keywords, err := batchKeywords(stdin, args)
	if err != nil {
		return err
	}
	result, err := ac.AddMany(keywords, &acor.BatchOptions{Mode: opts.batchMode})
	if err != nil {
		return err
	}
	return writeBatchResult(stdout, "added", result.Added, result)
}

func runRemove(_ io.Reader, stdout io.Writer, ac service, args []string, _ *commandOptions) error {
	count, err := ac.Remove(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{jsonKeyCount: count})
}

func runRemoveMany(stdin io.Reader, stdout io.Writer, ac service, args []string, opts *commandOptions) error {
	keywords, err := batchKeywords(stdin, args)
	if err != nil {
		return err
	}
	result, err := ac.RemoveManyWithOptions(keywords, &acor.BatchOptions{Mode: opts.batchMode})
	if err != nil {
		return err
	}
	return writeBatchResult(stdout, "removed", result.Removed, result)
}

func runFind(_ io.Reader, stdout io.Writer, ac service, args []string, _ *commandOptions) error {
	matches, err := ac.Find(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string][]string{jsonKeyMatches: matches})
}

func runFindIndex(_ io.Reader, stdout io.Writer, ac service, args []string, _ *commandOptions) error {
	matches, err := ac.FindIndex(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]map[string][]int{jsonKeyMatches: matches})
}

func runFindParallel(stdin io.Reader, stdout io.Writer, ac service, args []string, opts *commandOptions) error {
	input, err := textInput(stdin, args[0])
	if err != nil {
		return err
	}
	matches, err := ac.FindParallel(input, &opts.parallel)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string][]string{jsonKeyMatches: matches})
}

func runFindIndexParallel(stdin io.Reader, stdout io.Writer, ac service, args []string, opts *commandOptions) error {
	input, err := textInput(stdin, args[0])
	if err != nil {
		return err
	}
	matches, err := ac.FindIndexParallel(input, &opts.parallel)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]map[string][]int{jsonKeyMatches: matches})
}

func runSuggest(_ io.Reader, stdout io.Writer, ac service, args []string, _ *commandOptions) error {
	matches, err := ac.Suggest(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string][]string{jsonKeyMatches: matches})
}

func runSuggestIndex(_ io.Reader, stdout io.Writer, ac service, args []string, _ *commandOptions) error {
	matches, err := ac.SuggestIndex(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]map[string][]int{jsonKeyMatches: matches})
}

func runInfo(_ io.Reader, stdout io.Writer, ac service, _ []string, _ *commandOptions) error {
	info, err := ac.Info()
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{
		"keywords": info.Keywords,
		"nodes":    info.Nodes,
	})
}

func runFlush(_ io.Reader, stdout io.Writer, ac service, _ []string, _ *commandOptions) error {
	if err := ac.Flush(); err != nil {
		return err
	}
	return writeJSON(stdout, map[string]string{jsonKeyStatus: "ok"})
}

func runMigrate(_ io.Reader, stdout io.Writer, ac service, _ []string, opts *commandOptions) error {
	result, err := ac.MigrateV1ToV2(&acor.MigrationOptions{
		DryRun:      opts.dryRun,
		KeepOldKeys: opts.keepOldKeys,
	})
	if err != nil {
		return err
	}
	return writeJSON(stdout, result)
}

func runMigrateRollback(_ io.Reader, stdout io.Writer, ac service, _ []string, _ *commandOptions) error {
	if err := ac.RollbackToV1(); err != nil {
		return err
	}
	return writeJSON(stdout, map[string]string{jsonKeyStatus: "rolled_back"})
}

func runSchemaVersion(_ io.Reader, stdout io.Writer, ac service, _ []string, _ *commandOptions) error {
	version := ac.SchemaVersion()
	return writeJSON(stdout, map[string]int{"schema_version": version})
}

func batchKeywords(stdin io.Reader, args []string) ([]string, error) {
	if len(args) != 1 || args[0] != "-" {
		return args, nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return nil, fmt.Errorf("read batch input: %w", err)
	}
	if len(data) == 0 {
		return []string{}, nil
	}
	lines := strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines, nil
}

func textInput(stdin io.Reader, arg string) (string, error) {
	if arg != "-" {
		return arg, nil
	}
	data, err := io.ReadAll(stdin)
	if err != nil {
		return "", fmt.Errorf("read matching input: %w", err)
	}
	return string(data), nil
}

type batchFailure struct {
	Keyword string `json:"keyword"`
	Error   string `json:"error"`
}

func writeBatchResult(w io.Writer, changedKey string, changed []string, result *acor.BatchResult) error {
	failed := make([]batchFailure, len(result.Failed))
	for i, failure := range result.Failed {
		failed[i] = batchFailure{Keyword: failure.Keyword, Error: fmt.Sprint(failure.Error)}
	}
	return writeJSON(w, map[string]interface{}{
		changedKey: changed,
		"failed":   failed,
		"skipped":  result.Skipped,
	})
}

func writeJSON(w io.Writer, value interface{}) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(value)
}
