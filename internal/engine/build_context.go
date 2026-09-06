// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"context"
	"iter"
	"unicode/utf8"
)

type buildCanceled struct{ err error }
type buildGuard struct {
	ctx   context.Context
	steps uint32
}

func (g *buildGuard) check() {
	if g == nil {
		return
	}
	g.steps++
	if g.steps%1024 == 0 {
		g.now()
	}
}
func (g *buildGuard) now() {
	if err := g.ctx.Err(); err != nil {
		panic(buildCanceled{err})
	}
}

// BuildSequenceContext builds a fresh engine transactionally from a unique
// keyword sequence. Cancellation never publishes a partial engine or leaves a
// detached builder goroutine running. Allocation and sorting remain synchronous.
func (e *Engine) BuildSequenceContext(ctx context.Context, words iter.Seq[string], count int) (err error) {
	if contextErr := ctx.Err(); contextErr != nil {
		return contextErr
	}
	defer func() {
		if value := recover(); value != nil {
			if canceled, ok := value.(buildCanceled); ok {
				err = canceled.err
			} else {
				panic(value)
			}
		}
	}()
	next := New(e.preset)
	guard := &buildGuard{ctx: ctx}
	switch impl := next.impl.(type) {
	case *memEfficientEngine:
		impl.guard = guard
	case *speedEngine:
		impl.guard = guard
	case *balancedEngine:
		impl.banded.dat.guard = guard
	}
	maxRunes := 0
	sequence := func(yield func(string) bool) {
		for word := range words {
			guard.check()
			maxRunes = max(maxRunes, utf8.RuneCountInString(word))
			if !yield(word) {
				return
			}
		}
	}
	if impl, ok := next.impl.(*memEfficientEngine); ok {
		impl.buildFromSequence(sequence, count)
		impl.guard = nil
	} else {
		set := make(map[string]struct{}, count)
		for word := range sequence {
			set[word] = struct{}{}
		}
		next.impl.buildFromKeywords(set)
		switch impl := next.impl.(type) {
		case *speedEngine:
			impl.guard = nil
		case *balancedEngine:
			impl.banded.dat.guard = nil
		}
	}
	guard.now()
	next.maxKeywordRunes = maxRunes
	*e = *next
	return nil
}
