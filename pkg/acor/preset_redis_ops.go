// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
)

// Compile-time check.
var _ operations = (*presetRedisOps)(nil)

// presetRedisOps adapts a redisBackedAC to satisfy the operations interface
// used by AhoCorasick. Suggest/SuggestIndex are not supported in preset mode.
type presetRedisOps struct {
	ac *redisBackedAC
}

func newPresetRedisOps(ac *redisBackedAC) *presetRedisOps {
	return &presetRedisOps{ac: ac}
}

func (o *presetRedisOps) add(ctx context.Context, keyword string) (int, error) {
	return o.ac.Add(ctx, keyword)
}

func (o *presetRedisOps) remove(ctx context.Context, keyword string) (int, error) {
	return o.ac.Remove(ctx, keyword)
}

func (o *presetRedisOps) find(ctx context.Context, text string) ([]string, error) {
	return o.ac.Find(ctx, text)
}

func (o *presetRedisOps) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	return o.ac.FindIndex(ctx, text)
}

func (o *presetRedisOps) suggest(_ context.Context, _ string) ([]string, error) {
	return nil, ErrSuggestRequiresRedis
}

func (o *presetRedisOps) suggestIndex(_ context.Context, _ string) (map[string][]int, error) {
	return nil, ErrSuggestRequiresRedis
}

func (o *presetRedisOps) flush(ctx context.Context) error {
	return o.ac.Flush(ctx)
}

func (o *presetRedisOps) info(ctx context.Context) (*AhoCorasickInfo, error) {
	mi, err := o.ac.Info(ctx)
	if err != nil {
		return nil, err
	}
	return &AhoCorasickInfo{
		Keywords:    mi.Keywords,
		Nodes:       mi.Nodes,
		Preset:      mi.Preset,
		MemoryBytes: mi.MemoryBytes,
		TrieDepth:   mi.TrieDepth,
	}, nil
}
