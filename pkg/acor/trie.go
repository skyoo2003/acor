package acor

import (
	"bytes"
	"context"
	"strings"

	redis "github.com/go-redis/redis/v8"

	"github.com/skyoo2003/acor/internal/pkg/utils"
)

func (ac *AhoCorasick) _buildTrie(keyword string) error {
	return ac.buildTrieWithContext(ac.ctx, keyword)
}

//nolint:unused
func (ac *AhoCorasick) _rebuildOutput(suffix string) error {
	return ac.rebuildOutputWithContext(ac.ctx, suffix)
}

//nolint:unused
func (ac *AhoCorasick) _buildOutput(state string) error {
	return ac.buildOutputWithContext(ac.ctx, state)
}

func (ac *AhoCorasick) pruneTrie(keyword string) error {
	return ac.pruneTrieWithContext(ac.ctx, keyword)
}

//nolint:unused
func (ac *AhoCorasick) removePrefixAndSuffix(keyword, prefix, suffix string) error {
	return ac.removePrefixAndSuffixWithContext(ac.ctx, keyword, prefix, suffix)
}

func (ac *AhoCorasick) _go(inState string, input rune) (string, error) {
	return ac.goWithContext(ac.ctx, inState, input)
}

func (ac *AhoCorasick) _fail(inState string) (string, error) {
	return ac.failWithContext(ac.ctx, inState)
}

func (ac *AhoCorasick) _output(inState string) ([]string, error) {
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
	err := ac.redisClient.ZScore(ctx, pKey, nextState).Err()
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
		err := ac.redisClient.ZScore(ctx, pKey, nextState).Err()
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
	oKeywords, err := ac.redisClient.SMembers(ctx, oKey).Result()
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

		ac.logger.Printf("_buildTrie(%s) > Prefix(%s) Suffix(%s)", keyword, prefix, suffix)

		pKey := prefixKey(ac.name)
		err := ac.redisClient.ZScore(ctx, pKey, prefix).Err()
		if err == redis.Nil {
			sKey := suffixKey(ac.name)
			pMember := &redis.Z{Score: memberScore, Member: prefix}
			sMember := &redis.Z{Score: memberScore, Member: suffix}
			if _, pipeErr := ac.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
				pipe.ZAdd(ctx, pKey, pMember)
				pipe.ZAdd(ctx, sKey, sMember)
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
			kExists, err := ac.redisClient.SIsMember(ctx, kKey, prefix).Result()
			if err != nil {
				return err
			}
			ac.logger.Printf("_buildTrie(%s) > SISMEMBER key(%s) member(%v) : Exist(%t)", keyword, kKey, prefix, kExists)
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
	sZRank, err := ac.redisClient.ZRank(ctx, sKey, suffix).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}

	sKeywords, err = ac.redisClient.ZRange(ctx, sKey, sZRank, sZRank).Result()
	if err != nil {
		return err
	}
	for len(sKeywords) > 0 {
		ac.logger.Printf("_rebuildOutput(%s) > Key(%s) ZRank(%d) Keywords(%s)", suffix, sKey, sZRank, sKeywords)

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
		sKeywords, err = ac.redisClient.ZRange(ctx, sKey, sZRank, sZRank).Result()
		if err != nil {
			return err
		}
	}

	return nil
}

func (ac *AhoCorasick) buildOutputWithContext(ctx context.Context, state string) error {
	outputs := make([]string, 0)

	kKey := keywordKey(ac.name)
	kExists, err := ac.redisClient.SIsMember(ctx, kKey, state).Result()
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
		if _, pipeErr := ac.redisClient.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
			pipe.SAdd(ctx, oKey, args...)
			for _, output := range outputs {
				nKey := nodeKey(ac.name, output)
				pipe.SAdd(ctx, nKey, state)
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
		kExists, err := ac.redisClient.SIsMember(ctx, kKey, prefix).Result()
		if err != nil {
			return err
		}
		if kExists && idx != len(keywordRunes) {
			break
		}

		pKey := prefixKey(ac.name)
		pZRank, err := ac.redisClient.ZRank(ctx, pKey, prefix).Result()
		if err == redis.Nil {
			break
		}
		if err != nil {
			return err
		}

		pKeywords, err := ac.redisClient.ZRange(ctx, pKey, pZRank+1, pZRank+1).Result()
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
	pRemovedCount, err := ac.redisClient.ZRem(ctx, pKey, prefix).Result()
	if err != nil {
		return err
	}
	ac.logger.Printf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, pKey, pRemovedCount)

	sKey := suffixKey(ac.name)
	sRemovedCount, err := ac.redisClient.ZRem(ctx, sKey, suffix).Result()
	if err != nil {
		return err
	}
	ac.logger.Printf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, sKey, sRemovedCount)

	return nil
}

func (ac *AhoCorasick) appendMatchedIndexesWithContext(_ context.Context, matched map[string][]int, outputs []string, endIndex int) {
	for _, output := range outputs {
		startIndex := endIndex - len([]rune(output))
		matched[output] = append(matched[output], startIndex)
	}
}
