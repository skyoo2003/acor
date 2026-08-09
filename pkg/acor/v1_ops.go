// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	redis "github.com/redis/go-redis/v9"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// Compile-time check that v1Operations satisfies the operations interface.
var _ operations = (*v1Operations)(nil)

// v1Operations implements the operations interface for the V1 schema, which is
// read-only: add and remove refuse, and what remains is the read, suggest, info,
// and flush path. The writer that used to sit behind add lives in
// v1_fixture_test.go, so no production binary carries a way to write V1.
type v1Operations struct {
	storage         kvStorage
	name            string
	logger          Logger
	caseSensitive   bool
	rollbackTimeout time.Duration
	engines         engineMemo
}

// engineForKeywords returns the automaton for kws, rebuilding only when the set
// changed. See engineMemo in engine_memo.go, shared with V2.
func (m *engineMemo) engineForKeywords(kws []string) *matchengine.Engine {
	// Building from a keyword set cannot fail, so the error is always nil.
	engine, _ := m.engineFor(digestKeywords(kws), func() (*matchengine.Engine, error) {
		set := make(map[string]struct{}, len(kws))
		for _, k := range kws {
			set[k] = struct{}{}
		}
		return buildEngine(PresetBalanced, set), nil
	})
	return engine
}

// --- operations interface methods ---

// add refuses unconditionally: V1 collections take no new keywords. Every public
// write path reaches this method, so there is no second place to guard and no
// configuration that reopens it.
func (o *v1Operations) add(context.Context, string) (int, error) {
	return 0, ErrV1ReadOnly
}

// remove refuses for the same reason as add. Flush still works, so a V1 collection
// can be discarded wholesale even though single keywords can no longer be taken out
// of it.
func (o *v1Operations) remove(context.Context, string) (int, error) {
	return 0, ErrV1ReadOnly
}

// find scans text with the automaton loadEngine builds from the keyword set,
// which reports the same matches the per-character trie walk in Redis used to
// (pinned by v1_engine_parity_test.go) for one round trip instead of one per rune.
func (o *v1Operations) find(ctx context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	text = normalizeText(text, o.caseSensitive)

	engine, err := o.loadEngine(ctx)
	if err != nil {
		return nil, newOperationError("find", SchemaV1, err)
	}

	matched := engine.Find(text)
	o.logger.Println(fmt.Sprintf("Find(%s) > Matched(%v) : Count(%d)", text, matched, len(matched)))
	return matched, nil
}

// findIndex is find with the start offset of every match.
func (o *v1Operations) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}
	text = normalizeText(text, o.caseSensitive)

	engine, err := o.loadEngine(ctx)
	if err != nil {
		return nil, newOperationError("findIndex", SchemaV1, err)
	}

	matched := engine.FindIndex(text)
	o.logger.Println(fmt.Sprintf("FindIndex(%s) > Matched(%v) : Count(%d)", text, matched, len(matched)))
	return matched, nil
}

// loadEngine returns an in-memory automaton for the V1 keyword set. V1 stores
// keywords already normalized (lowercased when !caseSensitive), so no
// re-normalization is needed here.
//
// The set itself is re-read every call — V1 has no invalidation listener
// (EnableCache with V1 is ErrCacheRequiresV2), so a peer's write would otherwise
// go unnoticed. Only the rebuild is memoized (see engineMemo).
func (o *v1Operations) loadEngine(ctx context.Context) (*matchengine.Engine, error) {
	kws, err := o.storage.SMembers(ctx, keywordKey(o.name))
	if err != nil {
		return nil, newRedisError("SMEMBERS", keywordKey(o.name), err)
	}
	return o.engines.engineForKeywords(kws), nil
}

func (o *v1Operations) flush(_ context.Context) error {
	// Use a detached context so flush completes atomically even if the caller's
	// context is canceled (e.g., via FlushContext). A partial flush would leave
	// the trie in an inconsistent state (some keys deleted but not all).
	flushCtx, cancel := context.WithTimeout(context.Background(), o.rollbackTimeout)
	defer cancel()
	ctx := flushCtx

	kKey := keywordKey(o.name)
	pKey := prefixKey(o.name)
	sKey := suffixKey(o.name)

	keywords, err := o.storage.SMembers(ctx, kKey)
	if err != nil {
		return newRedisError("SMEMBERS", kKey, err)
	}
	o.logger.Println(fmt.Sprintf("Flush() > SMEMBERS Key(%s) : Members(%v)", kKey, keywords))

	for _, keyword := range keywords {
		nKey := nodeKey(o.name, keyword)
		nodes, err := o.storage.SMembers(ctx, nKey)
		if err != nil {
			return newRedisError("SMEMBERS", nKey, err)
		}
		for _, node := range nodes {
			oKey := outputKey(o.name, node)
			if err := o.storage.Del(ctx, oKey); err != nil {
				return newRedisError("DEL", oKey, err)
			}
			o.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s)", oKey))
		}
		if err := o.storage.Del(ctx, nKey); err != nil {
			return newRedisError("DEL", nKey, err)
		}
		o.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s)", nKey))
	}

	if err := o.storage.Del(ctx, pKey); err != nil {
		return newRedisError("DEL", pKey, err)
	}
	o.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s)", pKey))

	if err := o.storage.Del(ctx, sKey); err != nil {
		return newRedisError("DEL", sKey, err)
	}
	o.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s)", sKey))

	if err := o.storage.Del(ctx, kKey); err != nil {
		return newRedisError("DEL", kKey, err)
	}
	o.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s)", kKey))

	return nil
}

func (o *v1Operations) info(ctx context.Context) (*AhoCorasickInfo, error) {
	kKey := keywordKey(o.name)
	kCount, err := o.storage.SCard(ctx, kKey)
	if err != nil {
		return nil, newRedisError("SCARD", kKey, err)
	}
	o.logger.Println(fmt.Sprintf("Info() > SCARD Key(%s) : Count(%d)", kKey, kCount))

	nKey := prefixKey(o.name)
	nCount, err := o.storage.ZCard(ctx, nKey)
	if err != nil {
		return nil, newRedisError("ZCARD", nKey, err)
	}
	o.logger.Println(fmt.Sprintf("Info() > ZCARD Key(%s) : Count(%d)", nKey, nCount))

	return &AhoCorasickInfo{
		Keywords: int(kCount),
		Nodes:    int(nCount),
	}, nil
}

func (o *v1Operations) suggest(ctx context.Context, input string) ([]string, error) {
	input = strings.TrimSpace(input)
	if !o.caseSensitive {
		input = strings.ToLower(input)
	}
	if input == "" {
		return []string{}, nil
	}

	var pKeywords []string

	results := make([]string, 0)

	kKey := keywordKey(o.name)
	pKey := prefixKey(o.name)
	pZRank, err := o.storage.ZRank(ctx, pKey, input)
	if errors.Is(err, redis.Nil) {
		return results, nil
	}
	if err != nil {
		return nil, newRedisError("ZRANK", pKey, err)
	}

	pKeywords, err = o.storage.ZRange(ctx, pKey, pZRank, pZRank)
	if err != nil {
		return nil, newRedisError("ZRANGE", pKey, err)
	}
	for len(pKeywords) > 0 {
		pKeyword := pKeywords[0]
		kExists, err := o.storage.SIsMember(ctx, kKey, pKeyword)
		if err != nil {
			return nil, newRedisError("SISMEMBER", kKey, err)
		}
		if kExists && strings.HasPrefix(pKeyword, input) {
			results = append(results, pKeyword)
		}

		pZRank++
		pKeywords, err = o.storage.ZRange(ctx, pKey, pZRank, pZRank)
		if err != nil {
			return nil, newRedisError("ZRANGE", pKey, err)
		}
		if len(pKeywords) > 0 && !strings.HasPrefix(pKeywords[0], input) {
			break
		}
	}

	return results, nil
}

func (o *v1Operations) suggestIndex(ctx context.Context, input string) (map[string][]int, error) {
	results, err := o.suggest(ctx, input)
	if err != nil {
		return nil, err
	}

	indexed := make(map[string][]int, len(results))
	for _, kw := range results {
		indexed[kw] = []int{0}
	}
	return indexed, nil
}
