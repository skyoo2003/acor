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
	addCount       int
	removeCount    int
	findMatches    []string
	findIndexes    map[string][]int
	suggestMatches []string
	suggestIndexes map[string][]int
	info           *acor.AhoCorasickInfo
	err            error
	flushCalls     int
	closed         bool
	lastInput      string
	lastKeyword    string
}

func (f *fakeService) Add(keyword string) (int, error) {
	f.lastKeyword = keyword
	if f.err != nil {
		return 0, f.err
	}
	return f.addCount, nil
}

func (f *fakeService) Remove(keyword string) (int, error) {
	f.lastKeyword = keyword
	if f.err != nil {
		return 0, f.err
	}
	return f.removeCount, nil
}

func (f *fakeService) Find(input string) ([]string, error) {
	f.lastInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.findMatches, nil
}

func (f *fakeService) FindIndex(input string) (map[string][]int, error) {
	f.lastInput = input
	if f.err != nil {
		return nil, f.err
	}
	return f.findIndexes, nil
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
	if len(parsed.RingAddrs) != 2 {
		t.Fatalf("expected 2 ring addresses, got %v", parsed.RingAddrs)
	}
	if parsed.Password != "secret" || parsed.DB != 2 || parsed.Name != collectionNameSample || !parsed.Debug {
		t.Fatalf("unexpected parsed args: %+v", parsed)
	}
	if len(remaining) != 2 || remaining[0] != commandFind || remaining[1] != testKeywordHello {
		t.Fatalf("unexpected remaining args: %v", remaining)
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
		{name: "remove error", args: []string{"remove", "kw"}},
		{name: "find error", args: []string{"find", "input"}},
		{name: "find-index error", args: []string{"find-index", "input"}},
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
		name     string
		command  string
		needsArg bool
		wantErr  bool
	}{
		{name: "add", command: "add", needsArg: true, wantErr: false},
		{name: "remove", command: "remove", needsArg: true, wantErr: false},
		{name: "find", command: "find", needsArg: true, wantErr: false},
		{name: "find-index", command: "find-index", needsArg: true, wantErr: false},
		{name: "suggest", command: "suggest", needsArg: true, wantErr: false},
		{name: "suggest-index", command: "suggest-index", needsArg: true, wantErr: false},
		{name: "info", command: "info", needsArg: false, wantErr: false},
		{name: "flush", command: "flush", needsArg: false, wantErr: false},
		{name: "migrate", command: "migrate", needsArg: false, wantErr: false},
		{name: "migrate-rollback", command: "migrate-rollback", needsArg: false, wantErr: false},
		{name: "schema-version", command: "schema-version", needsArg: false, wantErr: false},
		{name: "unknown command", command: "bogus", needsArg: false, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner, needsArg, err := commandHandler(tt.command)
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
			if needsArg != tt.needsArg {
				t.Fatalf("expected needsArg=%v, got %v", tt.needsArg, needsArg)
			}
		})
	}
}

func TestCommandArgument(t *testing.T) {
	tests := []struct {
		name     string
		command  string
		args     []string
		needsArg bool
		want     string
		wantErr  bool
	}{
		{name: "needs arg with one arg", command: "find", args: []string{testKeywordHello}, needsArg: true, want: testKeywordHello, wantErr: false},
		{name: "needs arg with no args", command: "find", args: []string{}, needsArg: true, want: "", wantErr: true},
		{name: "needs arg with too many args", command: "find", args: []string{"a", "b"}, needsArg: true, want: "", wantErr: true},
		{name: "no arg needed with no args", command: "info", args: []string{}, needsArg: false, want: "", wantErr: false},
		{name: "no arg needed but gets args", command: "info", args: []string{"extra"}, needsArg: false, want: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := commandArgument(tt.command, tt.args, tt.needsArg)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %q, got %q", tt.want, got)
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
