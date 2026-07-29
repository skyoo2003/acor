// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"strings"
	"sync"
	"time"

	redis "github.com/redis/go-redis/v9"

	matchengine "github.com/skyoo2003/acor/internal/engine"
)

// Compile-time check that v1Operations satisfies the operations interface.
var _ operations = (*v1Operations)(nil)

// v1Operations implements the operations interface for the V1 schema.
// It holds all dependencies needed for V1 Aho-Corasick operations.
// The ac field provides access to trie.go helper methods (buildTrie, pruneTrie,
// gotoNode, failNode, collectOutputs, appendMatchedIndexes) which are methods
// on *AhoCorasick. This is a temporary bridge until trie.go is refactored in T15.
type v1Operations struct {
	storage         KVStorage
	name            string
	logger          Logger
	ac              *AhoCorasick // for trie.go helper access (temporary, cleaned up in T15)
	caseSensitive   bool
	rollbackTimeout time.Duration
	engines         v1EngineMemo
}

// v1EngineMemo holds the automaton last built for a keyword set, so repeated
// searches over an unchanged V1 collection skip the rebuild. Building is not
// cheap — an O(keywords) double-array pack that runs ~14ms for 10k keywords and
// ~800ms for 100k — and find, findIndex, FindMatches, Contains, FindStream and
// every FindParallel chunk all go through loadEngine.
//
// This is not the V2 cache: the keyword set is still read from Redis on every
// call, so a peer's write is picked up immediately. Only the rebuild is skipped,
// and only while the set Redis returns is unchanged.
type v1EngineMemo struct {
	mu     sync.Mutex
	engine *matchengine.Engine
	digest uint64
	count  int
}

// v1DigestSeed keys the keyword-set digest. It is per-process, which is all the
// memo needs — digests are only ever compared against ones taken in the same
// process, never persisted or sent over the wire.
var v1DigestSeed = maphash.MakeSeed()

// engineFor returns the automaton for kws, rebuilding only when the set changed.
// The digest sums per-member hashes, so it does not depend on the order SMEMBERS
// happened to return the members in.
func (m *v1EngineMemo) engineFor(kws []string) *matchengine.Engine {
	var digest uint64
	for _, k := range kws {
		digest += maphash.String(v1DigestSeed, k)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.engine != nil && m.count == len(kws) && m.digest == digest {
		return m.engine
	}

	set := make(map[string]struct{}, len(kws))
	for _, k := range kws {
		set[k] = struct{}{}
	}
	m.engine = buildEngine(PresetBalanced, set)
	m.digest = digest
	m.count = len(kws)
	return m.engine
}

// --- operations interface methods ---

func (o *v1Operations) add(ctx context.Context, keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	if !o.caseSensitive {
		keyword = strings.ToLower(keyword)
	}
	if keyword == "" {
		return 0, nil
	}

	keywordKey := keywordKey(o.name)

	exists, err := o.storage.SIsMember(ctx, keywordKey, keyword)
	if err != nil {
		return 0, newRedisError("SISMEMBER", keywordKey, err)
	}
	if exists {
		return 0, nil
	}

	if err := o.storage.SAdd(ctx, keywordKey, keyword); err != nil {
		return 0, newRedisError("SADD", keywordKey, err)
	}
	o.logger.Println(fmt.Sprintf(`Add(%s) > SADD {"key": "%s", "member": "%s"}`, keyword, keywordKey, keyword))

	if err := o.ac.buildTrieWithContext(ctx, keyword); err != nil {
		// Intentionally use a fresh context for rollback — the caller's ctx may be
		// canceled (timeout, etc.), but we still need to clean up the partially
		// added keyword to avoid leaving the trie in an inconsistent state.
		rollbackCtx, cancel := context.WithTimeout(context.Background(), o.rollbackTimeout)
		defer cancel()
		if _, rollbackErr := o.remove(rollbackCtx, keyword); rollbackErr != nil {
			return 0, newOperationError("add", SchemaV1, fmt.Errorf("build trie: %w; rollback keyword: %v", err, rollbackErr))
		}
		return 0, newOperationError("add", SchemaV1, err)
	}

	return 1, nil
}

func (o *v1Operations) remove(_ context.Context, keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	if !o.caseSensitive {
		keyword = strings.ToLower(keyword)
	}
	if keyword == "" {
		return 0, nil
	}

	// Use a detached context so remove completes atomically even if the caller's
	// context is canceled (e.g., via RemoveContext). Without this, a canceled
	// context could leave the trie in a partially-removed inconsistent state
	// (e.g., outputs removed from nodes but keyword still in the keyword set).
	removeCtx, cancel := context.WithTimeout(context.Background(), o.rollbackTimeout)
	defer cancel()
	ctx := removeCtx

	kKey := keywordKey(o.name)
	exists, err := o.storage.SIsMember(ctx, kKey, keyword)
	if err != nil {
		return 0, newRedisError("SISMEMBER", kKey, err)
	}
	if !exists {
		return 0, nil
	}

	nodeKey := nodeKey(o.name, keyword)
	nodes, err2 := o.storage.SMembers(ctx, nodeKey)
	if err2 != nil {
		return 0, newRedisError("SMEMBERS", nodeKey, err2)
	}
	for _, node := range nodes {
		oKey := outputKey(o.name, node)
		if sremErr := o.storage.SRem(ctx, oKey, keyword); sremErr != nil {
			return 0, newRedisError("SREM", oKey, sremErr)
		}
		o.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s)", keyword, oKey))
	}

	if delErr := o.storage.Del(ctx, nodeKey); delErr != nil {
		return 0, newRedisError("DEL", nodeKey, delErr)
	}
	o.logger.Println(fmt.Sprintf("Remove(%s) > DEL key(%s)", keyword, nodeKey))

	if pruneErr := o.ac.pruneTrieWithContext(ctx, keyword); pruneErr != nil {
		return 0, newOperationError("remove", SchemaV1, pruneErr)
	}

	if sremErr := o.storage.SRem(ctx, kKey, keyword); sremErr != nil {
		return 0, newRedisError("SREM", kKey, sremErr)
	}
	o.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s) members(%s)", keyword, kKey, keyword))

	return 1, nil
}

// find scans text with an automaton built from the keyword set.
//
// V1 used to resolve goto/fail/output against Redis once per character, which
// cost a round trip per rune of input. loadEngine reads the keyword set with a
// single SMEMBERS and builds the same automaton the other modes scan, so the
// matches are identical (v1_engine_parity_test.go pins that) for one round trip
// instead of one per character.
func (o *v1Operations) find(ctx context.Context, text string) ([]string, error) {
	if text == "" {
		return []string{}, nil
	}
	text = normalizeText(text, o.caseSensitive)

	engine, err := o.loadEngine(ctx)
	if err != nil {
		return nil, newOperationError("find", SchemaV1, err)
	}

	// Honor a canceled ctx at the match boundary; the in-memory scan itself isn't
	// ctx-threaded. Mirrors v2Operations.find.
	if err := ctx.Err(); err != nil {
		return nil, newOperationError("find", SchemaV1, err)
	}

	matched := engine.Find(text)
	o.logger.Println(fmt.Sprintf("Find(%s) > Matched(%v) : Count(%d)", text, matched, len(matched)))
	return matched, nil
}

// findIndex is find with the start offset of every match. See find on why this
// no longer walks the trie in Redis.
func (o *v1Operations) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	if text == "" {
		return map[string][]int{}, nil
	}
	text = normalizeText(text, o.caseSensitive)

	engine, err := o.loadEngine(ctx)
	if err != nil {
		return nil, newOperationError("findIndex", SchemaV1, err)
	}

	// See find: honor an already-canceled/expired ctx before the in-memory match.
	if err := ctx.Err(); err != nil {
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
// The keyword set is re-read on every call rather than cached: a real cache
// would need the pub/sub invalidation listener, which only runs under
// EnableCache — and EnableCache with V1 is ErrCacheRequiresV2 — so without the
// re-read a peer's write would leave this instance matching a stale set. The
// automaton built from that set is memoized (see v1EngineMemo), which keeps the
// freshness while skipping the rebuild when nothing changed.
func (o *v1Operations) loadEngine(ctx context.Context) (*matchengine.Engine, error) {
	kws, err := o.storage.SMembers(ctx, keywordKey(o.name))
	if err != nil {
		return nil, newRedisError("SMEMBERS", keywordKey(o.name), err)
	}
	return o.engines.engineFor(kws), nil
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
