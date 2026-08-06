// SPDX-License-Identifier: Apache-2.0

// Command licensesnap prints the NOTICE file: ACOR's own attribution followed by
// the copyright notice and verbatim license text of every third-party module
// linked into the acor binary, plus the Go standard library and runtime that
// every Go binary statically links.
//
// The output is committed as NOTICE, which is what makes the attribution an
// artifact CI can diff. The MIT and BSD licenses ACOR's dependencies carry all
// require that their copyright notice and full license text be reproduced in
// binary redistributions, so the generated file is shipped inside the release
// archives and the container images, not just kept in the repository.
//
// Scope is ./cmd/acor, the only binary this project distributes. The server/
// module is a source-only library: a consumer runs `go get` and the go command
// fetches its dependencies from the proxy with their own LICENSE and NOTICE
// files intact, so this project never redistributes them and owes no notice for
// them. benchmarks/ never leaves the repository.
//
// The import graph is walked once per operating system in releaseGOOS, because
// `go list` resolves build constraints against GOOS: a Windows-only import of a
// module the Linux build never sees would otherwise ship unattributed.
//
// License texts are copied byte-for-byte rather than summarized, because
// reproducing the text is the obligation. PATENTS and NOTICE files are copied
// too when a module has them: golang.org/x/sync and the Go runtime both carry an
// additional patent grant that has to travel with them.
//
// SPDX identifiers come from licenses below and are never inferred from the
// license text. A wrong identifier in this file is a wrong legal claim, and
// distinguishing MIT from BSD-2-Clause from ISC by pattern matching is exactly
// where inference gets it wrong. An unmapped module is a hard error, so adding a
// dependency fails the build until someone reads its license and records it, and
// each identifier is pinned to a digest of the text it was read from, so a
// version bump that rewrites a license cannot quietly keep the old identifier.
//
// Usage: go run ./tools/licensesnap   (from the repository root)
package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const (
	target     = "./cmd/acor"
	selfModule = "github.com/skyoo2003/acor"

	// goStdlib stands in for the Go standard library and runtime, which has no
	// module path because `go list` reports no module for standard-library
	// packages at all. See goRuntime.
	goStdlib = "go (standard library)"

	// moduleFields is the number of tab-separated fields the go list format below
	// emits per module: path, version, cache directory.
	moduleFields = 3
	ruleWidth    = 80

	header = `ACOR - Aho-Corasick automaton working On Redis
Copyright 2016-2026 Sungkyu Yoo

Licensed under the Apache License, Version 2.0 (see LICENSE).
`

	preamble = `The compiled ` + "`acor`" + ` binary bundles the third-party software listed below.
Each component remains under its own license, reproduced here in full.
`
)

// license is the SPDX identifier read by hand from a module's license file, plus
// the SHA-256 of the exact text it was read from. The digest is what makes a
// version bump safe to accept: a dependency whose license file is unchanged
// passes silently, and one that rewrote its license is a hard error rather than a
// large diff someone might wave through.
type license struct {
	id     string
	digest string
}

// licenses maps a module path to its approved license. Deliberately not inferred
// from the text — see the package doc.
var licenses = map[string]license{
	goStdlib:                       {"BSD-3-Clause", "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad"},
	"github.com/cespare/xxhash/v2": {"MIT", "f566a9f97bacdaf00d9f21dd991e81dc11201c4e016c86b470799429a1c9a79c"},
	"github.com/redis/go-redis/v9": {"BSD-2-Clause", "a3a7dff87da3927db65cb4c87b1cfbc96ca2755704461a485d457be7ae300a86"},
	"go.uber.org/atomic":           {"MIT", "edbb5a4d165ac69376c765b551c0662ff42bea87e1f1eda85f42ac90c34b09d0"},
	"golang.org/x/sync":            {"BSD-3-Clause", "911f8f5782931320f5b8d1160a76365b83aea6447ee6c04fa6d5591467db9dad"},
}

// releaseGOOS are the operating systems .goreleaser.yaml builds for. GOARCH is
// pinned rather than swept: build tags select files within a module, not
// different modules, so a module that appears on only one architecture is not a
// thing that happens.
// ponytail: sweep GOARCH here too if that ever stops being true.
var releaseGOOS = []string{"darwin", "linux", "windows"}

// licenseNames are the filenames a module may use for its license, in the order
// they are tried. Upstream is inconsistent: go-redis and x/sync use LICENSE,
// xxhash and go.uber.org/atomic use LICENSE.txt.
var licenseNames = []string{"LICENSE", "LICENSE.txt", "LICENSE.md", "COPYING"}

// extraNames are additional files that carry terms and so must be reproduced
// alongside the license. The PATENTS shipped by x/sync and by the Go runtime is
// an additional IP grant; a NOTICE belonging to a dependency has to be propagated
// verbatim under Apache-2.0 4(d). No current dependency ships a non-empty NOTICE,
// but a future one will.
var extraNames = []string{"PATENTS", "NOTICE"}

type module struct {
	path    string
	version string
	dir     string
}

// title is the module heading: "path version", or just the path for a component
// that has no version to report.
func (m module) title() string {
	if m.version == "" {
		return m.path
	}
	return m.path + " " + m.version
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "licensesnap:", err)
		os.Exit(1)
	}
}

func run() error {
	mods, err := modules()
	if err != nil {
		return err
	}

	out := &strings.Builder{}
	out.WriteString(header)
	fmt.Fprintf(out, "\n%s\nTHIRD-PARTY SOFTWARE\n%s\n\n%s", rule('='), rule('='), preamble)

	for _, m := range mods {
		lic, ok := licenses[m.path]
		if !ok {
			return fmt.Errorf("unknown license for %s; read its license file and add it to licenses in tools/licensesnap/main.go", m.path)
		}

		text, name, err := licenseText(m)
		if err != nil {
			return err
		}
		sum := sha256.Sum256([]byte(text))
		if got := hex.EncodeToString(sum[:]); got != lic.digest {
			return fmt.Errorf("license text for %s changed since %s was approved; re-read %s, then update its digest in tools/licensesnap/main.go to %s",
				m.path, lic.id, filepath.Join(m.dir, name), got)
		}

		fmt.Fprintf(out, "\n%s\n%s\nSPDX-License-Identifier: %s\n%s\n\n", rule('-'), m.title(), lic.id, rule('-'))
		out.WriteString(indent(text))

		for _, extra := range extraNames {
			// Path comes from `go list`, not from user input.
			b, err := os.ReadFile(filepath.Join(m.dir, extra)) //nolint:gosec
			// Any read error other than absence is a term this file was
			// supposed to reproduce and now silently would not, so it fails
			// rather than continues.
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("read %s for %s: %w", extra, m.path, err)
			}
			// Absent is the common case, and an empty NOTICE is what
			// google.golang.org/grpc ships — neither is worth a heading.
			if strings.TrimSpace(string(b)) == "" {
				continue
			}
			fmt.Fprintf(out, "\n%s (%s):\n\n", extra, m.path)
			out.WriteString(indent(string(b)))
		}
	}

	// Checked, unlike fmt.Print: stdout is redirected into the file that becomes
	// NOTICE, so a short write here would otherwise exit 0 with a truncated
	// attribution — the very thing the temp file in the Makefile guards against.
	_, writeErr := os.Stdout.WriteString(out.String())
	return writeErr
}

// modules returns everything the target binary bundles: the third-party modules
// linked into it, plus the Go standard library, sorted by path and deduplicated.
// `go list -deps` walks the import graph of the built package, so it reports what
// actually ends up in the binary rather than what go.mod requires: go.uber.org/atomic
// is an indirect requirement that does get linked (through go-redis internals),
// while miniredis and gopher-lua are direct requirements that do not, being
// test-only.
func modules() ([]module, error) {
	seen := map[string]bool{}
	var mods []module
	for _, goos := range releaseGOOS {
		found, err := listModules(goos)
		if err != nil {
			return nil, err
		}
		for _, m := range found {
			if seen[m.path] {
				continue
			}
			seen[m.path] = true
			mods = append(mods, m)
		}
	}
	if len(mods) == 0 {
		// Never a legitimate result: the binary links go-redis at minimum. An
		// empty list means `go list` reported nothing useful, and silently
		// writing a NOTICE with no third-party section would drop every
		// attribution at once.
		return nil, fmt.Errorf("no third-party modules found for %s", target)
	}

	goMod, err := goRuntime()
	if err != nil {
		return nil, err
	}
	mods = append(mods, goMod)

	sort.Slice(mods, func(i, j int) bool { return mods[i].path < mods[j].path })
	return mods, nil
}

// listModules returns the third-party modules the target binary links when built
// for goos.
func listModules(goos string) ([]module, error) {
	const format = `{{if and .Module (not .Standard)}}{{.Module.Path}}	{{.Module.Version}}	{{.Module.Dir}}{{end}}`

	cmd := exec.CommandContext(context.Background(), "go", "list", "-deps", "-f", format, target)
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64")
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list -deps %s (GOOS=%s): %w", target, goos, err)
	}

	var mods []module
	for line := range strings.SplitSeq(string(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) != moduleFields {
			return nil, fmt.Errorf("unexpected go list output: %q", line)
		}
		m := module{path: fields[0], version: fields[1], dir: fields[2]}
		if m.path == selfModule {
			continue
		}
		// Empty for a module replaced by a local directory, and for one that is
		// not in the cache. Either way there is no license file to read, and
		// guessing is not an option.
		if m.dir == "" {
			return nil, fmt.Errorf("no module cache directory for %s; run `go mod download`", m.path)
		}
		mods = append(mods, m)
	}
	return mods, nil
}

// goRuntime returns the Go standard library and runtime as a module entry. Every
// acor binary statically links compiled runtime and standard-library code, and
// the BSD license that code carries requires its copyright notice accompany
// binary redistributions. Those files live in GOROOT, which `go list` never
// reports: standard-library packages have no module at all.
//
// No version is recorded, deliberately. The license text is the thing that has to
// be reproduced, and stamping the toolchain version would make NOTICE differ
// between the 1.25 and 1.26 CI legs and so fail its own diff gate. The text is
// identical from go1.24 through go1.26; if a future release rewrites it, the
// digest check in run reports that as an error naming the file to re-read.
func goRuntime() (module, error) {
	out, err := exec.CommandContext(context.Background(), "go", "env", "GOROOT").Output()
	if err != nil {
		return module{}, fmt.Errorf("go env GOROOT: %w", err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return module{}, errors.New("go env GOROOT is empty; cannot read the Go license")
	}
	return module{path: goStdlib, dir: dir}, nil
}

// licenseText returns a module's license text and the filename it came from.
func licenseText(m module) (text, filename string, err error) {
	for _, name := range licenseNames {
		// Path comes from `go list`, not from user input.
		b, readErr := os.ReadFile(filepath.Join(m.dir, name)) //nolint:gosec
		if readErr == nil {
			return string(b), name, nil
		}
		// A license that is present but unreadable is not the same as one that
		// is absent, and reporting it as absent would send the reader looking
		// for a file that is sitting right there.
		if !errors.Is(readErr, os.ErrNotExist) {
			return "", "", fmt.Errorf("read %s for %s: %w", name, m.path, readErr)
		}
	}
	return "", "", fmt.Errorf("no license file in %s for %s (tried %s)", m.dir, m.path, strings.Join(licenseNames, ", "))
}

// indent offsets a verbatim license text by four spaces so it is unambiguous
// where an upstream text starts and stops. Blank lines stay empty rather than
// becoming trailing whitespace, which the trailing-whitespace and
// end-of-file-fixer hooks would otherwise rewrite on commit and break the gate.
// The carriage return of a CRLF upstream text is trimmed for the same reason:
// .gitattributes normalizes NOTICE to LF, so leaving it in would make the file
// differ from its own regenerated form on every run.
func indent(s string) string {
	var b strings.Builder
	for line := range strings.SplitSeq(strings.TrimRight(s, "\n"), "\n") {
		if line = strings.TrimRight(line, " \t\r"); line == "" {
			b.WriteString("\n")
			continue
		}
		b.WriteString("    " + line + "\n")
	}
	return b.String()
}

func rule(c byte) string { return strings.Repeat(string(c), ruleWidth) }
