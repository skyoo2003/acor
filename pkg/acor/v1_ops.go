package acor

import (
	"bytes"
	"fmt"
	"strings"

	redis "github.com/go-redis/redis/v8"
)

func (ac *AhoCorasick) addV1(keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	keywordKey := keywordKey(ac.name)
	addedCount, err := ac.redisClient.SAdd(ac.ctx, keywordKey, keyword).Result()
	if err != nil {
		return 0, err
	}
	ac.logger.Println(fmt.Sprintf(`Add(%s) > SADD {"key": "%s", "member": "%s", "count": %d}`, keyword, keywordKey, keyword, addedCount))

	if err := ac._buildTrie(keyword); err != nil {
		if addedCount == 0 {
			return 0, err
		}

		if _, rollbackErr := ac.Remove(keyword); rollbackErr != nil {
			return 0, fmt.Errorf("build trie: %w; rollback keyword: %v", err, rollbackErr)
		}
		return 0, err
	}

	return int(addedCount), nil
}

func (ac *AhoCorasick) removeV1(keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	nodeKey := nodeKey(ac.name, keyword)
	nodes, err := ac.redisClient.SMembers(ac.ctx, nodeKey).Result()
	if err != nil {
		return 0, err
	}
	var removedCount int64
	for _, node := range nodes {
		oKey := outputKey(ac.name, node)
		removedCount, err = ac.redisClient.SRem(ac.ctx, oKey, keyword).Result()
		if err != nil {
			return 0, err
		}
		ac.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s) : Count(%d)", keyword, oKey, removedCount))
	}

	delCount, err := ac.redisClient.Del(ac.ctx, nodeKey).Result()
	if err != nil {
		return 0, err
	}
	ac.logger.Println(fmt.Sprintf("Remove(%s) > DEL key(%s) : Count(%d)", keyword, nodeKey, delCount))

	err = ac.pruneTrie(keyword)
	if err != nil {
		return 0, err
	}

	kKey := keywordKey(ac.name)
	kRemovedCount, err := ac.redisClient.SRem(ac.ctx, kKey, keyword).Result()
	if err != nil {
		return 0, err
	}
	ac.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s) members(%s) : Count(%d)", keyword, kKey, keyword, kRemovedCount))

	kMemberCount, err := ac.redisClient.SCard(ac.ctx, kKey).Result()
	if err != nil {
		return 0, err
	}
	ac.logger.Println(fmt.Sprintf("Remove(%s) > SCARD key(%s) : Count(%d)", keyword, kKey, kMemberCount))

	return int(kMemberCount), nil
}

func (ac *AhoCorasick) findV1(text string) ([]string, error) {
	matched := make([]string, 0)
	state := ""

	for _, char := range text {
		nextState, err := ac._go(state, char)
		if err != nil {
			return nil, err
		}
		if nextState == "" {
			nextState, err = ac._fail(state)
			if err != nil {
				return nil, err
			}
			var afterNextState string
			afterNextState, err = ac._go(nextState, char)
			if err != nil {
				return nil, err
			}
			if afterNextState == "" {
				buffer := bytes.NewBufferString(nextState)
				buffer.WriteRune(char)
				afterNextState, err = ac._fail(buffer.String())
				if err != nil {
					return nil, err
				}
			}
			nextState = afterNextState
		}

		outputs, err := ac._output(state)
		if err != nil {
			return nil, err
		}
		matched = append(matched, outputs...)
		state = nextState
	}

	outputs, err := ac._output(state)
	if err != nil {
		return nil, err
	}
	matched = append(matched, outputs...)
	ac.logger.Println(fmt.Sprintf("Find(%s) > Matched(%s) : Count(%d)", text, matched, len(matched)))

	return matched, nil
}

func (ac *AhoCorasick) findIndexV1(text string) (map[string][]int, error) {
	matched := make(map[string][]int)
	state := ""
	runeIndex := 0

	for _, char := range text {
		nextState, err := ac._go(state, char)
		if err != nil {
			return nil, err
		}
		if nextState == "" {
			nextState, err = ac._fail(state)
			if err != nil {
				return nil, err
			}
			var afterNextState string
			afterNextState, err = ac._go(nextState, char)
			if err != nil {
				return nil, err
			}
			if afterNextState == "" {
				buffer := bytes.NewBufferString(nextState)
				buffer.WriteRune(char)
				afterNextState, err = ac._fail(buffer.String())
				if err != nil {
					return nil, err
				}
			}
			nextState = afterNextState
		}

		outputs, err := ac._output(state)
		if err != nil {
			return nil, err
		}
		ac.appendMatchedIndexes(matched, outputs, runeIndex)
		state = nextState
		runeIndex++
	}

	outputs, err := ac._output(state)
	if err != nil {
		return nil, err
	}
	ac.appendMatchedIndexes(matched, outputs, runeIndex)
	ac.logger.Println(fmt.Sprintf("FindIndex(%s) > Matched(%v) : Count(%d)", text, matched, len(matched)))

	return matched, nil
}

func (ac *AhoCorasick) flushV1() error {
	kKey := keywordKey(ac.name)
	pKey := prefixKey(ac.name)
	sKey := suffixKey(ac.name)

	keywords, err := ac.redisClient.SMembers(ac.ctx, kKey).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Flush() > SMEMBERS Key(%s) : Members(%s)", kKey, keywords))

	for _, keyword := range keywords {
		oKey := outputKey(ac.name, keyword)
		var oDelCount int64
		oDelCount, err = ac.redisClient.Del(ac.ctx, oKey).Result()
		if err != nil {
			return err
		}
		ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", oKey, oDelCount))

		nKey := nodeKey(ac.name, keyword)
		var nDelCount int64
		nDelCount, err = ac.redisClient.Del(ac.ctx, nKey).Result()
		if err != nil {
			return err
		}
		ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", nKey, nDelCount))
	}

	pDelCount, err := ac.redisClient.Del(ac.ctx, pKey).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", pKey, pDelCount))

	sDelCount, err := ac.redisClient.Del(ac.ctx, sKey).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", sKey, sDelCount))

	kDelCount, err := ac.redisClient.Del(ac.ctx, kKey).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", kKey, kDelCount))

	return nil
}

func (ac *AhoCorasick) infoV1() (*AhoCorasickInfo, error) {
	kKey := keywordKey(ac.name)
	kCount, err := ac.redisClient.SCard(ac.ctx, kKey).Result()
	if err != nil {
		return nil, err
	}
	ac.logger.Println(fmt.Sprintf("Info() > SCARD Key(%s) : Count(%d)", kKey, kCount))

	nKey := prefixKey(ac.name)
	nCount, err := ac.redisClient.ZCard(ac.ctx, nKey).Result()
	if err != nil {
		return nil, err
	}
	ac.logger.Println(fmt.Sprintf("Info() > ZCARD Key(%s) : Count(%d)", nKey, nCount))

	return &AhoCorasickInfo{
		Keywords: int(kCount),
		Nodes:    int(nCount),
	}, nil
}

func (ac *AhoCorasick) suggestV1(input string) ([]string, error) {
	var pKeywords []string

	results := make([]string, 0)

	kKey := keywordKey(ac.name)
	pKey := prefixKey(ac.name)
	pZRank, err := ac.redisClient.ZRank(ac.ctx, pKey, input).Result()
	if err == redis.Nil {
		return results, nil
	}
	if err != nil {
		return nil, err
	}

	pKeywords, err = ac.redisClient.ZRange(ac.ctx, pKey, pZRank, pZRank).Result()
	if err != nil {
		return nil, err
	}
	for len(pKeywords) > 0 {
		pKeyword := pKeywords[0]
		kExists, err := ac.redisClient.SIsMember(ac.ctx, kKey, pKeyword).Result()
		if err != nil {
			return nil, err
		}
		if kExists && strings.HasPrefix(pKeyword, input) {
			results = append(results, pKeyword)
		}

		pZRank++
		pKeywords, err = ac.redisClient.ZRange(ac.ctx, pKey, pZRank, pZRank).Result()
		if err != nil {
			return nil, err
		}
	}

	return results, nil
}

func (ac *AhoCorasick) suggestIndexV1(input string) (map[string][]int, error) {
	var pKeywords []string

	results := make(map[string][]int)

	kKey := keywordKey(ac.name)
	pKey := prefixKey(ac.name)
	pZRank, err := ac.redisClient.ZRank(ac.ctx, pKey, input).Result()
	if err == redis.Nil {
		return results, nil
	}
	if err != nil {
		return nil, err
	}

	pKeywords, err = ac.redisClient.ZRange(ac.ctx, pKey, pZRank, pZRank).Result()
	if err != nil {
		return nil, err
	}
	for len(pKeywords) > 0 {
		pKeyword := pKeywords[0]
		kExists, err := ac.redisClient.SIsMember(ac.ctx, kKey, pKeyword).Result()
		if err != nil {
			return nil, err
		}
		if kExists && strings.HasPrefix(pKeyword, input) {
			results[pKeyword] = append(results[pKeyword], 0)
		}

		pZRank++
		pKeywords, err = ac.redisClient.ZRange(ac.ctx, pKey, pZRank, pZRank).Result()
		if err != nil {
			return nil, err
		}
		if len(pKeywords) > 0 && !strings.HasPrefix(pKeywords[0], input) {
			break
		}
	}

	return results, nil
}
