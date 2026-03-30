package acor

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	redis "github.com/go-redis/redis/v8"
)

// Compile-time check that v1Operations satisfies the operations interface.
var _ operations = (*v1Operations)(nil)

// v1Operations implements the operations interface for the V1 schema.
// It holds all dependencies needed for V1 Aho-Corasick operations.
// The ac field provides access to trie.go helper methods (buildTrie, pruneTrie,
// gotoNode, failNode, collectOutputs, appendMatchedIndexes) which are methods
// on *AhoCorasick. This is a temporary bridge until trie.go is refactored in T15.
type v1Operations struct {
	storage KVStorage
	name    string
	ctx     context.Context
	logger  Logger
	ac      *AhoCorasick // for trie.go helper access (temporary, cleaned up in T15)
}

// --- operations interface methods ---

func (o *v1Operations) add(ctx context.Context, keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

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
		if _, rollbackErr := o.ac.removeV1(ctx, keyword); rollbackErr != nil {
			return 0, fmt.Errorf("build trie: %w; rollback keyword: %v", err, rollbackErr)
		}
		return 0, err
	}

	return 1, nil
}

func (o *v1Operations) remove(ctx context.Context, keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	nodeKey := nodeKey(o.name, keyword)
	nodes, err := o.storage.SMembers(ctx, nodeKey)
	if err != nil {
		return 0, newRedisError("SMEMBERS", nodeKey, err)
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
		return 0, pruneErr
	}

	kKey := keywordKey(o.name)
	if sremErr := o.storage.SRem(ctx, kKey, keyword); sremErr != nil {
		return 0, newRedisError("SREM", kKey, sremErr)
	}
	o.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s) members(%s)", keyword, kKey, keyword))

	kMemberCount, err := o.storage.SCard(ctx, kKey)
	if err != nil {
		return 0, newRedisError("SCARD", kKey, err)
	}
	o.logger.Println(fmt.Sprintf("Remove(%s) > SCARD key(%s) : Count(%d)", keyword, kKey, kMemberCount))

	return int(kMemberCount), nil
}

func (o *v1Operations) find(ctx context.Context, text string) ([]string, error) {
	matched := make([]string, 0)
	state := ""

	for _, char := range text {
		nextState, err := o.ac.goWithContext(ctx, state, char)
		if err != nil {
			return nil, newOperationError("find", SchemaV1, err)
		}
		if nextState == "" {
			nextState, err = o.ac.failWithContext(ctx, state)
			if err != nil {
				return nil, newOperationError("find", SchemaV1, err)
			}
			var afterNextState string
			afterNextState, err = o.ac.goWithContext(ctx, nextState, char)
			if err != nil {
				return nil, newOperationError("find", SchemaV1, err)
			}
			if afterNextState == "" {
				buffer := bytes.NewBufferString(nextState)
				buffer.WriteRune(char)
				afterNextState, err = o.ac.failWithContext(ctx, buffer.String())
				if err != nil {
					return nil, newOperationError("find", SchemaV1, err)
				}
			}
			nextState = afterNextState
		}

		outputs, err := o.ac.outputWithContext(ctx, state)
		if err != nil {
			return nil, newOperationError("find", SchemaV1, err)
		}
		matched = append(matched, outputs...)
		state = nextState
	}

	outputs, err := o.ac.outputWithContext(ctx, state)
	if err != nil {
		return nil, newOperationError("find", SchemaV1, err)
	}
	matched = append(matched, outputs...)
	o.logger.Println(fmt.Sprintf("Find(%s) > Matched(%s) : Count(%d)", text, matched, len(matched)))

	return matched, nil
}

func (o *v1Operations) findIndex(ctx context.Context, text string) (map[string][]int, error) {
	matched := make(map[string][]int)
	state := ""
	runeIndex := 0

	for _, char := range text {
		nextState, err := o.ac.goWithContext(ctx, state, char)
		if err != nil {
			return nil, newOperationError("findIndex", SchemaV1, err)
		}
		if nextState == "" {
			nextState, err = o.ac.failWithContext(ctx, state)
			if err != nil {
				return nil, newOperationError("findIndex", SchemaV1, err)
			}
			var afterNextState string
			afterNextState, err = o.ac.goWithContext(ctx, nextState, char)
			if err != nil {
				return nil, newOperationError("findIndex", SchemaV1, err)
			}
			if afterNextState == "" {
				buffer := bytes.NewBufferString(nextState)
				buffer.WriteRune(char)
				afterNextState, err = o.ac.failWithContext(ctx, buffer.String())
				if err != nil {
					return nil, newOperationError("findIndex", SchemaV1, err)
				}
			}
			nextState = afterNextState
		}

		outputs, err := o.ac.outputWithContext(ctx, state)
		if err != nil {
			return nil, newOperationError("findIndex", SchemaV1, err)
		}
		o.ac.appendMatchedIndexesWithContext(ctx, matched, outputs, runeIndex)
		state = nextState
		runeIndex++
	}

	outputs, err := o.ac.outputWithContext(ctx, state)
	if err != nil {
		return nil, newOperationError("findIndex", SchemaV1, err)
	}
	o.ac.appendMatchedIndexesWithContext(ctx, matched, outputs, runeIndex)
	o.logger.Println(fmt.Sprintf("FindIndex(%s) > Matched(%v) : Count(%d)", text, matched, len(matched)))

	return matched, nil
}

func (o *v1Operations) flush(ctx context.Context) error {
	kKey := keywordKey(o.name)
	pKey := prefixKey(o.name)
	sKey := suffixKey(o.name)

	keywords, err := o.storage.SMembers(ctx, kKey)
	if err != nil {
		return newRedisError("SMEMBERS", kKey, err)
	}
	o.logger.Println(fmt.Sprintf("Flush() > SMEMBERS Key(%s) : Members(%s)", kKey, keywords))

	for _, keyword := range keywords {
		oKey := outputKey(o.name, keyword)
		if err := o.storage.Del(ctx, oKey); err != nil {
			return newRedisError("DEL", oKey, err)
		}
		o.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s)", oKey))

		nKey := nodeKey(o.name, keyword)
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
	var pKeywords []string

	results := make([]string, 0)

	kKey := keywordKey(o.name)
	pKey := prefixKey(o.name)
	pZRank, err := o.storage.ZRank(ctx, pKey, input)
	if err == redis.Nil {
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
	}

	return results, nil
}

func (o *v1Operations) suggestIndex(ctx context.Context, input string) (map[string][]int, error) {
	var pKeywords []string

	results := make(map[string][]int)

	kKey := keywordKey(o.name)
	pKey := prefixKey(o.name)
	pZRank, err := o.storage.ZRank(ctx, pKey, input)
	if err == redis.Nil {
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
			results[pKeyword] = append(results[pKeyword], 0)
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
