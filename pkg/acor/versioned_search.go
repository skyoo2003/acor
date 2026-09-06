// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"io"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// v3SearchOps only backs private, read-only AhoCorasick adapters. No mutation or
// suggestion entry point is exported through VersionedCollection.
type v3SearchOps struct {
	operations
	engine    *matchengine.Engine
	sensitive bool
}

func (o *v3SearchOps) loadEngine(ctx context.Context) (*matchengine.Engine, error) {
	return o.engine, ctx.Err()
}
func (o *v3SearchOps) find(ctx context.Context, text string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return o.engine.Find(normalizeText(text, o.sensitive)), nil
}
func (o *v3SearchOps) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return o.engine.FindIndex(normalizeText(text, o.sensitive)), nil
}
func (v *VersionedCollection) search(ctx context.Context) (*AhoCorasick, error) {
	if err := v.check(ctx); err != nil {
		return nil, err
	}
	e := v.current.Load()
	if e == nil {
		return nil, ErrVersionedClosed
	}
	return &AhoCorasick{ctx: ctx, caseSensitive: v.opts.CaseSensitive, ops: &v3SearchOps{engine: e.engine, sensitive: v.opts.CaseSensitive}}, nil
}

// Find uses one serving engine and the existing AhoCorasick Find semantics.
func (v *VersionedCollection) Find(ctx context.Context, text string) ([]string, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.FindContext(ctx, text)
}

// FindIndex uses one serving engine and the existing AhoCorasick FindIndex semantics.
func (v *VersionedCollection) FindIndex(ctx context.Context, text string) (map[string][]int, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.FindIndexContext(ctx, text)
}

// FindSet uses one serving engine and the existing AhoCorasick FindSet semantics.
func (v *VersionedCollection) FindSet(ctx context.Context, text string) ([]string, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.FindSetContext(ctx, text)
}

// FindMatches uses one serving engine and the existing AhoCorasick FindMatches semantics.
func (v *VersionedCollection) FindMatches(ctx context.Context, text string, opts *MatchOptions) ([]Match, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.FindMatchesContext(ctx, text, opts)
}

// Contains uses one serving engine and the existing AhoCorasick Contains semantics.
func (v *VersionedCollection) Contains(ctx context.Context, text string) (bool, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return false, err
	}
	return ac.ContainsContext(ctx, text)
}

// FindParallel uses one serving engine and the existing AhoCorasick FindParallel semantics.
func (v *VersionedCollection) FindParallel(ctx context.Context, text string, opts *ParallelOptions) ([]string, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.FindParallelContext(ctx, text, opts)
}

// FindIndexParallel uses one serving engine and the existing AhoCorasick FindIndexParallel semantics.
func (v *VersionedCollection) FindIndexParallel(ctx context.Context, text string, opts *ParallelOptions) (map[string][]int, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	return ac.FindIndexParallelContext(ctx, text, opts)
}

// FindStream scans the entire stream with one engine, preserving chunk boundaries.
func (v *VersionedCollection) FindStream(ctx context.Context, r io.Reader, onMatch func(Match) bool) error {
	ac, err := v.search(ctx)
	if err != nil {
		return err
	}
	return ac.FindStreamContext(ctx, r, onMatch)
}

// FindBatch scans every input against one serving engine, preserving input order.
func (v *VersionedCollection) FindBatch(ctx context.Context, texts []string) ([][]string, error) {
	ac, err := v.search(ctx)
	if err != nil {
		return nil, err
	}
	out := make([][]string, len(texts))
	for i, text := range texts {
		out[i], err = ac.FindContext(ctx, text)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}
