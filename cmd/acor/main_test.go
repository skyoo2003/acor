// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/skyoo2003/acor/pkg/acor"
)

const (
	collectionNameSample = "sample"
	testKeywordHello     = "hello"
	testKeywordHE        = "he"
)

type fakeService struct {
	addCount         int
	batchResult      *acor.BatchResult
	removeCount      int
	findMatches      []string
	findIndexes      map[string][]int
	parallelMatches  []string
	parallelIndexes  map[string][]int
	suggestMatches   []string
	suggestIndexes   map[string][]int
	info             *acor.AhoCorasickInfo
	err              error
	flushCalls       int
	closed           bool
	lastInput        string
	lastKeyword      string
	lastKeywords     []string
	lastBatchOpts    *acor.BatchOptions
	lastParallelOpts *acor.ParallelOptions
	lastMatchOpts    *acor.MatchOptions
}

func (f *fakeService) Add(keyword string) (int, error) {
	f.lastKeyword = keyword
	if f.err != nil {
		return 0, f.err
	}
	return f.addCount, nil
}

func (f *fakeService) AddMany(keywords []string, opts *acor.BatchOptions) (*acor.BatchResult, error) {
	f.lastKeywords = keywords
	f.lastBatchOpts = opts
	return f.batchResult, f.err
}

func (f *fakeService) Remove(keyword string) (int, error) {
	f.lastKeyword = keyword
	if f.err != nil {
		return 0, f.err
	}
	return f.removeCount, nil
}

func (f *fakeService) RemoveMany(keywords []string, opts *acor.BatchOptions) (*acor.BatchResult, error) {
	f.lastKeywords = keywords
	f.lastBatchOpts = opts
	return f.batchResult, f.err
}

func (f *fakeService) Find(input string) ([]string, error) {
	f.lastInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.findMatches, nil
}

func (f *fakeService) FindSet(input string) ([]string, error) {
	f.lastInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.findMatches, nil
}

func (f *fakeService) FindMatches(input string, opts *acor.MatchOptions) ([]acor.Match, error) {
	f.lastInput = input
	f.lastMatchOpts = opts
	if f.err != nil {
		return nil, f.err
	}
	out := make([]acor.Match, 0, len(f.findMatches))
	for _, kw := range f.findMatches {
		out = append(out, acor.Match{Keyword: kw, Start: 0, End: len([]rune(kw))})
	}
	return out, nil
}

func (f *fakeService) Contains(input string) (bool, error) {
	f.lastInput = input
	if f.err != nil {
		return false, f.err
	}
	return len(f.findMatches) > 0, nil
}

func (f *fakeService) FindIndex(input string) (map[string][]int, error) {
	f.lastInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.findIndexes, nil
}

func (f *fakeService) FindParallel(input string, opts *acor.ParallelOptions) ([]string, error) {
	f.lastInput = input
	f.lastParallelOpts = opts
	return f.parallelMatches, f.err
}

func (f *fakeService) FindIndexParallel(input string, opts *acor.ParallelOptions) (map[string][]int, error) {
	f.lastInput = input
	f.lastParallelOpts = opts
	return f.parallelIndexes, f.err
}

func (f *fakeService) Suggest(input string) ([]string, error) {
	f.lastInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.suggestMatches, nil
}

func (f *fakeService) SuggestIndex(input string) (map[string][]int, error) {
	f.lastInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.suggestIndexes, nil
}

func (f *fakeService) Info() (*acor.AhoCorasickInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.info, nil
}

func (f *fakeService) Flush() error {
	if f.err != nil {
		return f.err
	}
	f.flushCalls++
	return nil
}

func (f *fakeService) MigrateV1ToV2(opts *acor.MigrationOptions) (*acor.MigrationResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &acor.MigrationResult{Status: "success"}, nil
}

func (f *fakeService) RollbackToV1() error {
	if f.err != nil {
		return f.err
	}
	return nil
}

func (f *fakeService) SchemaVersion() int {
	return acor.SchemaV2
}

func (f *fakeService) Close() error {
	f.closed = true
	return nil
}

func TestParseArgs(t *testing.T) {
	parsed, _, remaining, err := parseArgs([]string{
		"-addr", "127.0.0.1:6379",
		"-addrs", "127.0.0.1:7000, 127.0.0.1:7001",
		"-master-name", "mymaster",
		"-ring-addrs", "shard-1=127.0.0.1:7100, shard-2=127.0.0.1:7101",
		"-password", "secret",
		"-db", "2",
		"-name", collectionNameSample,
		"-debug",
		"-cache",
		commandFind, testKeywordHello,
	})
	if err != nil {
		t.Fatal(err)
	}

	if parsed.Addr != "127.0.0.1:6379" {
		t.Fatalf("expected addr to be parsed, got %q", parsed.Addr)
	}
	if len(parsed.Addrs) != 2 {
		t.Fatalf("expected 2 addrs, got %v", parsed.Addrs)
	}
	if parsed.MasterName != "mymaster" {
		t.Fatalf("expected master name to be parsed, got %q", parsed.MasterName)
	}
	if parsed.RingAddrs["shard-1"] != "127.0.0.1:7100" || parsed.RingAddrs["shard-2"] != "127.0.0.1:7101" {
		t.Fatalf("unexpected ring addresses: %v", parsed.RingAddrs)
	}
	if parsed.Password != "secret" || parsed.DB != 2 || parsed.Name != collectionNameSample ||
		!parsed.Debug || !parsed.EnableCache {
		t.Fatalf("unexpected parsed args: %+v", parsed)
	}
	if len(remaining) != 2 || remaining[0] != commandFind || remaining[1] != testKeywordHello {
		t.Fatalf("unexpected remaining args: %v", remaining)
	}
}

func TestRunForwardsPresetConfiguration(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{
		"-addr", "localhost:6379",
		"-preset", "balanced",
		"-invalidation-poll-interval", "30s",
		"info",
	}, stdout, stderr, func(args *acor.AhoCorasickArgs) (service, error) {
		if args.Preset != acor.PresetBalanced || args.InvalidationPollInterval.String() != "30s" {
			t.Fatalf("unexpected preset args %+v", args)
		}
		return &fakeService{info: &acor.AhoCorasickInfo{}}, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
}

func TestRunRejectsInvalidFeatureOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown preset", args: []string{"-preset", "fastest", "info"}, want: "unknown preset"},
		{name: "unknown batch mode", args: []string{"-batch-mode", "atomic", "add-many", "foo"}, want: "unknown batch mode"},
		{name: "unknown boundary", args: []string{"-boundary", "byte", "find-parallel", "text"}, want: "unknown boundary"},
		{name: "negative workers", args: []string{"-workers", "-1", "find-parallel", "text"}, want: "workers must be non-negative"},
		{name: "zero chunk size", args: []string{"-chunk-size", "0", "find-parallel", "text"}, want: "chunk-size must be positive"},
		{name: "large overlap", args: []string{"-chunk-size", "10", "-overlap", "10", "find-parallel", "text"}, want: "overlap must be"},
		{name: "batch option on find", args: []string{"-batch-mode", "best-effort", "find", "text"}, want: "only applies"},
		{name: "parallel option on find", args: []string{"-workers", "2", "find", "text"}, want: "parallel matching options"},
		{name: "cache with preset", args: []string{"-addr", "localhost:6379", "-cache", "-preset", "speed", "info"}, want: "cannot be used together"},
		{name: "poll without preset", args: []string{"-invalidation-poll-interval", "30s", "info"}, want: "requires -preset"},
		{name: "migrate in preset mode", args: []string{"-addr", "localhost:6379", "-preset", "balanced", "migrate"}, want: "unavailable in preset mode"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			created := false
			exitCode := run(tt.args, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
				created = true
				return &fakeService{}, nil
			})
			if exitCode != exitCodeUsage {
				t.Fatalf("expected exit code %d, got %d", exitCodeUsage, exitCode)
			}
			if created {
				t.Fatal("expected validation before service creation")
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("expected stderr to contain %q, got %q", tt.want, stderr.String())
			}
		})
	}
}

func TestParseArgsRejectsInvalidTopologyFlags(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "empty addrs", args: []string{"-addrs", ",", "info"}, want: "addrs must contain at least one address"},
		{name: "invalid ring addrs", args: []string{"-ring-addrs", "shard-1", "info"}, want: errInvalidRingAddrs.Error()},
		{name: "empty ring addr value", args: []string{"-ring-addrs", "shard-1= ", "info"}, want: errInvalidRingAddrs.Error()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, _, err := parseArgs(tt.args)
			if err == nil {
				t.Fatal("expected parseArgs to return an error")
			}
			if !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("expected error to contain %q, got %q", tt.want, err.Error())
			}
		})
	}
}

func TestRunAddCommand(t *testing.T) {
	fake := &fakeService{addCount: 1}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-name", collectionNameSample, "add", testKeywordHE}, stdout, stderr, func(args *acor.AhoCorasickArgs) (service, error) {
		if args.Name != collectionNameSample {
			t.Fatalf("expected collection name to be forwarded, got %q", args.Name)
		}
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if stdout.String() != "{\"count\":1}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
	if fake.lastKeyword != testKeywordHE {
		t.Fatalf("expected add to receive keyword, got %q", fake.lastKeyword)
	}
	if !fake.closed {
		t.Fatal("expected service to be closed")
	}
}

func TestRunAddManyCommand(t *testing.T) {
	fake := &fakeService{batchResult: &acor.BatchResult{
		Added:   []string{"foo"},
		Failed:  []acor.KeywordError{{Keyword: "", Error: acor.ErrEmptyKeyword}},
		Skipped: []string{"Foo"},
	}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-batch-mode", "transactional", "add-many", "foo", "Foo"}, stdout, stderr,
		func(*acor.AhoCorasickArgs) (service, error) { return fake, nil })

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if strings.Join(fake.lastKeywords, ",") != "foo,Foo" {
		t.Fatalf("unexpected keywords %v", fake.lastKeywords)
	}
	if fake.lastBatchOpts == nil || fake.lastBatchOpts.Mode != acor.BatchModeTransactional {
		t.Fatalf("unexpected batch options %+v", fake.lastBatchOpts)
	}
	want := "{\"added\":[\"foo\"],\"failed\":[{\"keyword\":\"\",\"error\":\"keyword cannot be empty\"}],\"skipped\":[\"Foo\"]}\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunRemoveManyReadsLinesFromStdin(t *testing.T) {
	fake := &fakeService{batchResult: &acor.BatchResult{
		Removed: []string{"foo", "hello world"},
		Failed:  []acor.KeywordError{},
		Skipped: []string{},
	}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := runWithInput([]string{"remove-many", "-"}, strings.NewReader("foo\r\nhello world\n"), stdout, stderr,
		func(*acor.AhoCorasickArgs) (service, error) { return fake, nil })

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if strings.Join(fake.lastKeywords, ",") != "foo,hello world" {
		t.Fatalf("unexpected stdin keywords %v", fake.lastKeywords)
	}
	if fake.lastBatchOpts == nil || fake.lastBatchOpts.Mode != acor.BatchModeBestEffort {
		t.Fatalf("unexpected batch options %+v", fake.lastBatchOpts)
	}
	want := "{\"failed\":[],\"removed\":[\"foo\",\"hello world\"],\"skipped\":[]}\n"
	if stdout.String() != want {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunFindParallelReadsStdinAndForwardsOptions(t *testing.T) {
	fake := &fakeService{parallelMatches: []string{"hello", "world"}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := runWithInput([]string{
		"-workers", "3",
		"-chunk-size", "100",
		"-boundary", "line",
		"-overlap", "10",
		"find-parallel", "-",
	}, strings.NewReader("hello\nworld\n"), stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if fake.lastInput != "hello\nworld\n" {
		t.Fatalf("unexpected input %q", fake.lastInput)
	}
	wantOpts := acor.ParallelOptions{Workers: 3, ChunkSize: 100, Boundary: acor.ChunkBoundaryLine, Overlap: 10}
	if fake.lastParallelOpts == nil || *fake.lastParallelOpts != wantOpts {
		t.Fatalf("unexpected parallel options %+v", fake.lastParallelOpts)
	}
	if stdout.String() != "{\"matches\":[\"hello\",\"world\"]}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunFindIndexParallelCommand(t *testing.T) {
	fake := &fakeService{parallelIndexes: map[string][]int{"he": {0, 6}}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"find-index-parallel", "hello hello"}, stdout, stderr,
		func(*acor.AhoCorasickArgs) (service, error) { return fake, nil })

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if stdout.String() != "{\"matches\":{\"he\":[0,6]}}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunInfoAndFlushCommands(t *testing.T) {
	fake := &fakeService{info: &acor.AhoCorasickInfo{Keywords: 2, Nodes: 3}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	if exitCode := run([]string{"info"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	}); exitCode != 0 {
		t.Fatalf("expected info exit code 0, got %d", exitCode)
	}
	if stdout.String() != "{\"keywords\":2,\"nodes\":3}\n" {
		t.Fatalf("unexpected info stdout %q", stdout.String())
	}

	stdout.Reset()
	if exitCode := run([]string{"flush"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	}); exitCode != 0 {
		t.Fatalf("expected flush exit code 0, got %d", exitCode)
	}
	if stdout.String() != "{\"status\":\"ok\"}\n" {
		t.Fatalf("unexpected flush stdout %q", stdout.String())
	}
	if fake.flushCalls != 1 {
		t.Fatalf("expected flush to be called once, got %d", fake.flushCalls)
	}
}

func TestRunReturnsUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "missing command", args: []string{}, want: "Usage:"},
		{name: "unknown command", args: []string{"unknown"}, want: "unknown command"},
		{name: "missing argument", args: []string{"find"}, want: "requires exactly one argument"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			exitCode := run(tt.args, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
				return &fakeService{}, nil
			})
			if exitCode != 2 {
				t.Fatalf("expected exit code 2, got %d", exitCode)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Fatalf("expected stderr to contain %q, got %q", tt.want, stderr.String())
			}
		})
	}
}

func TestRunReturnsServiceErrors(t *testing.T) {
	fake := &fakeService{err: errors.New("boom")}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"find", testKeywordHE}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("expected stderr to contain service error, got %q", stderr.String())
	}
}

func TestRunRemoveCommand(t *testing.T) {
	fake := &fakeService{removeCount: 2}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-name", "test", "remove", testKeywordHE}, stdout, stderr, func(args *acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if fake.lastKeyword != testKeywordHE {
		t.Fatalf("expected keyword %q, got %q", testKeywordHE, fake.lastKeyword)
	}
	if stdout.String() != "{\"count\":2}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
	if !fake.closed {
		t.Fatal("expected service to be closed")
	}
}

func TestRunFindIndexCommand(t *testing.T) {
	fake := &fakeService{findIndexes: map[string][]int{testKeywordHE: {0, 1}}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"find-index", testKeywordHello}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if fake.lastInput != testKeywordHello {
		t.Fatalf("expected input %q, got %q", testKeywordHello, fake.lastInput)
	}
	if stdout.String() != "{\"matches\":{\"he\":[0,1]}}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunSuggestCommand(t *testing.T) {
	fake := &fakeService{suggestMatches: []string{testKeywordHello, "help"}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"suggest", "hel"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if fake.lastInput != "hel" {
		t.Fatalf("expected input %q, got %q", "hel", fake.lastInput)
	}
	if stdout.String() != "{\"matches\":[\"hello\",\"help\"]}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunSuggestIndexCommand(t *testing.T) {
	fake := &fakeService{suggestIndexes: map[string][]int{testKeywordHE: {0, 1}, "her": {0, 2}}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"suggest-index", testKeywordHE}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if fake.lastInput != testKeywordHE {
		t.Fatalf("expected input %q, got %q", testKeywordHE, fake.lastInput)
	}
	if stdout.String() != "{\"matches\":{\"he\":[0,1],\"her\":[0,2]}}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunMigrateCommand(t *testing.T) {
	fake := &fakeService{}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"migrate"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"status\":\"success\"") {
		t.Fatalf("expected stdout to contain success status, got %q", stdout.String())
	}
}

func TestRunMigrateCommandWithDryRun(t *testing.T) {
	fake := &fakeService{}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-dry-run", "-keep-old-keys", "migrate"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "\"status\":\"success\"") {
		t.Fatalf("expected stdout to contain success status, got %q", stdout.String())
	}
}

// version must answer without a backend: it is what you run when the CLI is
// misbehaving, which is exactly when Redis may be unreachable.
func TestRunVersionCommandNeedsNoBackend(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"version"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		t.Fatal("version must not construct a service")
		return nil, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != version {
		t.Fatalf("expected %q, got %q", version, stdout.String())
	}
}

func TestRunMatchingCommands(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "find-set", args: []string{"find-set", "hehe"}, want: `"matches":["he"]`},
		{name: "contains", args: []string{"contains", "hehe"}, want: `"contains":true`},
		{
			name: "find-matches",
			args: []string{"find-matches", "hehe"},
			want: `"matches":[{"keyword":"he","start":0,"end":2}]`,
		},
		{
			name: "find-matches with options",
			args: []string{"-match-kind", "leftmost-longest", "-whole-word", "find-matches", "hehe"},
			want: `"keyword":"he"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeService{findMatches: []string{testKeywordHE}}
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			exitCode := run(tc.args, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
				return fake, nil
			})

			if exitCode != 0 {
				t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
			}
			if !strings.Contains(stdout.String(), tc.want) {
				t.Fatalf("expected stdout to contain %q, got %q", tc.want, stdout.String())
			}
		})
	}
}

func TestRunFindMatchesPassesOptions(t *testing.T) {
	fake := &fakeService{findMatches: []string{testKeywordHE}}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-match-kind", "leftmost-longest", "-whole-word", "find-matches", "hehe"},
		stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) { return fake, nil })

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if fake.lastMatchOpts == nil {
		t.Fatal("expected match options to reach FindMatches")
	}
	if fake.lastMatchOpts.Kind != acor.MatchKindLeftmostLongest {
		t.Fatalf("expected leftmost-longest, got %v", fake.lastMatchOpts.Kind)
	}
	if !fake.lastMatchOpts.WholeWord {
		t.Fatal("expected WholeWord to be set")
	}
}

func TestRunRejectsMatchFlagsOnOtherCommands(t *testing.T) {
	for _, args := range [][]string{
		{"-match-kind", "leftmost-longest", "find", "text"},
		{"-whole-word", "find-set", "text"},
	} {
		t.Run(args[0], func(t *testing.T) {
			fake := &fakeService{}
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			exitCode := run(args, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
				return fake, nil
			})

			if exitCode != exitCodeUsage {
				t.Fatalf("expected exit code %d, got %d", exitCodeUsage, exitCode)
			}
			if !strings.Contains(stderr.String(), "only apply to") {
				t.Fatalf("expected an explanatory error, got %q", stderr.String())
			}
		})
	}
}

func TestRunRejectsMigrateFlagsOnOtherCommands(t *testing.T) {
	for _, args := range [][]string{
		{"-dry-run", "flush"},
		{"-keep-old-keys", "info"},
	} {
		t.Run(args[0], func(t *testing.T) {
			fake := &fakeService{}
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			exitCode := run(args, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
				return fake, nil
			})

			if exitCode != exitCodeUsage {
				t.Fatalf("expected exit code %d, got %d", exitCodeUsage, exitCode)
			}
			if fake.flushCalls != 0 {
				t.Fatal("expected the command not to run")
			}
			if !strings.Contains(stderr.String(), "only apply to") {
				t.Fatalf("expected an explanatory error, got %q", stderr.String())
			}
		})
	}
}

func TestRunMigrateRollbackCommand(t *testing.T) {
	fake := &fakeService{}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"migrate-rollback"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if stdout.String() != "{\"status\":\"rolled_back\"}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunSchemaVersionCommand(t *testing.T) {
	fake := &fakeService{}
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"schema-version"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d with stderr %q", exitCode, stderr.String())
	}
	if stdout.String() != "{\"schema_version\":2}\n" {
		t.Fatalf("unexpected stdout %q", stdout.String())
	}
}

func TestRunCommandServiceErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "add error", args: []string{"add", "kw"}},
		{name: "add-many error", args: []string{"add-many", "kw"}},
		{name: "remove error", args: []string{"remove", "kw"}},
		{name: "remove-many error", args: []string{"remove-many", "kw"}},
		{name: "find error", args: []string{"find", "input"}},
		{name: "find-index error", args: []string{"find-index", "input"}},
		{name: "find-parallel error", args: []string{"find-parallel", "input"}},
		{name: "find-index-parallel error", args: []string{"find-index-parallel", "input"}},
		{name: "suggest error", args: []string{"suggest", "input"}},
		{name: "suggest-index error", args: []string{"suggest-index", "input"}},
		{name: "info error", args: []string{"info"}},
		{name: "flush error", args: []string{"flush"}},
		{name: "migrate error", args: []string{"migrate"}},
		{name: "migrate-rollback error", args: []string{"migrate-rollback"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeService{err: errors.New("service error")}
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			exitCode := run(tt.args, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
				return fake, nil
			})

			if exitCode != 1 {
				t.Fatalf("expected exit code 1, got %d", exitCode)
			}
			if !strings.Contains(stderr.String(), "service error") {
				t.Fatalf("expected stderr to contain service error, got %q", stderr.String())
			}
		})
	}
}

func TestRunCreateServiceError(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"info"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return nil, errors.New("connection refused")
	})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "connection refused") {
		t.Fatalf("expected stderr to contain creation error, got %q", stderr.String())
	}
}

func TestRunHelpFlag(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"-help"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return &fakeService{}, nil
	})

	if exitCode != 0 {
		t.Fatalf("expected exit code 0, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected stderr to contain usage text, got %q", stderr.String())
	}
	for _, want := range []string{"add-many", "find-parallel", "-batch-mode", "-preset", "-cache"} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("expected help to contain %q, got %q", want, stderr.String())
		}
	}
}

func TestParseCSV(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{name: "empty string", input: "", want: []string{}},
		{name: "single value", input: "a", want: []string{"a"}},
		{name: "multiple values", input: "a,b,c", want: []string{"a", "b", "c"}},
		{name: "trailing comma", input: "a,b,", want: []string{"a", "b"}},
		{name: "leading comma", input: ",a,b", want: []string{"a", "b"}},
		{name: "spaces trimmed", input: " a , b , c ", want: []string{"a", "b", "c"}},
		{name: "only commas", input: ",,,", want: []string{}},
		{name: "only spaces", input: " , , ", want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseCSV(tt.input)
			if len(got) != len(tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			for i, v := range got {
				if v != tt.want[i] {
					t.Fatalf("at index %d: expected %q, got %q", i, tt.want[i], v)
				}
			}
		})
	}
}

func TestParseRingAddrs(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]string
		wantErr bool
	}{
		{name: "empty string", input: "", want: nil, wantErr: false},
		{name: "spaces only", input: "  ", want: nil, wantErr: false},
		{name: "valid pair", input: "shard-1=localhost:7000", want: map[string]string{"shard-1": "localhost:7000"}, wantErr: false},
		{
			name:    "valid multiple pairs",
			input:   "shard-1=localhost:7000,shard-2=localhost:7001",
			want:    map[string]string{"shard-1": "localhost:7000", "shard-2": "localhost:7001"},
			wantErr: false,
		},
		{name: "missing equals sign", input: "shard-1", want: nil, wantErr: true},
		{name: "empty name", input: "=localhost:7000", want: nil, wantErr: true},
		{name: "empty addr", input: "shard-1=", want: nil, wantErr: true},
		{name: "space only name", input: " =localhost:7000", want: nil, wantErr: true},
		{name: "space only addr", input: "shard-1= ", want: nil, wantErr: true},
		{name: "empty part between commas", input: "shard-1=localhost:7000,,shard-2=localhost:7001", want: nil, wantErr: true},
		{name: "with spaces around pair", input: " shard-1 = localhost:7000 ", want: map[string]string{"shard-1": "localhost:7000"}, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseRingAddrs(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err != errInvalidRingAddrs {
					t.Fatalf("expected errInvalidRingAddrs, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.want == nil {
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
			for k, v := range tt.want {
				if got[k] != v {
					t.Fatalf("expected %q=%q, got %q=%q", k, v, k, got[k])
				}
			}
		})
	}
}

func TestCommandHandler(t *testing.T) {
	tests := []struct {
		name    string
		command string
		mode    argumentMode
		wantErr bool
	}{
		{name: "add", command: "add", mode: argumentsOne},
		{name: "add-many", command: "add-many", mode: argumentsOneOrMore},
		{name: "remove", command: "remove", mode: argumentsOne},
		{name: "remove-many", command: "remove-many", mode: argumentsOneOrMore},
		{name: "find", command: "find", mode: argumentsOne},
		{name: "find-index", command: "find-index", mode: argumentsOne},
		{name: "find-parallel", command: "find-parallel", mode: argumentsOne},
		{name: "find-index-parallel", command: "find-index-parallel", mode: argumentsOne},
		{name: "suggest", command: "suggest", mode: argumentsOne},
		{name: "suggest-index", command: "suggest-index", mode: argumentsOne},
		{name: "info", command: "info", mode: argumentsNone},
		{name: "flush", command: "flush", mode: argumentsNone},
		{name: "migrate", command: "migrate", mode: argumentsNone},
		{name: "migrate-rollback", command: "migrate-rollback", mode: argumentsNone},
		{name: "schema-version", command: "schema-version", mode: argumentsNone},
		{name: "unknown command", command: "bogus", mode: argumentsNone, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, mode, err := commandHandler(tt.command)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), "unknown command") {
					t.Fatalf("expected unknown command error, got %q", err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if runner == nil {
				t.Fatal("expected runner, got nil")
			}
			if mode != tt.mode {
				t.Fatalf("expected mode=%v, got %v", tt.mode, mode)
			}
		})
	}
}

func TestCommandArguments(t *testing.T) {
	tests := []struct {
		name    string
		command string
		args    []string
		mode    argumentMode
		wantErr bool
	}{
		{name: "one arg", command: "find", args: []string{testKeywordHello}, mode: argumentsOne},
		{name: "one arg missing", command: "find", mode: argumentsOne, wantErr: true},
		{name: "one arg gets too many", command: "find", args: []string{"a", "b"}, mode: argumentsOne, wantErr: true},
		{name: "many args", command: "add-many", args: []string{"a", "b"}, mode: argumentsOneOrMore},
		{name: "many args missing", command: "add-many", mode: argumentsOneOrMore, wantErr: true},
		{name: "no args", command: "info", mode: argumentsNone},
		{name: "no args gets one", command: "info", args: []string{"extra"}, mode: argumentsNone, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := commandArguments(tt.command, tt.args, tt.mode)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.args) {
				t.Fatalf("expected %v, got %v", tt.args, got)
			}
		})
	}
}

func TestRunNoCommandShowsUsage(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return &fakeService{}, nil
	})

	if exitCode != exitCodeUsage {
		t.Fatalf("expected exit code %d, got %d", exitCodeUsage, exitCode)
	}
	if !strings.Contains(stderr.String(), "Usage:") {
		t.Fatalf("expected stderr to contain usage, got %q", stderr.String())
	}
}

func TestRunTooManyArgumentsForCommand(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"info", "extra"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return &fakeService{}, nil
	})

	if exitCode != exitCodeUsage {
		t.Fatalf("expected exit code %d, got %d", exitCodeUsage, exitCode)
	}
	if !strings.Contains(stderr.String(), "does not accept arguments") {
		t.Fatalf("expected stderr to contain 'does not accept arguments', got %q", stderr.String())
	}
}

func TestRunAddWithTooManyArgs(t *testing.T) {
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}

	exitCode := run([]string{"add", "a", "b"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return &fakeService{}, nil
	})

	if exitCode != exitCodeUsage {
		t.Fatalf("expected exit code %d, got %d", exitCodeUsage, exitCode)
	}
	if !strings.Contains(stderr.String(), "requires exactly one argument") {
		t.Fatalf("expected stderr to contain 'requires exactly one argument', got %q", stderr.String())
	}
}

func TestParseArgsDefaultName(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "default name when empty", args: []string{"-name", "", "info"}, want: "default"},
		{name: "default name when whitespace", args: []string{"-name", "  ", "info"}, want: "default"},
		{name: "preserves explicit name", args: []string{"-name", "mycol", "info"}, want: "mycol"},
		{name: "trims name", args: []string{"-name", " mycol ", "info"}, want: "mycol"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, _, _, err := parseArgs(tt.args)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if parsed.Name != tt.want {
				t.Fatalf("expected name %q, got %q", tt.want, parsed.Name)
			}
		})
	}
}

func TestParseArgsInvalidFlag(t *testing.T) {
	_, _, _, err := parseArgs([]string{"-bogus", "info"})
	if err == nil {
		t.Fatal("expected error for invalid flag, got nil")
	}
}

// commandSpecs is the dispatch table; commandsText is the hand-written help. A
// command added to one and not the other is either invisible in `acor --help` or
// advertised and unimplemented, and nothing else catches that.
func TestUsageTextListsEveryCommand(t *testing.T) {
	for command := range commandSpecs {
		if !strings.Contains(commandsText, "\n  "+command) {
			t.Errorf("command %q is dispatchable but missing from the usage text", command)
		}
	}

	_, listed, ok := strings.Cut(commandsText, "Commands:\n")
	if !ok {
		t.Fatal(`usage text has no "Commands:" section`)
	}
	listed, _, _ = strings.Cut(listed, "\nOptions:")
	for _, line := range strings.Split(listed, "\n") {
		// The first field is the command name; the rest is argument syntax.
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if _, ok := commandSpecs[fields[0]]; !ok && fields[0] != "dictionary" {
			t.Errorf("usage text advertises %q, which has no commandSpecs entry", fields[0])
		}
	}
}

// version must reject what every other command rejects: the short-circuit that
// keeps it from needing Redis must not also skip argument and flag validation.
func TestRunVersionValidatesArguments(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "stray argument", args: []string{"version", "extra"}},
		{name: "migrate flag", args: []string{"-dry-run", "version"}},
		{name: "match flag", args: []string{"-whole-word", "version"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			exitCode := run(tc.args, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
				t.Fatal("version must not construct a service")
				return nil, nil
			})

			if exitCode != exitCodeUsage {
				t.Fatalf("expected exit code %d, got %d with stdout %q", exitCodeUsage, exitCode, stdout.String())
			}
		})
	}
}

// Preset mode has no prefix index, so the library answers ErrSuggestRequiresRedis.
// The CLI must say so before connecting and loading the whole dictionary.
func TestRunRejectsSuggestInPresetMode(t *testing.T) {
	for _, command := range []string{"suggest", "suggest-index"} {
		t.Run(command, func(t *testing.T) {
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}

			exitCode := run([]string{"-addr", "localhost:6379", "-preset", "speed", command, "he"},
				stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
					t.Fatal("preset-unsupported commands must fail before create()")
					return nil, nil
				})

			if exitCode != exitCodeUsage {
				t.Fatalf("expected exit code %d, got %d", exitCodeUsage, exitCode)
			}
			if !strings.Contains(stderr.String(), "unavailable in preset mode") {
				t.Fatalf("expected an explanatory error, got %q", stderr.String())
			}
		})
	}
}

func TestWriteJSON(t *testing.T) {
	tests := []struct {
		name  string
		value interface{}
		want  string
	}{
		{name: "map string int", value: map[string]int{"count": 5}, want: "{\"count\":5}\n"},
		{name: "map string string", value: map[string]string{"status": "ok"}, want: "{\"status\":\"ok\"}\n"},
		{name: "map string slice", value: map[string][]string{"matches": {"a", "b"}}, want: "{\"matches\":[\"a\",\"b\"]}\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeJSON(&buf, tt.value); err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if buf.String() != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, buf.String())
			}
		})
	}
}
