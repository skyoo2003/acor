// SPDX-License-Identifier: Apache-2.0

package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// lines renders one declaration the way the snapshot would. declLines is the seam:
// it takes a parsed file, so a case is a source snippet rather than a fixture tree.
func lines(t *testing.T, src string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "x.go", "package acor\n"+src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return declLines(f)
}

func TestDeclLines(t *testing.T) {
	tests := []struct {
		name string
		src  string
		want []string
	}{{
		// The type of an untyped constant is its value, so the value has to be in
		// the line — see TestUntypedConstNarrowingIsVisible for what that catches.
		name: "untyped const records its value",
		src:  "const DefaultChunkSize = 1000",
		want: []string{"const DefaultChunkSize = 1000"},
	}, {
		name: "typed const records the type, not the value",
		src:  "type Mode int\nconst ModeFast Mode = 2",
		want: []string{"type Mode int", "const ModeFast Mode"},
	}, {
		// Only the first spec of an iota group names the type; the rest inherit it.
		// Recording it per spec as written would make the snapshot depend on order.
		name: "iota group inherits the declared type",
		src:  "type Mode int\nconst (\n\tModeA Mode = iota\n\tModeB\n\tModeC\n)",
		want: []string{"type Mode int", "const ModeA Mode", "const ModeB Mode", "const ModeC Mode"},
	}, {
		// The whole point of excluding var values: the promise covers sentinel
		// identity, not the message, so a reworded string must not fail the gate.
		name: "sentinel var stays name-only",
		src:  `var ErrNilArgs = errors.New("args must not be nil")`,
		want: []string{"var ErrNilArgs"},
	}, {
		name: "var with a declared type records it",
		src:  "var Registry map[string]int",
		want: []string{"var Registry map[string]int"},
	}, {
		name: "generic type records its constraints",
		src:  "type Box[T comparable] struct {\n\tValue T\n}",
		want: []string{"type Box[T comparable] struct", "field Box.Value T"},
	}, {
		name: "struct tag is part of the field",
		src:  "type R struct {\n\tStatus string `json:\"status\"`\n}",
		want: []string{"type R struct", "field R.Status string `json:\"status\"`"},
	}, {
		name: "unexported symbols are absent",
		src:  "func helper() {}\ntype cache struct{ N int }\nconst limit = 4",
		want: nil,
	}, {
		name: "unexported field of an exported struct is absent",
		src:  "type R struct {\n\tN int\n\tmu sync.Mutex\n}",
		want: []string{"type R struct", "field R.N int"},
	}, {
		// A method on an unexported type is unreachable from outside the package,
		// so it is not surface even though its own name is exported.
		name: "method needs an exported receiver",
		src:  "type cache struct{}\nfunc (c *cache) Get() error { return nil }",
		want: nil,
	}, {
		name: "method on an exported receiver",
		src:  "type R struct{}\nfunc (r *R) Get(ctx context.Context) error { return nil }",
		want: []string{"type R struct", "method (*R) Get(ctx context.Context) error"},
	}, {
		// An alias is the same type as its right-hand side, so the RHS is surface.
		// This is the line that catches a leak back into internal/.
		name: "alias expands its right-hand side",
		src:  "type Stats = internal.Stats",
		want: []string{"type Stats = internal.Stats"},
	}, {
		name: "interface methods are listed",
		src:  "type Logger interface {\n\tPrintf(format string, args ...any)\n\tunexported()\n}",
		want: []string{"type Logger interface", "method Logger.Printf(format string, args ...any)"},
	}, {
		name: "embedded exported field",
		src:  "type R struct {\n\tBase\n\tnoted\n}",
		want: []string{"type R struct", "field R.Base Base"},
	}, {
		// Rewrapping a declaration changes no API, so it must not change the line.
		// render prints against an empty FileSet for exactly this: the wrapped form
		// below has to come back identical to the one-line form above it.
		name: "wrapped declaration renders canonically",
		src:  "func New(\n\targs *Args,\n) (\n\t*R,\n\terror,\n) {\n\treturn nil, nil\n}",
		want: []string{"func New(args *Args) (*R, error)"},
	}, {
		name: "unwrapped declaration renders the same",
		src:  "func New(args *Args) (*R, error) { return nil, nil }",
		want: []string{"func New(args *Args) (*R, error)"},
	}}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lines(t, tt.src)
			if !slices.Equal(got, tt.want) {
				t.Errorf("declLines:\n got %q\nwant %q", got, tt.want)
			}
		})
	}
}

// The break this guards is silent: DefaultChunkSize = 1000 assigns to any numeric
// type and int64(1000) does not, and neither the name nor any signature moves.
func TestUntypedConstNarrowingIsVisible(t *testing.T) {
	before := lines(t, "const DefaultChunkSize = 1000")
	after := lines(t, "const DefaultChunkSize = int64(1000)")
	if slices.Equal(before, after) {
		t.Errorf("narrowing an untyped const left the snapshot unchanged: %q", before)
	}
}

// Same shape for a tag rename, which changes the wire name and nothing else.
func TestTagRenameIsVisible(t *testing.T) {
	before := lines(t, "type R struct {\n\tStatus string `json:\"status\"`\n}")
	after := lines(t, "type R struct {\n\tStatus string `json:\"state\"`\n}")
	if slices.Equal(before, after) {
		t.Errorf("renaming a json tag left the snapshot unchanged: %q", before)
	}
}

func writeGo(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// A package split across platforms has no single exported surface. apisnap unions
// every file, which would hide a removal only one GOOS sees, so it has to refuse
// instead — and that refusal is the one failure mode nothing else here would notice.
func TestSnapshotRejectsBuildConstrainedFiles(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "a.go", "package acor\n\nfunc Shared() {}\n")

	if _, err := snapshot(dir); err != nil {
		t.Fatalf("unconstrained package should snapshot cleanly: %v", err)
	}

	// A build tag no context satisfies, not an "_linux.go" suffix: that file is part
	// of the build on Linux, which is where CI runs, so the guard would never fire
	// there and this test would pass on a developer's Mac and fail in CI.
	writeGo(t, dir, "elsewhere.go", "//go:build apisnap_never\n\npackage acor\n\nfunc OnlyElsewhere() {}\n")
	_, err := snapshot(dir)
	if err == nil {
		t.Fatal("want an error for a platform-split package, got none")
	}
	if !strings.Contains(err.Error(), "build-constrained") {
		t.Errorf("error should name the cause, got: %v", err)
	}
}

// A cgo file is kept out of GoFiles rather than reported as constrained, so without
// its own check its exported symbols would disappear from a *passing* snapshot —
// the silent version of exactly what the constrained-file guard exists to prevent.
func TestSnapshotRejectsCgoFiles(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "a.go", "package acor\n\nfunc Shared() {}\n")
	writeGo(t, dir, "c.go", "package acor\n\n// #include <stdio.h>\nimport \"C\"\n\nfunc Cgo() {}\n")

	_, err := snapshot(dir)
	if err == nil {
		t.Fatal("want an error for a package with cgo files, got none")
	}
	if !strings.Contains(err.Error(), "c.go") {
		t.Errorf("error should name the file, got: %v", err)
	}
}

// A test file behind a build tag carries no exported surface, so refusing over one
// would fail the API gate for a reason that has nothing to do with the API.
func TestSnapshotToleratesConstrainedTestFiles(t *testing.T) {
	dir := t.TempDir()
	writeGo(t, dir, "a.go", "package acor\n\nfunc Shared() {}\n")
	writeGo(t, dir, "b_test.go", "//go:build integration\n\npackage acor\n\nfunc helper() {}\n")

	got, err := snapshot(dir)
	if err != nil {
		t.Fatalf("a build-tagged test file must not fail the gate: %v", err)
	}
	if !slices.Contains(got, "func Shared()") {
		t.Errorf("snapshot lost the package surface: %q", got)
	}
}
