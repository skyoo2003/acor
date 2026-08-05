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
  find-set <input>
  find-matches <input>
  contains <input>
  find-parallel <input> | -
  find-index-parallel <input> | -
  suggest <input>
  suggest-index <input>
  info
  flush
  migrate [options]
  migrate-rollback
  schema-version
  version

Options:
`
)

// version is stamped at build time with -ldflags "-X main.version=vX.Y.Z". A
// plain `go build` or `go install` leaves it "dev".
var version = "dev"

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
	commandFindSet           = "find-set"
	commandFindMatches       = "find-matches"
	commandContains          = "contains"
	commandVersion           = "version"
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

	jsonKeyCount    = "count"
	jsonKeyMatches  = "matches"
	jsonKeyStatus   = "status"
	jsonKeyContains = "contains"
)

// matchJSON is the wire shape for find-matches. acor.Match carries no JSON tags,
// so marshaling it directly would emit Go field names among the CLI's snake_case
// output — and adding tags upstream would change what library callers marshal.
type matchJSON struct {
	Keyword string `json:"keyword"`
	Start   int    `json:"start"`
	End     int    `json:"end"`
}

type service interface {
	Add(string) (int, error)
	AddMany([]string, *acor.BatchOptions) (*acor.BatchResult, error)
	Remove(string) (int, error)
	RemoveMany([]string, *acor.BatchOptions) (*acor.BatchResult, error)
	Find(string) ([]string, error)
	FindIndex(string) (map[string][]int, error)
	FindSet(string) ([]string, error)
	FindMatches(string, *acor.MatchOptions) ([]acor.Match, error)
	Contains(string) (bool, error)
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
	matchKind    string
	wholeWord    bool
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
	commandFindSet:           {runFindSet, argumentsOne},
	commandFindMatches:       {runFindMatches, argumentsOne},
	commandContains:          {runContains, argumentsOne},
	commandFindParallel:      {runFindParallel, argumentsOne},
	commandFindIndexParallel: {runFindIndexParallel, argumentsOne},
	commandSuggest:           {runSuggest, argumentsOne},
	commandSuggestIndex:      {runSuggestIndex, argumentsOne},
	commandInfo:              {runInfo, argumentsNone},
	commandFlush:             {runFlush, argumentsNone},
	commandMigrate:           {runMigrate, argumentsNone},
	commandMigrateRollback:   {runMigrateRollback, argumentsNone},
	commandSchemaVersion:     {runSchemaVersion, argumentsNone},
	commandVersion:           {runVersion, argumentsNone},
}

func main() {
	os.Exit(runWithInput(os.Args[1:], os.Stdin, os.Stdout, os.Stderr, createService))
}

type commandOptions struct {
	dryRun           bool
	keepOldKeys      bool
	batchMode        acor.BatchMode
	parallel         acor.ParallelOptions
	match            acor.MatchOptions
	batchFlagsSet    bool
	parallelFlagsSet bool
	matchFlagsSet    bool
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

	// version is the one command that must answer without a backend: it is what you
	// run when the CLI misbehaves, which is exactly when Redis may be unreachable.
	// It still passes through every check above, so stray arguments and misapplied
	// flags are rejected for it like for any other command.
	var ac service
	if command != commandVersion {
		created, createErr := create(config)
		if createErr != nil {
			_, _ = fmt.Fprintln(stderr, createErr.Error())
			return 1
		}
		defer func() { _ = created.Close() }()
		ac = created
	}

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
		matchKind: "overlapping",
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
	fs.StringVar(&config.matchKind, "match-kind", config.matchKind,
		"find-matches: overlapping or leftmost-longest")
	fs.BoolVar(&config.wholeWord, "whole-word", false,
		"find-matches: drop matches whose neighboring runes are word characters "+
			"(scripts without spaces between words, such as CJK, drop nearly every match)")
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

	enums, err := parseEnumOptions(config)
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
		batchMode:   enums.batchMode,
		parallel: acor.ParallelOptions{
			Workers:   config.workers,
			ChunkSize: config.chunkSize,
			Boundary:  enums.boundary,
			Overlap:   config.overlap,
		},
		match: acor.MatchOptions{
			Kind:      enums.matchKind,
			WholeWord: config.wholeWord,
		},
		batchFlagsSet:    seen["batch-mode"],
		parallelFlagsSet: seen["workers"] || seen["chunk-size"] || seen["boundary"] || seen["overlap"],
		matchFlagsSet:    seen["match-kind"] || seen["whole-word"],
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
		Preset:                   enums.preset,
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

// enumOptions holds the flags that map a string onto a library enum. They are
// parsed together so parseArgs carries one error branch instead of four.
type enumOptions struct {
	preset    acor.Preset
	batchMode acor.BatchMode
	boundary  acor.ChunkBoundary
	matchKind acor.MatchKind
}

func parseEnumOptions(config *commandConfig) (*enumOptions, error) {
	preset, err := parsePreset(config.preset)
	if err != nil {
		return nil, err
	}
	batchMode, err := parseBatchMode(config.batchMode)
	if err != nil {
		return nil, err
	}
	boundary, err := parseBoundary(config.boundary)
	if err != nil {
		return nil, err
	}
	matchKind, err := parseMatchKind(config.matchKind)
	if err != nil {
		return nil, err
	}
	return &enumOptions{
		preset:    preset,
		batchMode: batchMode,
		boundary:  boundary,
		matchKind: matchKind,
	}, nil
}

func parseMatchKind(raw string) (acor.MatchKind, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "overlapping":
		return acor.MatchKindOverlapping, nil
	case "leftmost-longest":
		return acor.MatchKindLeftmostLongest, nil
	default:
		return acor.MatchKindOverlapping, fmt.Errorf("unknown match kind %q", raw)
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

	if opts.matchFlagsSet && command != commandFindMatches {
		return fmt.Errorf("-match-kind and -whole-word only apply to %q", commandFindMatches)
	}

	return validatePresetOptions(command, config, opts)
}

// presetUnsupported lists the commands the library refuses in preset mode:
// ErrMigrationRequiresRedis for the migration pair, ErrSuggestRequiresRedis for
// the prefix lookups the local engine holds no index for.
var presetUnsupported = map[string]bool{
	commandMigrate:         true,
	commandMigrateRollback: true,
	commandSuggest:         true,
	commandSuggestIndex:    true,
}

// validatePresetOptions rejects flag and command combinations that preset mode
// cannot honor. The library refuses the same combinations (ErrCacheWithPreset,
// ErrMigrationRequiresRedis, ErrSuggestRequiresRedis); these checks name the
// offending flag and fail with the usage exit code instead of after a connection
// attempt and a full dictionary load.
func validatePresetOptions(command string, config *acor.AhoCorasickArgs, opts *commandOptions) error {
	if config.EnableCache && config.Preset != acor.PresetNone {
		return errors.New("-cache and -preset cannot be used together; preset mode already uses a local engine")
	}
	if opts.pollFlagSet && config.Preset == acor.PresetNone {
		return errors.New("-invalidation-poll-interval requires -preset")
	}
	if config.Preset != acor.PresetNone && presetUnsupported[command] {
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
	result, err := ac.RemoveMany(keywords, &acor.BatchOptions{Mode: opts.batchMode})
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

func runFindSet(_ io.Reader, stdout io.Writer, ac service, args []string, _ *commandOptions) error {
	matches, err := ac.FindSet(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string][]string{jsonKeyMatches: matches})
}

func runFindMatches(_ io.Reader, stdout io.Writer, ac service, args []string, opts *commandOptions) error {
	matches, err := ac.FindMatches(args[0], &opts.match)
	if err != nil {
		return err
	}
	out := make([]matchJSON, 0, len(matches))
	for _, m := range matches {
		out = append(out, matchJSON{Keyword: m.Keyword, Start: m.Start, End: m.End})
	}
	return writeJSON(stdout, map[string][]matchJSON{jsonKeyMatches: out})
}

func runContains(_ io.Reader, stdout io.Writer, ac service, args []string, _ *commandOptions) error {
	found, err := ac.Contains(args[0])
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]bool{jsonKeyContains: found})
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
	// Not named `version`: that identifier is the build version at package scope.
	schemaVersion := ac.SchemaVersion()
	return writeJSON(stdout, map[string]int{"schema_version": schemaVersion})
}

// runVersion is the only runner that receives a nil service: runWithInput skips
// create() for it so the build version is reportable without a reachable Redis.
func runVersion(_ io.Reader, stdout io.Writer, _ service, _ []string, _ *commandOptions) error {
	_, _ = fmt.Fprintln(stdout, version)
	return nil
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
