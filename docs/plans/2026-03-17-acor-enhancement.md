# ACOR Enhancement Implementation Plan

> **For agentic workers:** REQUIRED: Use superpowers:subagent-driven-development (if subagents available) or superpowers:executing-plans to implement this plan. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add batch operations and parallel matching capabilities to ACOR library.

**Architecture:** Extend existing `pkg/acor` package with new files for batch operations, parallel matching, options types, and errors. All new types and methods integrate seamlessly with existing API.

**Tech Stack:** Go 1.24, go-redis/redis/v8, miniredis for testing

---

## File Structure

```
pkg/acor/
├── acor.go           # Existing - no changes
├── acor_test.go      # Existing - no changes
├── errors.go         # New - error definitions
├── options.go        # New - options types for batch/parallel
├── batch.go          # New - batch operations
├── batch_test.go     # New - batch tests
├── parallel.go       # New - parallel matching
└── parallel_test.go  # New - parallel tests
```

---

## Task 1: Error Definitions

**Files:**
- Create: `pkg/acor/errors.go`

- [ ] **Step 1: Create errors.go with new error types**

```go
package acor

import "errors"

var (
	ErrEmptyKeyword       = errors.New("keyword cannot be empty")
	ErrInvalidChunkSize   = errors.New("chunk size must be positive")
	ErrInvalidWorkerCount = errors.New("worker count must be positive")
	ErrNoBoundariesFound  = errors.New("could not find suitable chunk boundaries")
	ErrStreamInterrupted  = errors.New("stream processing was interrupted")
)
```

- [ ] **Step 2: Run tests to verify no regressions**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/...`
Expected: All tests pass

- [ ] **Step 3: Commit**

```bash
git add pkg/acor/errors.go
git commit -m "feat: add error definitions for batch and parallel operations"
```

---

## Task 2: Options Types

**Files:**
- Create: `pkg/acor/options.go`

- [ ] **Step 1: Create options.go with batch and parallel option types**

```go
package acor

import "runtime"

type BatchMode int

const (
	BatchModeBestEffort BatchMode = iota
	BatchModeTransactional
)

type BatchOptions struct {
	Mode BatchMode
}

type ChunkBoundary int

const (
	ChunkBoundaryWord ChunkBoundary = iota
	ChunkBoundarySentence
	ChunkBoundaryLine
)

type ParallelOptions struct {
	Workers   int
	ChunkSize int
	Boundary  ChunkBoundary
	Overlap   int
}

func DefaultParallelOptions() *ParallelOptions {
	return &ParallelOptions{
		Workers:   runtime.NumCPU(),
		ChunkSize: 1000,
		Boundary:  ChunkBoundaryWord,
		Overlap:   0,
	}
}

type KeywordError struct {
	Keyword string
	Error   error
}

type BatchResult struct {
	Added   []string
	Failed  []KeywordError
	Skipped []string
}
```

- [ ] **Step 2: Run tests to verify no regressions**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/...`
Expected: All tests pass

- [ ] **Step 3: Commit**

```bash
git add pkg/acor/options.go
git commit -m "feat: add options types for batch and parallel operations"
```

---

## Task 3: Batch Operations - AddMany

**Files:**
- Create: `pkg/acor/batch.go`
- Create: `pkg/acor/batch_test.go`

- [ ] **Step 1: Write failing test for AddMany with BestEffort mode**

```go
package acor

import (
	"testing"
)

func TestAddManyBestEffort(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "her", "him"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Added) != 3 {
		t.Errorf("expected 3 added, got %d", len(result.Added))
	}
	if len(result.Failed) != 0 {
		t.Errorf("expected 0 failed, got %d", len(result.Failed))
	}
	if len(result.Skipped) != 0 {
		t.Errorf("expected 0 skipped, got %d", len(result.Skipped))
	}
}

func TestAddManyBestEffortWithDuplicates(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "he", "her"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeBestEffort})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Added) != 2 {
		t.Errorf("expected 2 added, got %d", len(result.Added))
	}
	if len(result.Skipped) != 1 {
		t.Errorf("expected 1 skipped, got %d", len(result.Skipped))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestAddMany`
Expected: FAIL - AddMany not defined

- [ ] **Step 3: Implement AddMany with BestEffort mode**

Create `pkg/acor/batch.go`:

```go
package acor

import (
	"strings"
	"sync"
)

func (ac *AhoCorasick) AddMany(keywords []string, opts *BatchOptions) (*BatchResult, error) {
	if opts == nil {
		opts = &BatchOptions{Mode: BatchModeBestEffort}
	}

	result := &BatchResult{
		Added:   make([]string, 0),
		Failed:  make([]KeywordError, 0),
		Skipped: make([]string, 0),
	}

	if opts.Mode == BatchModeTransactional {
		return ac.addManyTransactional(keywords, result)
	}
	return ac.addManyBestEffort(keywords, result)
}

func (ac *AhoCorasick) addManyBestEffort(keywords []string, result *BatchResult) (*BatchResult, error) {
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   ErrEmptyKeyword,
			})
			continue
		}

		count, err := ac.Add(keyword)
		if err != nil {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   err,
			})
			continue
		}

		if count == 0 {
			result.Skipped = append(result.Skipped, keyword)
		} else {
			result.Added = append(result.Added, keyword)
		}
	}

	return result, nil
}

func (ac *AhoCorasick) addManyTransactional(keywords []string, result *BatchResult) (*BatchResult, error) {
	added := make([]string, 0)

	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			ac.rollbackAdded(added)
			return nil, ErrEmptyKeyword
		}

		count, err := ac.Add(keyword)
		if err != nil {
			ac.rollbackAdded(added)
			return nil, err
		}

		if count > 0 {
			added = append(added, keyword)
		}
	}

	result.Added = added
	return result, nil
}

func (ac *AhoCorasick) rollbackAdded(keywords []string) {
	var wg sync.WaitGroup
	for _, keyword := range keywords {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			_, _ = ac.Remove(k)
		}(keyword)
	}
	wg.Wait()
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestAddMany -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/acor/batch.go pkg/acor/batch_test.go
git commit -m "feat: implement AddMany batch operation"
```

---

## Task 4: Batch Operations - AddMany Transactional

**Files:**
- Modify: `pkg/acor/batch_test.go`

- [ ] **Step 1: Write failing test for AddMany with Transactional mode**

Add to `pkg/acor/batch_test.go`:

```go
func TestAddManyTransactional(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "her", "him"}
	result, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeTransactional})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Added) != 3 {
		t.Errorf("expected 3 added, got %d", len(result.Added))
	}
}

func TestAddManyTransactionalRollbackOnError(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	ac.buildTrieHook = func(prefix string) error {
		if prefix == "her" {
			return errors.New("forced failure")
		}
		return nil
	}
	defer func() { ac.buildTrieHook = nil }()

	keywords := []string{"he", "her", "him"}
	_, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeTransactional})
	if err == nil {
		t.Fatal("expected error on transactional batch")
	}

	ac.buildTrieHook = nil

	results, err := ac.Find("he")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected rollback to remove all keywords, found %d", len(results))
	}
}

func TestAddManyTransactionalRollbackOnEmpty(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("existing"); err != nil {
		t.Fatal(err)
	}

	keywords := []string{"he", "", "him"}
	_, err := ac.AddMany(keywords, &BatchOptions{Mode: BatchModeTransactional})
	if !errors.Is(err, ErrEmptyKeyword) {
		t.Fatalf("expected ErrEmptyKeyword, got %v", err)
	}

	results, err := ac.Find("he")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected rollback to remove he, found %d", len(results))
	}
}
```

- [ ] **Step 2: Run tests to verify they pass**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestAddManyTransactional -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add pkg/acor/batch_test.go
git commit -m "test: add tests for AddMany transactional mode"
```

---

## Task 5: Batch Operations - RemoveMany

**Files:**
- Modify: `pkg/acor/batch.go`
- Modify: `pkg/acor/batch_test.go`

- [ ] **Step 1: Write failing test for RemoveMany**

Add to `pkg/acor/batch_test.go`:

```go
func TestRemoveMany(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
		t.Fatal(err)
	}

	result, err := ac.RemoveMany([]string{"he", "her"})
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Added) != 1 {
		t.Errorf("expected 1 remaining keyword, got %d", len(result.Added))
	}

	results, err := ac.Find("him")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != "him" {
		t.Error("expected 'him' to remain")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestRemoveMany`
Expected: FAIL - RemoveMany not defined

- [ ] **Step 3: Implement RemoveMany**

Add to `pkg/acor/batch.go`:

```go
func (ac *AhoCorasick) RemoveMany(keywords []string) (*BatchResult, error) {
	result := &BatchResult{
		Added:   make([]string, 0),
		Failed:  make([]KeywordError, 0),
		Skipped: make([]string, 0),
	}

	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}

		remaining, err := ac.Remove(keyword)
		if err != nil {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   err,
			})
			continue
		}

		result.Added = append(result.Added, keyword)
	}

	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestRemoveMany -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/acor/batch.go pkg/acor/batch_test.go
git commit -m "feat: implement RemoveMany batch operation"
```

---

## Task 6: Batch Operations - FindMany

**Files:**
- Modify: `pkg/acor/batch.go`
- Modify: `pkg/acor/batch_test.go`

- [ ] **Step 1: Write failing test for FindMany**

Add to `pkg/acor/batch_test.go`:

```go
func TestFindMany(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.AddMany([]string{"he", "her", "him"}, nil); err != nil {
		t.Fatal(err)
	}

	texts := []string{"he is here", "him and her", "nothing"}
	results, err := ac.FindMany(texts)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if len(results["he is here"]) < 2 {
		t.Errorf("expected at least 2 matches in 'he is here', got %d", len(results["he is here"]))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestFindMany`
Expected: FAIL - FindMany not defined

- [ ] **Step 3: Implement FindMany**

Add to `pkg/acor/batch.go`:

```go
func (ac *AhoCorasick) FindMany(texts []string) (map[string][]string, error) {
	results := make(map[string][]string)

	for _, text := range texts {
		matches, err := ac.Find(text)
		if err != nil {
			return nil, err
		}
		results[text] = matches
	}

	return results, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestFindMany -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/acor/batch.go pkg/acor/batch_test.go
git commit -m "feat: implement FindMany batch operation"
```

---

## Task 7: Parallel Operations - Chunking

**Files:**
- Create: `pkg/acor/parallel.go`
- Create: `pkg/acor/parallel_test.go`

- [ ] **Step 1: Write failing test for chunk splitting**

Create `pkg/acor/parallel_test.go`:

```go
package acor

import (
	"testing"
)

func TestSplitChunksByWord(t *testing.T) {
	opts := &ParallelOptions{
		ChunkSize: 10,
		Boundary:  ChunkBoundaryWord,
		Overlap:   2,
	}

	text := "hello world this is a test"
	chunks := splitChunks(text, opts)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}

	for i, chunk := range chunks {
		if chunk.start > chunk.end {
			t.Errorf("chunk %d has invalid bounds: start=%d, end=%d", i, chunk.start, chunk.end)
		}
	}
}

func TestSplitChunksByLine(t *testing.T) {
	opts := &ParallelOptions{
		ChunkSize: 10,
		Boundary:  ChunkBoundaryLine,
		Overlap:   0,
	}

	text := "line one\nline two\nline three"
	chunks := splitChunks(text, opts)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}
}

func TestSplitChunksBySentence(t *testing.T) {
	opts := &ParallelOptions{
		ChunkSize: 10,
		Boundary:  ChunkBoundarySentence,
		Overlap:   0,
	}

	text := "First sentence. Second sentence! Third?"
	chunks := splitChunks(text, opts)

	if len(chunks) < 2 {
		t.Errorf("expected at least 2 chunks, got %d", len(chunks))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestSplitChunks`
Expected: FAIL - splitChunks not defined

- [ ] **Step 3: Implement chunk splitting**

Create `pkg/acor/parallel.go`:

```go
package acor

import (
	"strings"
	"unicode"
)

type chunk struct {
	text       string
	start      int
	end        int
	textOffset int
}

func splitChunks(text string, opts *ParallelOptions) []chunk {
	if opts == nil {
		opts = DefaultParallelOptions()
	}

	runes := []rune(text)
	if len(runes) <= opts.ChunkSize {
		return []chunk{{text: text, start: 0, end: len(runes), textOffset: 0}}
	}

	chunks := make([]chunk, 0)
	start := 0

	for start < len(runes) {
		end := start + opts.ChunkSize
		if end >= len(runes) {
			chunks = append(chunks, chunk{
				text:       string(runes[start:]),
				start:      start,
				end:        len(runes),
				textOffset: start,
			})
			break
		}

		boundary := findBoundary(runes, end, opts.Boundary, opts.ChunkSize/2)
		if boundary <= start {
			boundary = end
		}

		chunkText := string(runes[start:boundary])
		chunks = append(chunks, chunk{
			text:       chunkText,
			start:      0,
			end:        len(runes[start:boundary]),
			textOffset: start,
		})

		nextStart := boundary - opts.Overlap
		if nextStart <= start {
			nextStart = boundary
		}
		start = nextStart
	}

	return chunks
}

func findBoundary(runes []rune, target int, boundaryType ChunkBoundary, maxBacktrack int) int {
	backtrack := 0
	for i := target; i > target-maxBacktrack && i > 0; i-- {
		backtrack++
		if isBoundary(runes, i, boundaryType) {
			return i
		}
	}
	return target
}

func isBoundary(runes []rune, idx int, boundaryType ChunkBoundary) bool {
	if idx <= 0 || idx >= len(runes) {
		return false
	}

	switch boundaryType {
	case ChunkBoundaryWord:
		return unicode.IsSpace(runes[idx]) && !unicode.IsSpace(runes[idx-1])
	case ChunkBoundaryLine:
		return runes[idx-1] == '\n'
	case ChunkBoundarySentence:
		return (runes[idx-1] == '.' || runes[idx-1] == '!' || runes[idx-1] == '?') &&
			unicode.IsSpace(runes[idx])
	}
	return false
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestSplitChunks -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/acor/parallel.go pkg/acor/parallel_test.go
git commit -m "feat: implement text chunking for parallel matching"
```

---

## Task 8: Parallel Operations - FindParallel

**Files:**
- Modify: `pkg/acor/parallel.go`
- Modify: `pkg/acor/parallel_test.go`

- [ ] **Step 1: Write failing test for FindParallel**

Add to `pkg/acor/parallel_test.go`:

```go
func TestFindParallel(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "her", "him", "test"}
	for _, k := range keywords {
		if _, err := ac.Add(k); err != nil {
			t.Fatal(err)
		}
	}

	text := "he is here with him. this is a test of the system."
	opts := &ParallelOptions{
		Workers:   2,
		ChunkSize: 20,
		Boundary:  ChunkBoundaryWord,
		Overlap:   3,
	}

	results, err := ac.FindParallel(text, opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Error("expected some matches")
	}
}

func TestFindParallelFallsBackToSequential(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add("test"); err != nil {
		t.Fatal(err)
	}

	text := "test"
	results, err := ac.FindParallel(text, &ParallelOptions{ChunkSize: 100})
	if err != nil {
		t.Fatal(err)
	}

	if len(results) != 1 || results[0] != "test" {
		t.Errorf("expected ['test'], got %v", results)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestFindParallel`
Expected: FAIL - FindParallel not defined

- [ ] **Step 3: Implement FindParallel**

Update imports in `pkg/acor/parallel.go` (merge with existing imports):

```go
import (
	"runtime"
	"strings"
	"sync"
	"unicode"
)

func (ac *AhoCorasick) FindParallel(text string, opts *ParallelOptions) ([]string, error) {
	if opts == nil {
		opts = DefaultParallelOptions()
	}

	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	if opts.ChunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}

	chunks := splitChunks(text, opts)
	if len(chunks) == 0 {
		return []string{}, nil
	}

	if len(chunks) == 1 {
		return ac.Find(text)
	}

	results := make(chan []string, len(chunks))
	errors := make(chan error, len(chunks))

	var wg sync.WaitGroup
	worker := func(c chunk) {
		defer wg.Done()
		matches, err := ac.Find(c.text)
		if err != nil {
			errors <- err
			return
		}
		results <- matches
	}

	sem := make(chan struct{}, opts.Workers)
	for _, c := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(chunk chunk) {
			defer func() { <-sem }()
			worker(chunk)
		}(c)
	}

	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	allMatches := make(map[string]struct{})
	for matches := range results {
		for _, m := range matches {
			allMatches[m] = struct{}{}
		}
	}

	if err := <-errors; err != nil {
		return nil, err
	}

	unique := make([]string, 0, len(allMatches))
	for m := range allMatches {
		unique = append(unique, m)
	}
	return unique, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestFindParallel -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/acor/parallel.go pkg/acor/parallel_test.go
git commit -m "feat: implement FindParallel for parallel pattern matching"
```

---

## Task 9: Parallel Operations - FindIndexParallel

**Files:**
- Modify: `pkg/acor/parallel.go`
- Modify: `pkg/acor/parallel_test.go`

- [ ] **Step 1: Write failing test for FindIndexParallel**

Add to `pkg/acor/parallel_test.go`:

```go
func TestFindIndexParallel(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"he", "her"}
	for _, k := range keywords {
		if _, err := ac.Add(k); err != nil {
			t.Fatal(err)
		}
	}

	text := "he is her friend"
	opts := &ParallelOptions{
		Workers:   2,
		ChunkSize: 10,
		Boundary:  ChunkBoundaryWord,
		Overlap:   3,
	}

	results, err := ac.FindIndexParallel(text, opts)
	if err != nil {
		t.Fatal(err)
	}

	if len(results) == 0 {
		t.Error("expected some matches")
	}

	for keyword, indices := range results {
		for _, idx := range indices {
			if idx < 0 {
				t.Errorf("negative index for %s: %d", keyword, idx)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestFindIndexParallel`
Expected: FAIL - FindIndexParallel not defined

- [ ] **Step 3: Implement FindIndexParallel**

Add to `pkg/acor/parallel.go`:

```go
func (ac *AhoCorasick) FindIndexParallel(text string, opts *ParallelOptions) (map[string][]int, error) {
	if opts == nil {
		opts = DefaultParallelOptions()
	}

	if opts.Workers <= 0 {
		opts.Workers = runtime.NumCPU()
	}
	if opts.ChunkSize <= 0 {
		return nil, ErrInvalidChunkSize
	}

	chunks := splitChunks(text, opts)
	if len(chunks) == 0 {
		return map[string][]int{}, nil
	}

	if len(chunks) == 1 {
		return ac.FindIndex(text)
	}

	type indexedResult struct {
		matches map[string][]int
		offset  int
	}

	results := make(chan indexedResult, len(chunks))
	errors := make(chan error, len(chunks))

	var wg sync.WaitGroup
	worker := func(c chunk) {
		defer wg.Done()
		matches, err := ac.FindIndex(c.text)
		if err != nil {
			errors <- err
			return
		}
		results <- indexedResult{matches: matches, offset: c.textOffset}
	}

	sem := make(chan struct{}, opts.Workers)
	for _, c := range chunks {
		wg.Add(1)
		sem <- struct{}{}
		go func(chunk chunk) {
			defer func() { <-sem }()
			worker(chunk)
		}(c)
	}

	go func() {
		wg.Wait()
		close(results)
		close(errors)
	}()

	allMatches := make(map[string]map[int]struct{})
	for res := range results {
		for keyword, indices := range res.matches {
			if allMatches[keyword] == nil {
				allMatches[keyword] = make(map[int]struct{})
			}
			for _, idx := range indices {
				adjustedIdx := idx + res.offset
				allMatches[keyword][adjustedIdx] = struct{}{}
			}
		}
	}

	if err := <-errors; err != nil {
		return nil, err
	}

	result := make(map[string][]int)
	for keyword, indices := range allMatches {
		result[keyword] = make([]int, 0, len(indices))
		for idx := range indices {
			result[keyword] = append(result[keyword], idx)
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -run TestFindIndexParallel -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add pkg/acor/parallel.go pkg/acor/parallel_test.go
git commit -m "feat: implement FindIndexParallel for parallel matching with indices"
```

---

## Task 10: Integration Tests and Final Verification

**Files:**
- Modify: `pkg/acor/batch_test.go`
- Modify: `pkg/acor/parallel_test.go`

- [ ] **Step 1: Add integration test combining batch and parallel**

Add to `pkg/acor/parallel_test.go`:

```go
func TestBatchAddAndParallelFind(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"apple", "application", "apply", "approach", "approximate"}
	result, err := ac.AddMany(keywords, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Added) != len(keywords) {
		t.Errorf("expected %d added, got %d", len(keywords), len(result.Added))
	}

	text := "I want to apply for an application. The approach is approximate."
	parallelResults, err := ac.FindParallel(text, &ParallelOptions{
		Workers:   2,
		ChunkSize: 30,
		Boundary:  ChunkBoundarySentence,
		Overlap:   5,
	})
	if err != nil {
		t.Fatal(err)
	}

	sequentialResults, err := ac.Find(text)
	if err != nil {
		t.Fatal(err)
	}

	if len(parallelResults) != len(sequentialResults) {
		t.Errorf("parallel results (%d) != sequential results (%d)", len(parallelResults), len(sequentialResults))
	}
}
```

- [ ] **Step 2: Run all tests**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./pkg/acor/... -v`
Expected: All tests pass

- [ ] **Step 3: Run linter**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go vet ./...`
Expected: No issues

- [ ] **Step 4: Commit**

```bash
git add pkg/acor/batch_test.go pkg/acor/parallel_test.go
git commit -m "test: add integration tests for batch and parallel operations"
```

---

## Task 11: Final Commit and Verification

- [ ] **Step 1: Run full test suite**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go test ./... -v`
Expected: All tests pass

- [ ] **Step 2: Verify build**

Run: `cd /Users/lukas/Workspace/acor/.worktrees/acor-enhancement && go build ./...`
Expected: Build succeeds

- [ ] **Step 3: Final commit with all changes**

```bash
git add -A
git status
git log --oneline -10
```

---

## Summary

**New Files Created:**
- `pkg/acor/errors.go` - Error definitions
- `pkg/acor/options.go` - Options types for batch/parallel
- `pkg/acor/batch.go` - Batch operations (AddMany, RemoveMany, FindMany)
- `pkg/acor/batch_test.go` - Batch operation tests
- `pkg/acor/parallel.go` - Parallel matching (FindParallel, FindIndexParallel)
- `pkg/acor/parallel_test.go` - Parallel matching tests

**API Additions:**
- `AddMany(keywords []string, opts *BatchOptions) (*BatchResult, error)`
- `RemoveMany(keywords []string) (*BatchResult, error)`
- `FindMany(texts []string) (map[string][]string, error)`
- `FindParallel(text string, opts *ParallelOptions) ([]string, error)`
- `FindIndexParallel(text string, opts *ParallelOptions) (map[string][]int, error)`
