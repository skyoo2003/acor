package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/skyoo2003/acor/pkg/acor"
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

func (f *fakeService) Close() error {
	f.closed = true
	return nil
}

func TestParseArgs(t *testing.T) {
	parsed, remaining, err := parseArgs([]string{
		"-addr", "127.0.0.1:6379",
		"-addrs", "127.0.0.1:7000, 127.0.0.1:7001",
		"-master-name", "mymaster",
		"-ring-addrs", "shard-1=127.0.0.1:7100, shard-2=127.0.0.1:7101",
		"-password", "secret",
		"-db", "2",
		"-name", "sample",
		"-debug",
		"find", "hello",
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
	if parsed.Password != "secret" || parsed.DB != 2 || parsed.Name != "sample" || !parsed.Debug {
		t.Fatalf("unexpected parsed args: %+v", parsed)
	}
	if len(remaining) != 2 || remaining[0] != "find" || remaining[1] != "hello" {
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
			_, _, err := parseArgs(tt.args)
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

	exitCode := run([]string{"-name", "sample", "add", "he"}, stdout, stderr, func(args *acor.AhoCorasickArgs) (service, error) {
		if args.Name != "sample" {
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
	if fake.lastKeyword != "he" {
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

	exitCode := run([]string{"find", "he"}, stdout, stderr, func(*acor.AhoCorasickArgs) (service, error) {
		return fake, nil
	})

	if exitCode != 1 {
		t.Fatalf("expected exit code 1, got %d", exitCode)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("expected stderr to contain service error, got %q", stderr.String())
	}
}
