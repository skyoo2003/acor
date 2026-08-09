// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"strings"
	"testing"

	redis "github.com/redis/go-redis/v9"
)

// The V1 writer, in full, and reachable only from tests.
//
// Add and Remove return ErrV1ReadOnly in every build as of v1.5.0, so nothing in
// production reaches any of this. It is kept rather than replaced by hand-written
// Redis fixtures because the read and migration paths must be exercised against
// the real V1 layout: a fixture that approximated the trie would let those paths
// pass against data no release ever wrote.
//
// Living in a _test.go file is what makes that guarantee structural. No
// production binary contains a way to write a V1 collection, and the code cannot
// drift back onto a reachable path without someone moving it out of here.
// TestV1WritesAreClosed covers the closed path itself.

// v1WritableOps re-opens the V1 write path for fixtures.
//
// hook fires for each prefix buildTrie visits, so a test can fail the trie build
// partway and assert the rollback. It lives here rather than on AhoCorasick
// because only this fixture writer ever consults it.
type v1WritableOps struct {
	*v1Operations
	ac   *AhoCorasick
	hook func(prefix string) error
}

func (o *v1WritableOps) add(ctx context.Context, keyword string) (int, error) {
	return o.writeKeyword(ctx, keyword)
}

func (o *v1WritableOps) remove(ctx context.Context, keyword string) (int, error) {
	return o.deleteKeyword(ctx, keyword)
}

// v1Writable swaps ac's operations for the fixture-writable variant and returns ac,
// so a test that builds its own V1 args can stay a one-liner.
func v1Writable(t testing.TB, ac *AhoCorasick) *AhoCorasick {
	t.Helper()
	ops, ok := ac.ops.(*v1Operations)
	if !ok {
		t.Fatalf("v1Writable: not a V1 instance (%T)", ac.ops)
	}
	ac.ops = &v1WritableOps{v1Operations: ops, ac: ac}
	return ac
}

// setV1BuildTrieHook installs fn as the per-prefix hook the fixture writer calls
// during buildTrie, and clears it when the test ends. ac must already be
// fixture-writable (see v1Writable).
func setV1BuildTrieHook(t testing.TB, ac *AhoCorasick, fn func(prefix string) error) {
	t.Helper()
	ops, ok := ac.ops.(*v1WritableOps)
	if !ok {
		t.Fatalf("setV1BuildTrieHook: not a fixture-writable V1 instance (%T)", ac.ops)
	}
	ops.hook = fn
	t.Cleanup(func() { ops.hook = nil })
}

// clearV1BuildTrieHook drops the hook before a test's remaining assertions, for
// tests that keep using ac after the injected failure.
func clearV1BuildTrieHook(t testing.TB, ac *AhoCorasick) {
	t.Helper()
	setV1BuildTrieHook(t, ac, nil)
}

// --- the writer itself, formerly v1Operations.writeKeyword ---

// writeKeyword is the V1 writer that releases before v1.5.0 reached through add.
func (o *v1WritableOps) writeKeyword(ctx context.Context, keyword string) (int, error) {
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

	if err := o.ac.buildTrieWithContext(ctx, keyword, o.hook); err != nil {
		// Intentionally use a fresh context for rollback — the caller's ctx may be
		// canceled (timeout, etc.), but we still need to clean up the partially
		// added keyword to avoid leaving the trie in an inconsistent state.
		rollbackCtx, cancel := context.WithTimeout(context.Background(), o.rollbackTimeout)
		defer cancel()
		if _, rollbackErr := o.deleteKeyword(rollbackCtx, keyword); rollbackErr != nil {
			return 0, newOperationError("add", SchemaV1, fmt.Errorf("build trie: %w; rollback keyword: %v", err, rollbackErr))
		}
		return 0, newOperationError("add", SchemaV1, err)
	}

	return 1, nil
}

// deleteKeyword is writeKeyword's counterpart: the V1 deleter that remove used to
// expose. It stays reachable for writeKeyword's own rollback.
func (o *v1WritableOps) deleteKeyword(_ context.Context, keyword string) (int, error) {
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

// --- the per-character trie walk, formerly trie.go ---

func (ac *AhoCorasick) goWithContext(ctx context.Context, inState string, input rune) (string, error) {
	nextState := inState + string(input)

	pKey := prefixKey(ac.name)
	_, err := ac.storage.ZScore(ctx, pKey, nextState)
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return nextState, nil
}

func (ac *AhoCorasick) failWithContext(ctx context.Context, inState string) (string, error) {
	pKey := prefixKey(ac.name)
	idx := 0
	inStateRunes := []rune(inState)
	for idx < len(inStateRunes) {
		nextState := string(inStateRunes[idx+1:])
		_, err := ac.storage.ZScore(ctx, pKey, nextState)
		if err == redis.Nil {
			idx++
			continue
		}
		if err != nil {
			return "", err
		}
		return nextState, nil
	}
	return "", nil
}

// buildTrieWithContext writes the prefix, suffix, output, and node keys for one
// keyword. hook, when non-nil, is called with each prefix as it is written, so a
// test can fail the build partway; nil means no injection.
func (ac *AhoCorasick) buildTrieWithContext(ctx context.Context, keyword string, hook func(prefix string) error) error {
	keywordRunes := []rune(keyword)
	for idx := range keywordRunes {
		prefix := string(keywordRunes[:idx+1])
		suffix := reverse(prefix)

		ac.logger.Printf("buildTrie(%s) > Prefix(%s) Suffix(%s)", keyword, prefix, suffix)

		pKey := prefixKey(ac.name)
		_, err := ac.storage.ZScore(ctx, pKey, prefix)
		if err == redis.Nil {
			sKey := suffixKey(ac.name)
			pMember := &zMember{Score: memberScore, Member: prefix}
			sMember := &zMember{Score: memberScore, Member: suffix}
			if pipeErr := ac.storage.TxPipelined(ctx, func(pipe pipeliner) error {
				_ = pipe.ZAdd(ctx, pKey, pMember)
				_ = pipe.ZAdd(ctx, sKey, sMember)
				return nil
			}); pipeErr != nil {
				return pipeErr
			}
			if hook != nil {
				if hookErr := hook(prefix); hookErr != nil {
					return hookErr
				}
			}

			if rebuildErr := ac.rebuildOutputWithContext(ctx, suffix); rebuildErr != nil {
				return rebuildErr
			}
		} else if err != nil {
			return err
		} else {
			kKey := keywordKey(ac.name)
			kExists, err := ac.storage.SIsMember(ctx, kKey, prefix)
			if err != nil {
				return err
			}
			ac.logger.Printf("buildTrie(%s) > SISMEMBER key(%s) member(%v) : Exist(%t)", keyword, kKey, prefix, kExists)
			if kExists {
				if rebuildErr := ac.rebuildOutputWithContext(ctx, suffix); rebuildErr != nil {
					return rebuildErr
				}
			}
		}
	}

	return nil
}

func (ac *AhoCorasick) rebuildOutputWithContext(ctx context.Context, suffix string) error {
	var sKeywords []string

	sKey := suffixKey(ac.name)
	sZRank, err := ac.storage.ZRank(ctx, sKey, suffix)
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}

	sKeywords, err = ac.storage.ZRange(ctx, sKey, sZRank, sZRank)
	if err != nil {
		return err
	}
	for len(sKeywords) > 0 {
		ac.logger.Printf("rebuildOutput(%s) > Key(%s) ZRank(%d) Keywords(%v)", suffix, sKey, sZRank, sKeywords)

		sKeyword := sKeywords[0]
		if strings.HasPrefix(sKeyword, suffix) {
			state := reverse(sKeyword)
			if buildErr := ac.buildOutputWithContext(ctx, state); buildErr != nil {
				return buildErr
			}
		} else {
			break
		}

		sZRank++
		sKeywords, err = ac.storage.ZRange(ctx, sKey, sZRank, sZRank)
		if err != nil {
			return err
		}
	}

	return nil
}

func (ac *AhoCorasick) buildOutputWithContext(ctx context.Context, state string) error {
	outputs := make([]string, 0)

	kKey := keywordKey(ac.name)
	kExists, err := ac.storage.SIsMember(ctx, kKey, state)
	if err != nil {
		return err
	}
	if kExists {
		outputs = append(outputs, state)
	}

	failState, err := ac.failWithContext(ctx, state)
	if err != nil {
		return err
	}
	failOutputs, err := ac.outputWithContext(ctx, failState)
	if err != nil {
		return err
	}
	if len(failOutputs) > 0 {
		outputs = append(outputs, failOutputs...)
	}

	if len(outputs) > 0 {
		oKey := outputKey(ac.name, state)
		args := make([]interface{}, len(outputs))
		for i, v := range outputs {
			args[i] = v
		}
		if pipeErr := ac.storage.TxPipelined(ctx, func(pipe pipeliner) error {
			_ = pipe.SAdd(ctx, oKey, args...)
			for _, output := range outputs {
				nKey := nodeKey(ac.name, output)
				_ = pipe.SAdd(ctx, nKey, state)
			}
			return nil
		}); pipeErr != nil {
			return pipeErr
		}
	}

	return nil
}

func (ac *AhoCorasick) pruneTrieWithContext(ctx context.Context, keyword string) error {
	keywordRunes := []rune(keyword)
	for idx := len(keywordRunes); idx > 0; idx-- {
		prefix := string(keywordRunes[:idx])
		suffix := reverse(prefix)

		kKey := keywordKey(ac.name)
		kExists, err := ac.storage.SIsMember(ctx, kKey, prefix)
		if err != nil {
			return err
		}
		if kExists && idx != len(keywordRunes) {
			break
		}

		pKey := prefixKey(ac.name)
		pZRank, err := ac.storage.ZRank(ctx, pKey, prefix)
		if err == redis.Nil {
			break
		}
		if err != nil {
			return err
		}

		pKeywords, err := ac.storage.ZRange(ctx, pKey, pZRank+1, pZRank+1)
		if err != nil {
			return err
		}
		if len(pKeywords) > 0 && strings.HasPrefix(pKeywords[0], prefix) {
			break
		}

		if err := ac.removePrefixAndSuffixWithContext(ctx, keyword, prefix, suffix); err != nil {
			return err
		}
	}

	return nil
}

func (ac *AhoCorasick) removePrefixAndSuffixWithContext(ctx context.Context, keyword, prefix, suffix string) error {
	pKey := prefixKey(ac.name)
	err := ac.storage.ZRem(ctx, pKey, prefix)
	if err != nil {
		return err
	}
	ac.logger.Printf("Remove(%s) > ZREM key(%s)", keyword, pKey)

	sKey := suffixKey(ac.name)
	err = ac.storage.ZRem(ctx, sKey, suffix)
	if err != nil {
		return err
	}
	ac.logger.Printf("Remove(%s) > ZREM key(%s)", keyword, sKey)

	return nil
}

func (ac *AhoCorasick) appendMatchedIndexesWithContext(_ context.Context, matched map[string][]int, outputs []string, endIndex int) {
	for _, output := range outputs {
		startIndex := endIndex - len([]rune(output))
		matched[output] = append(matched[output], startIndex)
	}
}
