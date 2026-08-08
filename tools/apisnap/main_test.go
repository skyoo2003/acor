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

// auditFixture writes the audit record plus a three-line const.go for its citations
// to resolve against, and returns the record's path and the roots to look under.
// The cited file is real so the resolution check is exercised rather than stubbed:
// every citation in these cases either lands inside it or is meant not to.
func auditFixture(t *testing.T, audit string) (path string, roots []string) {
	t.Helper()
	dir := t.TempDir()
	writeGo(t, dir, "const.go", "package p\n\nconst Answer = 42\n")
	path = filepath.Join(dir, "v1-audit.txt")
	if err := os.WriteFile(path, []byte(audit), 0o600); err != nil {
		t.Fatal(err)
	}
	return path, []string{dir}
}

// TestAuditProblems covers the ways the verdict record can stop being a record of
// the surface. The uncited case is the one with the most to catch: a verdict
// nobody can re-check reads as evidence without being any. The two unresolvable
// citations are next: a note pointing at a file that is gone, or past the end of
// one that shrank, is how a record rots without anyone editing it.
func TestAuditProblems(t *testing.T) {
	surface := []string{"const Answer = 42", "type Widget struct"}

	tests := []struct {
		name  string
		audit string
		want  string
	}{
		{
			name:  "complete",
			audit: "# header\nconst Answer = 42\tok\tconst.go:3\ntype Widget struct\tunaudited\n",
		},
		{
			name:  "entry with no verdict",
			audit: "const Answer = 42\tok\tconst.go:3\n",
			want:  `no verdict for "type Widget struct"`,
		},
		{
			name:  "verdict for an entry that is gone",
			audit: "const Answer = 42\tok\tconst.go:3\ntype Widget struct\tunaudited\nfunc Removed()\tok\tconst.go:1\n",
			want:  `"func Removed()" is not in api/v1.txt`,
		},
		{
			name:  "verdict outside the vocabulary",
			audit: "const Answer = 42\tprobably-fine\tconst.go:3\ntype Widget struct\tunaudited\n",
			want:  `has verdict "probably-fine"`,
		},
		{
			name:  "audited but uncited",
			audit: "const Answer = 42\tok\tlooks right to me\ntype Widget struct\tunaudited\n",
			want:  `with no file:line in its note`,
		},
		{
			name:  "same entry judged twice",
			audit: "const Answer = 42\tok\tconst.go:1\nconst Answer = 42\trisk\tconst.go:3\ntype Widget struct\tunaudited\n",
			want:  "already has a verdict on line 1",
		},
		{
			name:  "cites a file that no longer exists",
			audit: "const Answer = 42\tok\tmoved.go:1\ntype Widget struct\tunaudited\n",
			want:  "cites moved.go, which is not a file under",
		},
		{
			name:  "cites past the end of the file",
			audit: "const Answer = 42\tok\tconst.go:99\ntype Widget struct\tunaudited\n",
			want:  "cites const.go:99, but const.go has 3 lines",
		},
		{
			// The start resolves and the end does not, which is what a shrinking file
			// does to a span citation.
			name:  "cites a range that runs past the end",
			audit: "const Answer = 42\tok\tconst.go:1-99\ntype Widget struct\tunaudited\n",
			want:  "cites const.go:99, but const.go has 3 lines",
		},
		{
			// An unaudited entry carries no note, but a stale citation on one is still
			// a stale citation, so the resolution check does not skip it.
			name:  "unaudited entry with a rotted citation",
			audit: "const Answer = 42\tok\tconst.go:1\ntype Widget struct\tunaudited\tsee gone.go:1\n",
			want:  "cites gone.go, which is not a file under",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, roots := auditFixture(t, tc.audit)

			_, problems := auditProblems(surface, path, roots)
			if tc.want == "" {
				if len(problems) > 0 {
					t.Fatalf("a complete record was rejected: %v", problems)
				}
				return
			}
			if len(problems) == 0 {
				t.Fatalf("no problem reported; want one mentioning %q", tc.want)
			}
			if !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, tc.want) }) {
				t.Errorf("problems = %v, want one mentioning %q", problems, tc.want)
			}
		})
	}
}

// TestAuditTallyCountsAudited pins what milestone 4 of the readiness PRD reads off
// this gate: the split between reviewed and not. A record that miscounts is worse
// than none, because the number is what the release decision rests on.
func TestAuditTallyCountsAudited(t *testing.T) {
	surface := []string{"const A = 1", "const B = 2", "const C = 3"}
	audit := "const A = 1\tok\tconst.go:1\nconst B = 2\tfixed\tconst.go:2\nconst C = 3\tunaudited\n"

	path, roots := auditFixture(t, audit)

	tally, problems := auditProblems(surface, path, roots)
	if len(problems) > 0 {
		t.Fatalf("unexpected problems: %v", problems)
	}
	if tally["ok"] != 1 || tally["fixed"] != 1 || tally["unaudited"] != 1 || tally["risk"] != 0 {
		t.Errorf("tally = %v, want 1 ok, 1 fixed, 0 risk, 1 unaudited", tally)
	}
	if got := tallyLine(tally); got != "1 ok, 1 fixed, 0 risk, 1 unaudited" {
		t.Errorf("tallyLine = %q, want the verdicts in declared order", got)
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
