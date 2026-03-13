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

const usageText = `Usage:
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

Global options:
  -addr string
        Redis server address for standalone mode
  -addrs string
        Comma-separated Redis addresses for Sentinel or Cluster mode
  -master-name string
        Redis Sentinel master name
  -ring-addrs string
        Comma-separated shard=addr pairs for Redis Ring mode
  -password string
        Redis password
  -db int
        Redis DB number
  -name string
        Pattern collection name (default "default")
  -debug
        Enable debug logging
`

type service interface {
	Add(string) (int, error)
	Remove(string) (int, error)
	Find(string) ([]string, error)
	FindIndex(string) (map[string][]int, error)
	Suggest(string) ([]string, error)
	SuggestIndex(string) (map[string][]int, error)
	Info() (*acor.AhoCorasickInfo, error)
	Flush() error
	Close() error
}

type commandConfig struct {
	addr       string
	addrs      string
	masterName string
	ringAddrs  string
	password   string
	db         int
	name       string
	debug      bool
}

type commandRunner func(io.Writer, service, string) error

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, createService))
}

func run(args []string, stdout, stderr io.Writer, create func(*acor.AhoCorasickArgs) (service, error)) int {
	config, remaining, err := parseArgs(args)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			_, _ = fmt.Fprint(stderr, usageText)
			return 0
		}
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}

	if len(remaining) == 0 {
		_, _ = fmt.Fprint(stderr, usageText)
		return 2
	}

	command := remaining[0]
	runner, needsArg, err := commandHandler(command)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		_, _ = fmt.Fprint(stderr, usageText)
		return 2
	}

	commandArg, err := commandArgument(command, remaining[1:], needsArg)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 2
	}

	ac, err := create(config)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}
	defer func() { _ = ac.Close() }()

	if err := runner(stdout, ac, commandArg); err != nil {
		_, _ = fmt.Fprintln(stderr, err.Error())
		return 1
	}

	return 0
}

func parseArgs(args []string) (*acor.AhoCorasickArgs, []string, error) {
	config := &commandConfig{}
	fs := flag.NewFlagSet("acor", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&config.addr, "addr", "", "redis server address")
	fs.StringVar(&config.addrs, "addrs", "", "comma-separated redis addresses")
	fs.StringVar(&config.masterName, "master-name", "", "redis sentinel master name")
	fs.StringVar(&config.ringAddrs, "ring-addrs", "", "comma-separated shard=addr pairs")
	fs.StringVar(&config.password, "password", "", "redis password")
	fs.IntVar(&config.db, "db", 0, "redis db number")
	fs.StringVar(&config.name, "name", "default", "pattern collection name")
	fs.BoolVar(&config.debug, "debug", false, "enable debug logging")
	fs.Usage = func() {}

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, nil, flag.ErrHelp
		}
		return nil, nil, err
	}

	config.name = strings.TrimSpace(config.name)
	if config.name == "" {
		config.name = "default"
	}

	ringAddrs, err := parseRingAddrs(config.ringAddrs)
	if err != nil {
		return nil, nil, err
	}

	addrs := parseCSV(config.addrs)
	if strings.TrimSpace(config.addrs) != "" && len(addrs) == 0 {
		return nil, nil, errors.New("addrs must contain at least one address")
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
	}, fs.Args(), nil
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

		pair := strings.SplitN(trimmed, "=", 2)
		if len(pair) != 2 {
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
	case "add":
		return runAdd, true, nil
	case "remove":
		return runRemove, true, nil
	case "find":
		return runFind, true, nil
	case "find-index":
		return runFindIndex, true, nil
	case "suggest":
		return runSuggest, true, nil
	case "suggest-index":
		return runSuggestIndex, true, nil
	case "info":
		return runInfo, false, nil
	case "flush":
		return runFlush, false, nil
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

func runAdd(stdout io.Writer, ac service, input string) error {
	count, err := ac.Add(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{"count": count})
}

func runRemove(stdout io.Writer, ac service, input string) error {
	count, err := ac.Remove(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{"count": count})
}

func runFind(stdout io.Writer, ac service, input string) error {
	matches, err := ac.Find(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string][]string{"matches": matches})
}

func runFindIndex(stdout io.Writer, ac service, input string) error {
	matches, err := ac.FindIndex(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]map[string][]int{"matches": matches})
}

func runSuggest(stdout io.Writer, ac service, input string) error {
	matches, err := ac.Suggest(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string][]string{"matches": matches})
}

func runSuggestIndex(stdout io.Writer, ac service, input string) error {
	matches, err := ac.SuggestIndex(input)
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]map[string][]int{"matches": matches})
}

func runInfo(stdout io.Writer, ac service, _ string) error {
	info, err := ac.Info()
	if err != nil {
		return err
	}
	return writeJSON(stdout, map[string]int{
		"keywords": info.Keywords,
		"nodes":    info.Nodes,
	})
}

func runFlush(stdout io.Writer, ac service, _ string) error {
	if err := ac.Flush(); err != nil {
		return err
	}
	return writeJSON(stdout, map[string]string{"status": "ok"})
}

func writeJSON(w io.Writer, value interface{}) error {
	encoder := json.NewEncoder(w)
	return encoder.Encode(value)
}
