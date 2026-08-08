// SPDX-License-Identifier: Apache-2.0

// Command apisnap prints the exported surface of pkg/acor as sorted text, one
// symbol per line, so the v1 compatibility promise has an artifact CI can diff.
//
// The output is committed as api/v1.txt, which makes a public API change visible
// in the pull request that causes it: adding a symbol appends a line, removing
// one deletes a line. Nothing here prevents a removal — it only makes one
// impossible to do quietly, which is what "any bypass requires explicit recorded
// approval" means in practice.
//
// Struct fields and interface methods are included because the promise covers
// them specifically: structs ACOR returns may gain fields, and exported
// interfaces may not gain methods at all (docs/content/reference/compatibility.md).
// Field tags come along with the fields: a json tag is the name a field goes out
// under, so renaming one breaks a caller without changing any Go signature.
// Type aliases print their right-hand side, so an alias reaching back into an
// internal package also shows up here.
//
// Doc comments are deliberately absent, and so is the value of anything carrying a
// declared type. Documentation wording and error strings are explicitly not covered,
// so recording those would fail the gate on every rewording. An untyped constant is
// the one exception, because there the value is the type: DefaultChunkSize = 1000
// assigns to any numeric type and = int64(1000) does not. Untyped vars stay
// name-only, since a sentinel's initializer is errors.New("...") and its static type
// is not something a caller can hold onto.
//
// The snapshot says the surface did not change; it cannot say anyone checked that
// the surface still behaves as its godoc claims. compatibility.md freezes that
// wording too and admits it is "enforced by review only". So this also gates
// api/v1-audit.txt, which carries one verdict per line of the snapshot, and fails
// when an entry has none — an entry nobody recorded a verdict for is then a build
// failure rather than an omission nobody notices. Recording "unaudited" clears the
// gate, so what this buys is that unreviewed entries are counted out loud, not that
// they block the build.
//
// A cited file:line is resolved, not just pattern-matched: the file has to exist
// and the line has to be inside it. That does not prove the line still says what
// the note claims — code moves under a citation without changing the file's
// length — but it does catch the citation that outlived its file.
//
// Usage: go run ./tools/apisnap   (from the repository root; writes api/v1.txt)
package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
)

const (
	pkgDir     = "pkg/acor"
	pkgImport  = "github.com/skyoo2003/acor/pkg/acor"
	snapFile   = "api/v1.txt"
	auditFile  = "api/v1-audit.txt"
	regenerate = "make api-check"
	// Applies only when creating the file, which git already tracks and checks out
	// with its own mode. It is here to satisfy WriteFile, not to protect anything.
	snapMode = 0o600
)

// verdicts are the only values api/v1-audit.txt may carry. "unaudited" is legal
// on purpose: the release can ship with part of the surface unreviewed, but not
// with that fact hidden, so the gate counts it rather than rejecting it.
var verdicts = []string{"ok", "fixed", "risk", "unaudited"}

// noteCol is the zero-based column the note starts at: entry, verdict, then note.
const noteCol = 2

// citation matches the file:line an audited verdict has to point at. A verdict
// with nowhere to look is the failure mode the record exists to avoid: 223 lines
// of "ok" that nobody read are worse than no record, because they read as evidence.
//
// Restricted to *.go so the match can be resolved against a real file. Matching any
// token with a colon and a digit in it accepted "0-15" style prose as a citation.
// The optional end of a range is captured too: a "file.go:99-111" whose 111 is past
// the end of the file is exactly the drift worth catching, and checking only the
// start would miss it.
var citation = regexp.MustCompile(`([\w./-]+\.go):(\d+)(?:-(\d+))?`)

// citeRoots are the directories a citation's file is looked up under, in order. The
// record cites by base name ("acor.go:271"), not by repo-relative path.
var citeRoots = []string{pkgDir, "internal/engine", "tools/apisnap", "."}

// resolveCitation reports the problem with a cited file:line (or file:start-end,
// where last is the end), or "" if it resolves. Line counts are cached because one
// file carries dozens of citations.
func resolveCitation(file string, line, last int, roots []string, lineCount map[string]int) string {
	n, ok := lineCount[file]
	if !ok {
		n = countLines(file, roots)
		lineCount[file] = n
	}
	switch {
	case n < 0:
		return fmt.Sprintf("cites %s, which is not a file under %v", file, roots)
	case line < 1 || line > n || last > n:
		return fmt.Sprintf("cites %s:%d, but %s has %d lines", file, max(line, last), file, n)
	}
	return ""
}

// auditColumns splits one record line into its three columns. A missing verdict or
// note comes back empty, which the caller reports; columns past the note are note
// text that happened to contain a tab.
func auditColumns(line string) (entry, verdict, note string) {
	cols := strings.Split(line, "\t")
	entry = cols[0]
	if len(cols) > 1 {
		verdict = cols[1]
	}
	if len(cols) > noteCol {
		note = strings.Join(cols[noteCol:], "\t")
	}
	return entry, verdict, note
}

// citationProblems resolves every citation matched in one note and returns what did
// not resolve, phrased to follow the quoted entry name.
func citationProblems(cites [][]string, roots []string, lineCount map[string]int) []string {
	var bad []string
	for _, c := range cites {
		line, err := strconv.Atoi(c[2])
		if err != nil {
			continue // \d+ too long for an int; not a citation anyone wrote
		}
		last, _ := strconv.Atoi(c[3]) // absent or overlong parses to 0, which never trips the check
		if problem := resolveCitation(c[1], line, last, roots, lineCount); problem != "" {
			bad = append(bad, problem)
		}
	}
	return bad
}

// countLines returns the number of lines in the first of roots that holds file, or
// -1 if none does. A citation names a path inside the repository, so one that
// escapes upward or is absolute resolves to nothing rather than being read.
func countLines(file string, roots []string) int {
	rel := filepath.Clean(file)
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return -1
	}
	for _, root := range roots {
		data, err := os.ReadFile(filepath.Join(root, rel)) // #nosec G304,G703 -- rel is checked above to stay inside root
		if err != nil {
			continue
		}
		n := bytes.Count(data, []byte("\n"))
		if len(data) > 0 && !bytes.HasSuffix(data, []byte("\n")) {
			n++ // a file with no trailing newline still has a last line
		}
		return n
	}
	return -1
}

func main() {
	lines, err := snapshot(pkgDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "apisnap:", err)
		os.Exit(1)
	}

	out := &strings.Builder{}
	fmt.Fprintf(out, "# Public API surface of %s, frozen for the v1 line.\n", pkgImport)
	fmt.Fprintf(out, "# Generated by tools/apisnap. Regenerate with: %s\n", regenerate)
	fmt.Fprintf(out, "# A deleted line is a breaking change. See docs/content/reference/compatibility.md.\n")
	fmt.Fprintf(out, "# Each line's godoc is frozen too; %s records whether it was verified.\n", auditFile)
	for _, l := range lines {
		fmt.Fprintln(out, l)
	}
	// Written here rather than piped: a shell redirect empties the committed
	// snapshot before the tool even runs, so any failure above would leave a
	// truncated file behind and a diff that says the whole API was removed.
	if err := os.WriteFile(snapFile, []byte(out.String()), snapMode); err != nil {
		fmt.Fprintln(os.Stderr, "apisnap:", err)
		os.Exit(1)
	}

	// After the snapshot, not instead of it: a surface change still has to show up
	// in the api/v1.txt diff even when the audit is the thing that fails.
	tally, problems := auditProblems(lines, auditFile, citeRoots)
	for _, p := range problems {
		fmt.Fprintln(os.Stderr, "apisnap:", p)
	}
	if len(problems) > 0 {
		os.Exit(1)
	}
	fmt.Printf("apisnap: audit %d/%d entries — %s\n", len(lines)-tally["unaudited"], len(lines), tallyLine(tally))
}

// auditProblems checks that api/v1-audit.txt carries exactly one usable verdict
// per snapshot entry. api/v1.txt proves the surface did not change; this proves
// somebody looked at every line of it. compatibility.md freezes documented
// behavior and admits it is "enforced by review only" — this is that review,
// made re-runnable.
//
// Returns every problem rather than the first, so one run names all of them.
func auditProblems(entries []string, path string, roots []string) (tally map[string]int, problems []string) {
	// #nosec G304 -- path is the auditFile constant and roots is citeRoots in
	// production; both are parameters only so the tests can point at a temp tree.
	data, err := os.ReadFile(path)
	if err != nil {
		// Not "regenerate with make api-check": nothing generates this file. It is
		// written by hand, one line per entry of the snapshot, so re-running the gate
		// would only report the same absence again.
		return nil, []string{fmt.Sprintf("%v; add it by hand, one line per entry of %s", err, snapFile)}
	}

	tally = map[string]int{}
	seen := map[string]int{}
	lineCount := map[string]int{}
	for i, line := range strings.Split(string(data), "\n") {
		lineNo := i + 1
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		entry, verdict, note := auditColumns(line)
		cites := citation.FindAllStringSubmatch(note, -1)
		switch {
		case !slices.Contains(verdicts, verdict):
			problems = append(problems, fmt.Sprintf(
				"%s:%d: %q has verdict %q; want one of %v, tab-separated", path, lineNo, entry, verdict, verdicts))
		case verdict != "unaudited" && len(cites) == 0:
			// The note is what makes a verdict re-checkable by the next reader.
			problems = append(problems, fmt.Sprintf(
				"%s:%d: %q is %q with no file:line in its note; cite where the behavior was read", path, lineNo, entry, verdict))
		default:
			tally[verdict]++
		}
		// Every citation is resolved, whatever the verdict: a note that outlived the
		// file it points at is the record going stale, which is the thing this gate
		// is for. Checked outside the switch so a bad verdict does not hide it.
		for _, bad := range citationProblems(cites, roots, lineCount) {
			problems = append(problems, fmt.Sprintf("%s:%d: %q %s", path, lineNo, entry, bad))
		}
		if prev, dup := seen[entry]; dup {
			problems = append(problems, fmt.Sprintf("%s:%d: %q already has a verdict on line %d", path, lineNo, entry, prev))
		}
		seen[entry] = lineNo
	}

	for _, e := range entries {
		if _, ok := seen[e]; !ok {
			problems = append(problems, fmt.Sprintf("%s: no verdict for %q; every entry of %s needs one", path, e, snapFile))
		}
	}
	for entry := range seen {
		if !slices.Contains(entries, entry) {
			problems = append(problems, fmt.Sprintf("%s: %q is not in %s; the entry was removed or the line is misspelled", path, entry, snapFile))
		}
	}
	sort.Strings(problems)
	return tally, problems
}

// tallyLine renders the per-verdict counts in the fixed order of verdicts, so the
// output reads the same on every run and diffs cleanly in CI logs.
func tallyLine(tally map[string]int) string {
	parts := make([]string, 0, len(verdicts))
	for _, v := range verdicts {
		parts = append(parts, fmt.Sprintf("%d %s", tally[v], v))
	}
	return strings.Join(parts, ", ")
}

func snapshot(dir string) ([]string, error) {
	// go/build applies the same filename and //go:build rules the go command does,
	// so GoFiles is the package as the compiler sees it: _test.go files and anything
	// constrained out are separated for us rather than filtered by hand here.
	p, err := build.ImportDir(dir, 0)
	if err != nil {
		return nil, err
	}
	if skipped := unsnapshottable(p); len(skipped) > 0 {
		return nil, fmt.Errorf(
			"%s: build-constrained or cgo files %v are outside GoFiles; apisnap reads one build context, so teach it build contexts first",
			dir, skipped)
	}

	fset := token.NewFileSet()
	var lines []string
	for _, name := range p.GoFiles {
		// parser.ParseDir would be shorter but returns the deprecated ast.Package,
		// and the surface spans files anyway — nothing here needs them grouped.
		f, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, err
		}
		lines = append(lines, declLines(f)...)
	}

	sort.Strings(lines)
	return dedupe(lines), nil
}

// unsnapshottable names the .go files go/build kept out of GoFiles that would make
// the snapshot describe something other than the package. A package split across
// platforms has no single exported surface, and the union of every file hides a
// removal only one GOOS can see; a cgo file is dropped from GoFiles entirely, so its
// exported symbols would vanish behind a green gate. Refuse in both cases.
//
// Test files are exempt on purpose: they carry no exported surface, so a
// //go:build integration on one is not a reason to fail the API gate.
func unsnapshottable(p *build.Package) []string {
	skipped := slices.Clone(p.CgoFiles)
	for _, name := range p.IgnoredGoFiles {
		if !strings.HasSuffix(name, "_test.go") {
			skipped = append(skipped, name)
		}
	}
	return skipped
}

func declLines(f *ast.File) []string {
	var lines []string
	for _, decl := range f.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if l, ok := funcLine(d); ok {
				lines = append(lines, l)
			}
		case *ast.GenDecl:
			lines = append(lines, genLines(d)...)
		}
	}
	return lines
}

func funcLine(d *ast.FuncDecl) (string, bool) {
	if !d.Name.IsExported() {
		return "", false
	}
	sig := signature(d.Type)
	if d.Recv == nil {
		return "func " + d.Name.Name + sig, true
	}
	// A method is only part of the surface if its receiver is too: a method on an
	// unexported type is unreachable from outside the package.
	recv := d.Recv.List[0].Type
	if !ast.IsExported(baseName(recv)) {
		return "", false
	}
	return fmt.Sprintf("method (%s) %s%s", render(recv), d.Name.Name, sig), true
}

func genLines(d *ast.GenDecl) []string {
	var lines []string
	// Only the first spec of an iota group declares the type; the rest inherit it.
	// Recording the type per spec as written would make the snapshot depend on
	// declaration order — reordering a const block would move the annotation from
	// one line to another, and deleting the first constant would silently drop the
	// type from every constant under it.
	groupType := ""
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.ValueSpec:
			kind := "var"
			if d.Tok == token.CONST {
				kind = "const"
			}
			inferred := false
			switch {
			case s.Type != nil:
				groupType = render(s.Type)
			case len(s.Values) > 0:
				// Own value, so the type is inferred from it rather than inherited.
				groupType = ""
				inferred = true
			}
			for i, n := range s.Names {
				if !n.IsExported() {
					continue
				}
				line := kind + " " + n.Name
				switch {
				case groupType != "":
					// The declared type only, never the value. Sentinel error
					// identity is promised; the message text explicitly is not.
					line += " " + groupType
				case inferred && d.Tok == token.CONST && i < len(s.Values):
					// Nothing declares this constant's type, so the value is the
					// only thing that does: 1000 assigns to any numeric type and
					// int64(1000) does not. Recording the name alone would let
					// that break through the gate silently.
					line += " = " + render(s.Values[i])
				}
				lines = append(lines, line)
			}
		case *ast.TypeSpec:
			if !s.Name.IsExported() {
				continue
			}
			lines = append(lines, typeLines(s)...)
		}
	}
	return lines
}

func typeLines(s *ast.TypeSpec) []string {
	name := s.Name.Name
	// Type parameters go on the header line only. Repeating "[T any]" on every
	// member would state the same constraint once per field for no added signal.
	head := name + typeParams(s.TypeParams)
	if s.Assign.IsValid() {
		// An alias is the same type as its right-hand side, so the RHS is part of
		// the surface. This is what catches a leak back into internal/.
		return []string{"type " + head + " = " + render(s.Type)}
	}

	switch t := s.Type.(type) {
	case *ast.StructType:
		lines := []string{"type " + head + " struct"}
		for _, fld := range t.Fields.List {
			lines = append(lines, memberLines(name, "field", fld)...)
		}
		return lines
	case *ast.InterfaceType:
		lines := []string{"type " + head + " interface"}
		for _, fld := range t.Methods.List {
			lines = append(lines, memberLines(name, "method", fld)...)
		}
		return lines
	default:
		return []string{"type " + head + " " + render(s.Type)}
	}
}

// typeParams renders a generic declaration's parameter list as "[K comparable, V any]".
// go/printer cannot print a bare *ast.FieldList, so this assembles one. Tightening a
// constraint breaks every existing instantiation while leaving the name untouched,
// which is exactly the kind of change the snapshot exists to surface.
func typeParams(l *ast.FieldList) string {
	if l == nil {
		return ""
	}
	params := make([]string, 0, len(l.List))
	for _, f := range l.List {
		names := make([]string, 0, len(f.Names))
		for _, n := range f.Names {
			names = append(names, n.Name)
		}
		params = append(params, strings.Join(names, ", ")+" "+render(f.Type))
	}
	return "[" + strings.Join(params, ", ") + "]"
}

// memberLines renders one struct field or interface method. An embedded entry has
// no name of its own, so its type supplies one.
func memberLines(owner, kind string, fld *ast.Field) []string {
	if len(fld.Names) == 0 {
		embedded := baseName(fld.Type)
		if !ast.IsExported(embedded) {
			return nil
		}
		return []string{fmt.Sprintf("%s %s.%s %s", kind, owner, embedded, render(fld.Type)) + tag(fld)}
	}

	var lines []string
	for _, n := range fld.Names {
		if !n.IsExported() {
			continue
		}
		if ft, ok := fld.Type.(*ast.FuncType); ok && kind == "method" {
			lines = append(lines, fmt.Sprintf("method %s.%s%s", owner, n.Name, signature(ft)))
			continue
		}
		lines = append(lines, fmt.Sprintf("%s %s.%s %s", kind, owner, n.Name, render(fld.Type))+tag(fld))
	}
	return lines
}

// tag renders a struct tag, which names the field on the wire. Renaming a json tag
// on a struct ACOR returns breaks everyone marshaling it, and does so without moving
// a single Go signature — invisible to any tool that compares types alone.
func tag(fld *ast.Field) string {
	if fld.Tag == nil {
		return ""
	}
	return " " + fld.Tag.Value
}

// signature renders a func type as the part that follows the name, so a printed
// "func(ctx context.Context) error" becomes "(ctx context.Context) error".
func signature(ft *ast.FuncType) string {
	return strings.TrimPrefix(render(ft), "func")
}

// render prints an AST node on one line, deliberately against an empty FileSet
// rather than the one the node was parsed with. go/printer reproduces the line
// breaks of whatever positions it can resolve, so passing the real FileSet would
// make a hand-wrapped declaration render as several lines — and rewrapping one
// changes no API but would still fail the gate. With no positions to find, every
// node comes back in canonical single-line form. Collapsing whitespace afterwards
// is belt-and-braces for anything that still carries an internal newline.
func render(node ast.Node) string {
	var buf bytes.Buffer
	if err := printer.Fprint(&buf, token.NewFileSet(), node); err != nil {
		// Unreachable for a parsed node, and a panic here would be a worse
		// failure mode than a line that fails the diff loudly.
		return "<unprintable>"
	}
	return strings.Join(strings.Fields(buf.String()), " ")
}

// baseName reduces a type expression to the identifier a receiver or embedded
// field is named by: *T, T[K], and pkg.T all answer with their own last ident.
func baseName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return baseName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.IndexExpr:
		return baseName(t.X)
	case *ast.IndexListExpr:
		return baseName(t.X)
	}
	return ""
}

func dedupe(sorted []string) []string {
	out := sorted[:0]
	for i, l := range sorted {
		if i == 0 || l != sorted[i-1] {
			out = append(out, l)
		}
	}
	return out
}
