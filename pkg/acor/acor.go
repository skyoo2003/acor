// Package acor means Aho-Corasick automation working On Redis, Written in Go
package acor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"

	redis "github.com/go-redis/redis/v8"

	"github.com/skyoo2003/acor/internal/pkg/utils"
)

// Key type constants
const (
	KeywordKey = "%s:keyword"
	PrefixKey  = "%s:prefix"
	SuffixKey  = "%s:suffix"
	OutputKey  = "%s:output"
	NodeKey    = "%s:node"
)

const (
	initScore   = 0.0
	memberScore = 1.0
)

var (
	ErrRedisAlreadyClosed = errors.New("redis client was already closed")
)

type AhoCorasickArgs struct {
	Addr     string // redis server address (ex) localhost:6379
	Password string // redis password
	DB       int    // redis db number
	Name     string // pattern's collection name
	Debug    bool   // debug flag
}

type AhoCorasick struct {
	ctx           context.Context
	redisClient   *redis.Client // redis client
	name          string        // Pattern's collection name
	logger        *log.Logger   // logger
	buildTrieHook func(string) error
}

type AhoCorasickInfo struct {
	Keywords int // Aho-Corasick keywords count
	Nodes    int // Aho-Corasick nodes count
}

func Create(args *AhoCorasickArgs) (*AhoCorasick, error) {
	logger := log.New(ioutil.Discard, "ACOR: ", log.LstdFlags|log.Lshortfile)
	if args.Debug {
		logger.SetOutput(os.Stdout)
	}

	ac := &AhoCorasick{
		redisClient: redis.NewClient(&redis.Options{
			Addr:     args.Addr,
			Password: args.Password,
			DB:       args.DB,
		}),
		ctx:    context.Background(),
		name:   args.Name,
		logger: logger,
	}
	if err := ac.init(); err != nil {
		_ = ac.redisClient.Close()
		return nil, err
	}
	return ac, nil
}

func (ac *AhoCorasick) init() error {
	// Init trie root
	prefixKey := fmt.Sprintf(PrefixKey, ac.name)
	member := &redis.Z{
		Score:  initScore,
		Member: "",
	}
	if err := ac.redisClient.ZAdd(ac.ctx, prefixKey, member).Err(); err != nil {
		return err
	}
	return nil
}

func (ac *AhoCorasick) Close() error {
	if ac.redisClient != nil {
		return ac.redisClient.Close()
	}
	return ErrRedisAlreadyClosed
}

func (ac *AhoCorasick) Add(keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	keywordKey := fmt.Sprintf(KeywordKey, ac.name)
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

func (ac *AhoCorasick) Remove(keyword string) (int, error) {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	nodeKey := fmt.Sprintf(NodeKey, keyword)
	nodes, err := ac.redisClient.SMembers(ac.ctx, nodeKey).Result()
	if err != nil {
		return 0, err
	}
	var removedCount int64
	for _, node := range nodes {
		oKey := fmt.Sprintf(OutputKey, node)
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

	kKey := fmt.Sprintf(KeywordKey, ac.name)
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

func (ac *AhoCorasick) pruneTrie(keyword string) error {
	keywordRunes := []rune(keyword)
	for idx := len(keywordRunes); idx >= 0; idx-- {
		prefix := string(keywordRunes[:idx])
		suffix := utils.Reverse(prefix)

		kKey := fmt.Sprintf(KeywordKey, ac.name)
		kExists, err := ac.redisClient.SIsMember(ac.ctx, kKey, prefix).Result()
		if err != nil {
			return err
		}
		if kExists && idx != len(keywordRunes) {
			break
		}

		pKey := fmt.Sprintf(PrefixKey, ac.name)
		pZRank, err := ac.redisClient.ZRank(ac.ctx, pKey, prefix).Result()
		if err == redis.Nil {
			break
		}
		if err != nil {
			return err
		}

		pKeywords, err := ac.redisClient.ZRange(ac.ctx, pKey, pZRank+1, pZRank+1).Result()
		if err != nil {
			return err
		}
		if len(pKeywords) > 0 && strings.HasPrefix(pKeywords[0], prefix) {
			break
		}

		if err := ac.removePrefixAndSuffix(keyword, prefix, suffix); err != nil {
			return err
		}
	}

	return nil
}

func (ac *AhoCorasick) removePrefixAndSuffix(keyword, prefix, suffix string) error {
	pKey := fmt.Sprintf(PrefixKey, ac.name)
	pRemovedCount, err := ac.redisClient.ZRem(ac.ctx, pKey, prefix).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, pKey, pRemovedCount))

	sKey := fmt.Sprintf(SuffixKey, ac.name)
	sRemovedCount, err := ac.redisClient.ZRem(ac.ctx, sKey, suffix).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, sKey, sRemovedCount))

	return nil
}

func (ac *AhoCorasick) Find(text string) ([]string, error) {
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

func (ac *AhoCorasick) FindIndex(text string) (map[string][]int, error) {
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

func (ac *AhoCorasick) Flush() error {
	kKey := fmt.Sprintf(KeywordKey, ac.name)
	pKey := fmt.Sprintf(PrefixKey, ac.name)
	sKey := fmt.Sprintf(SuffixKey, ac.name)

	keywords, err := ac.redisClient.SMembers(ac.ctx, kKey).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Flush() > SMEMBERS Key(%s) : Members(%s)", kKey, keywords))

	for _, keyword := range keywords {
		oKey := fmt.Sprintf(OutputKey, keyword)
		var oDelCount int64
		oDelCount, err = ac.redisClient.Del(ac.ctx, oKey).Result()
		if err != nil {
			return err
		}
		ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", oKey, oDelCount))

		nKey := fmt.Sprintf(NodeKey, keyword)
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

func (ac *AhoCorasick) Info() (*AhoCorasickInfo, error) {
	kKey := fmt.Sprintf(KeywordKey, ac.name)
	kCount, err := ac.redisClient.SCard(ac.ctx, kKey).Result()
	if err != nil {
		return nil, err
	}
	ac.logger.Println(fmt.Sprintf("Info() > SCARD Key(%s) : Count(%d)", kKey, kCount))

	nKey := fmt.Sprintf(PrefixKey, ac.name)
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

func (ac *AhoCorasick) Suggest(input string) ([]string, error) {
	var pKeywords []string

	results := make([]string, 0)

	kKey := fmt.Sprintf(KeywordKey, ac.name)
	pKey := fmt.Sprintf(PrefixKey, ac.name)
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

func (ac *AhoCorasick) SuggestIndex(input string) (map[string][]int, error) {
	var pKeywords []string

	results := make(map[string][]int)

	kKey := fmt.Sprintf(KeywordKey, ac.name)
	pKey := fmt.Sprintf(PrefixKey, ac.name)
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

func (ac *AhoCorasick) Debug() {
	kKey := fmt.Sprintf(KeywordKey, ac.name)
	fmt.Println("-", ac.redisClient.SMembers(ac.ctx, kKey))

	pKey := fmt.Sprintf(PrefixKey, ac.name)
	fmt.Println("-", ac.redisClient.ZRange(ac.ctx, pKey, 0, -1))

	sKey := fmt.Sprintf(SuffixKey, ac.name)
	fmt.Println("-", ac.redisClient.ZRange(ac.ctx, sKey, 0, -1))

	outputs := make([]string, 0)
	pKeywords := ac.redisClient.ZRange(ac.ctx, pKey, 0, -1).Val()
	for _, keyword := range pKeywords {
		oKeywords, err := ac._output(keyword)
		if err != nil {
			fmt.Println("-", err)
			return
		}
		outputs = append(outputs, oKeywords...)
	}
	fmt.Println("-", outputs)

	nodes := make([]string, 0)
	kKeywords := ac.redisClient.SMembers(ac.ctx, kKey).Val()
	for _, keyword := range kKeywords {
		nKey := fmt.Sprintf(NodeKey, keyword)
		nKeywords := ac.redisClient.SMembers(ac.ctx, nKey).Val()
		nodes = append(nodes, nKeywords...)
	}
	fmt.Println("-", nodes)
}

func (ac *AhoCorasick) _go(inState string, input rune) (string, error) {
	buffer := bytes.NewBufferString(inState)
	buffer.WriteRune(input)
	nextState := buffer.String()

	pKey := fmt.Sprintf(PrefixKey, ac.name)
	err := ac.redisClient.ZScore(ac.ctx, pKey, nextState).Err()
	if err == redis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return nextState, nil
}

func (ac *AhoCorasick) _fail(inState string) (string, error) {
	pKey := fmt.Sprintf(PrefixKey, ac.name)
	idx := 0
	inStateRunes := []rune(inState)
	for idx < len(inStateRunes) {
		nextState := string(inStateRunes[idx+1:])
		err := ac.redisClient.ZScore(ac.ctx, pKey, nextState).Err()
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

func (ac *AhoCorasick) _output(inState string) ([]string, error) {
	oKey := fmt.Sprintf(OutputKey, inState)
	oKeywords, err := ac.redisClient.SMembers(ac.ctx, oKey).Result()
	if err == redis.Nil {
		return make([]string, 0), nil
	}
	if err != nil {
		return nil, err
	}
	return oKeywords, nil
}

func (ac *AhoCorasick) appendMatchedIndexes(matched map[string][]int, outputs []string, endIndex int) {
	for _, output := range outputs {
		startIndex := endIndex - len([]rune(output))
		matched[output] = append(matched[output], startIndex)
	}
}

func (ac *AhoCorasick) _buildTrie(keyword string) error {
	keywordRunes := []rune(keyword)
	for idx := range keywordRunes {
		prefix := string(keywordRunes[:idx+1])
		suffix := utils.Reverse(prefix)

		ac.logger.Printf("_buildTrie(%s) > Prefix(%s) Suffix(%s)", keyword, prefix, suffix)

		pKey := fmt.Sprintf(PrefixKey, ac.name)
		err := ac.redisClient.ZScore(ac.ctx, pKey, prefix).Err()
		if err == redis.Nil {
			sKey := fmt.Sprintf(SuffixKey, ac.name)
			pMember := &redis.Z{Score: memberScore, Member: prefix}
			sMember := &redis.Z{Score: memberScore, Member: suffix}
			if _, pipeErr := ac.redisClient.TxPipelined(ac.ctx, func(pipe redis.Pipeliner) error {
				pipe.ZAdd(ac.ctx, pKey, pMember)
				pipe.ZAdd(ac.ctx, sKey, sMember)
				return nil
			}); pipeErr != nil {
				return pipeErr
			}
			if ac.buildTrieHook != nil {
				if hookErr := ac.buildTrieHook(prefix); hookErr != nil {
					return hookErr
				}
			}

			if rebuildErr := ac._rebuildOutput(suffix); rebuildErr != nil {
				return rebuildErr
			}
		} else if err != nil {
			return err
		} else {
			kKey := fmt.Sprintf(KeywordKey, ac.name)
			kExists, err := ac.redisClient.SIsMember(ac.ctx, kKey, prefix).Result()
			if err != nil {
				return err
			}
			ac.logger.Println(fmt.Sprintf("_buildTrie(%s) > SISMEMBER key(%s) member(%v) : Exist(%t)", keyword, kKey, prefix, kExists))
			if kExists {
				if rebuildErr := ac._rebuildOutput(suffix); rebuildErr != nil {
					return rebuildErr
				}
			}
		}
	}

	return nil
}

func (ac *AhoCorasick) _rebuildOutput(suffix string) error {
	var sKeywords []string

	sKey := fmt.Sprintf(SuffixKey, ac.name)
	sZRank, err := ac.redisClient.ZRank(ac.ctx, sKey, suffix).Result()
	if err == redis.Nil {
		return nil
	}
	if err != nil {
		return err
	}

	sKeywords, err = ac.redisClient.ZRange(ac.ctx, sKey, sZRank, sZRank).Result()
	if err != nil {
		return err
	}
	for len(sKeywords) > 0 {
		ac.logger.Printf("_rebuildOutput(%s) > Key(%s) ZRank(%d) Keywords(%s)", suffix, sKey, sZRank, sKeywords)

		sKeyword := sKeywords[0]
		if strings.HasPrefix(sKeyword, suffix) {
			state := utils.Reverse(sKeyword)
			if buildErr := ac._buildOutput(state); buildErr != nil {
				return buildErr
			}
		} else {
			break
		}

		sZRank++
		sKeywords, err = ac.redisClient.ZRange(ac.ctx, sKey, sZRank, sZRank).Result()
		if err != nil {
			return err
		}
	}

	return nil
}

func (ac *AhoCorasick) _buildOutput(state string) error {
	outputs := make([]string, 0)

	kKey := fmt.Sprintf(KeywordKey, ac.name)
	kExists, err := ac.redisClient.SIsMember(ac.ctx, kKey, state).Result()
	if err != nil {
		return err
	}
	if kExists {
		outputs = append(outputs, state)
	}

	failState, err := ac._fail(state)
	if err != nil {
		return err
	}
	failOutputs, err := ac._output(failState)
	if err != nil {
		return err
	}
	if len(failOutputs) > 0 {
		outputs = append(outputs, failOutputs...)
	}

	if len(outputs) > 0 {
		oKey := fmt.Sprintf(OutputKey, state)
		args := make([]interface{}, len(outputs))
		for i, v := range outputs {
			args[i] = v
		}
		if _, pipeErr := ac.redisClient.TxPipelined(ac.ctx, func(pipe redis.Pipeliner) error {
			pipe.SAdd(ac.ctx, oKey, args...)
			for _, output := range outputs {
				nKey := fmt.Sprintf(NodeKey, output)
				pipe.SAdd(ac.ctx, nKey, state)
			}
			return nil
		}); pipeErr != nil {
			return pipeErr
		}
	}

	return nil
}
