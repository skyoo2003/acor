// SPDX-License-Identifier: Apache-2.0

// Command doccheck compiles Go code blocks in Markdown docs against the real
// packages, so documented examples can't silently drift from the API.
//
// Only blocks opted in with an HTML comment on the line before the fence are
// checked (most doc snippets are illustrative fragments and won't compile):
//
//	<!-- doccheck -->          compile against the main module (github.com/skyoo2003/acor)
//	<!-- doccheck:server -->   compile against the server module
//
// A block that already declares `package` is compiled verbatim; otherwise it is
// wrapped in a function with a small preamble that predeclares common
// identifiers (ac, ctx, keywords, ...). Imports are inferred from the selectors
// the block uses. Temp files go under a dot-dir the go tool ignores.
//
// Usage: go run ./tools/doccheck <markdown files...>
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	repoImport   = "github.com/skyoo2003/acor/pkg/acor"
	tmpDir       = ".doccheck"
	markerMain   = "<!-- doccheck -->"
	markerServer = "<!-- doccheck:server -->"
)

// selector -> import path. Inferred imports are added only when the block
// references the selector, so we never emit an unused import.
var imports = map[string]string{
	"fmt":       "fmt",
	"os":        "os",
	"strings":   "strings",
	"time":      "time",
	"log":       "log",
	"errors":    "errors",
	"sync":      "sync",
	"http":      "net/http",
	"health":    "github.com/skyoo2003/acor/server/health",
	"logging":   "github.com/skyoo2003/acor/server/logging",
	"tracing":   "github.com/skyoo2003/acor/server/tracing",
	"metrics":   "github.com/skyoo2003/acor/server/metrics",
	"miniredis": "github.com/alicebob/miniredis/v2",
	"redis":     "github.com/redis/go-redis/v9",
}

// preamble predeclares identifiers a fragment may reference without defining.
// acor and context are always imported and used by these declarations, so the
// wrapped file always has at least those two imports satisfied.
const preamble = `var (
	ac        *acor.AhoCorasick
	ctx       context.Context
	text      string
	largeText string
	keywords  []string
	texts     []string
	traceID   string
	spanID    string
)
`

type block struct {
	file   string
	line   int
	server bool
	code   string
}

var fenceRe = regexp.MustCompile("^```go\\s*$")

func extract(path string) ([]block, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(string(data), "\n")
	var blocks []block
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		server := trimmed == markerServer
		if trimmed != markerMain && !server {
			continue
		}
		// Find the opening fence on the next non-empty line.
		j := i + 1
		for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
			j++
		}
		if j >= len(lines) || !fenceRe.MatchString(lines[j]) {
			return nil, fmt.Errorf("%s:%d: %s not followed by a ```go block", path, i+1, trimmed)
		}
		start := j + 1
		end := start
		for end < len(lines) && strings.TrimSpace(lines[end]) != "```" {
			end++
		}
		if end == len(lines) {
			return nil, fmt.Errorf("%s:%d: ```go block is missing a closing ```", path, j+1)
		}
		blocks = append(blocks, block{file: path, line: start, server: server,
			code: strings.Join(lines[start:end], "\n")})
		i = end
	}
	return blocks, nil
}

// wrap turns a code block into a compilable Go source file.
func wrap(code string) string {
	if regexp.MustCompile(`(?m)^package `).MatchString(code) {
		return code // already a full file
	}
	var imp strings.Builder
	imp.WriteString("import (\n")
	// context and acor are always used by the preamble; the rest are inferred
	// from the selectors the block references, so we never emit an unused one.
	imp.WriteString("\t\"context\"\n")
	imp.WriteString("\t\"" + repoImport + "\"\n")
	for sel, path := range imports {
		if regexp.MustCompile(`\b` + sel + `\.`).MatchString(code) {
			imp.WriteString("\t\"" + path + "\"\n")
		}
	}
	imp.WriteString(")\n")
	return "package doccheck\n" + imp.String() + preamble +
		"func run() {\n" + code + "\n}\n"
}

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: doccheck <markdown files...>")
		os.Exit(2)
	}

	var blocks []block
	for _, path := range os.Args[1:] {
		bs, err := extract(path)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		blocks = append(blocks, bs...)
	}
	if len(blocks) == 0 {
		fmt.Fprintln(os.Stderr, "doccheck: no <!-- doccheck --> blocks found")
		os.Exit(1)
	}

	// Clean up any leftovers, and on exit.
	_ = os.RemoveAll(tmpDir)
	_ = os.RemoveAll(filepath.Join("server", tmpDir))
	defer os.RemoveAll(tmpDir)
	defer os.RemoveAll(filepath.Join("server", tmpDir))

	failed := 0
	for i, b := range blocks {
		root := "."
		if b.server {
			root = "server"
		}
		dir := filepath.Join(root, tmpDir, fmt.Sprintf("doc%d", i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(wrap(b.code)), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		cmd := exec.Command("go", "build", "-o", os.DevNull, ".")
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		where := fmt.Sprintf("%s:%d", b.file, b.line)
		if err != nil {
			failed++
			fmt.Printf("FAIL %s\n%s\n", where, indent(string(out)))
		} else {
			fmt.Printf("ok   %s\n", where)
		}
	}
	if failed > 0 {
		fmt.Printf("\ndoccheck: %d of %d block(s) failed\n", failed, len(blocks))
		os.Exit(1)
	}
	fmt.Printf("\ndoccheck: all %d block(s) compiled\n", len(blocks))
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "    " + l
	}
	return strings.Join(lines, "\n")
}
