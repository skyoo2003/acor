// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"bytes"
	"context"
	"strings"

	redis "github.com/go-redis/redis/v8"

	"github.com/skyoo2003/acor/internal/pkg/utils"
	kvstore "github.com/skyoo2003/acor/internal/storage"
)

func (ac *AhoCorasick) buildTrie(keyword string) error {
	return ac.buildTrieWithContext(ac.ctx, keyword)
}

func (ac *AhoCorasick) gotoNode(inState string, input rune) (string, error) {
	return ac.goWithContext(ac.ctx, inState, input)
}

func (ac *AhoCorasick) failNode(inState string) (string, error) {
	return ac.failWithContext(ac.ctx, inState)
}

func (ac *AhoCorasick) collectOutputs(inState string) ([]string, error) {
	return ac.outputWithContext(ac.ctx, inState)
}

func (ac *AhoCorasick) appendMatchedIndexes(matched map[string][]int, outputs []string, endIndex int) {
	ac.appendMatchedIndexesWithContext(ac.ctx, matched, outputs, endIndex)
}

func (ac *AhoCorasick) goWithContext(ctx context.Context, inState string, input rune) (string, error) {
	buffer := bytes.NewBufferString(inState)
	buffer.WriteRune(input)
	nextState := buffer.String()

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

func (ac *AhoCorasick) outputWithContext(ctx context.Context, inState string) ([]string, error) {
	oKey := outputKey(ac.name, inState)
	oKeywords, err := ac.storage.SMembers(ctx, oKey)
	if err != nil {
		return nil, err
	}
	return oKeywords, nil
}

func (ac *AhoCorasick) buildTrieWithContext(ctx context.Context, keyword string) error {
	keywordRunes := []rune(keyword)
	for idx := range keywordRunes {
		prefix := string(keywordRunes[:idx+1])
		suffix := utils.Reverse(prefix)

		ac.logger.Printf("buildTrie(%s) > Prefix(%s) Suffix(%s)", keyword, prefix, suffix)

		pKey := prefixKey(ac.name)
		_, err := ac.storage.ZScore(ctx, pKey, prefix)
		if err == redis.Nil {
			sKey := suffixKey(ac.name)
			pMember := &kvstore.Z{Score: memberScore, Member: prefix}
			sMember := &kvstore.Z{Score: memberScore, Member: suffix}
			if pipeErr := ac.storage.TxPipelined(ctx, func(pipe kvstore.Pipeliner) error {
				_ = pipe.ZAdd(ctx, pKey, pMember)
				_ = pipe.ZAdd(ctx, sKey, sMember)
				return nil
			}); pipeErr != nil {
				return pipeErr
			}
			if ac.buildTrieHook != nil {
				if hookErr := ac.buildTrieHook(prefix); hookErr != nil {
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
			state := utils.Reverse(sKeyword)
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
		if pipeErr := ac.storage.TxPipelined(ctx, func(pipe kvstore.Pipeliner) error {
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
		suffix := utils.Reverse(prefix)

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
