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
	ctx         context.Context
	redisClient *redis.Client // redis client
	name        string        // Pattern's collection name
	logger      *log.Logger   // logger
}

type AhoCorasickInfo struct {
	Keywords int // Aho-Corasick keywords count
	Nodes    int // Aho-Corasick nodes count
}

func Create(args *AhoCorasickArgs) *AhoCorasick {
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
	ac.init()
	return ac
}

func (ac *AhoCorasick) init() {
	// Init trie root
	prefixKey := fmt.Sprintf(PrefixKey, ac.name)
	member := &redis.Z{
		Score:  initScore,
		Member: "",
	}
	ac.redisClient.ZAdd(ac.ctx, prefixKey, member).Val()
}

func (ac *AhoCorasick) Close() error {
	if ac.redisClient != nil {
		return ac.redisClient.Close()
	}
	return ErrRedisAlreadyClosed
}

func (ac *AhoCorasick) Add(keyword string) int {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	keywordKey := fmt.Sprintf(KeywordKey, ac.name)
	addedCount := ac.redisClient.SAdd(ac.ctx, keywordKey, keyword).Val()
	ac.logger.Println(fmt.Sprintf(`Add(%s) > SADD {"key": "%s", "member": "%s", "count": %d}`, keyword, keywordKey, keyword, addedCount))

	ac._buildTrie(keyword)

	return int(addedCount)
}

func (ac *AhoCorasick) Remove(keyword string) int {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	nodeKey := fmt.Sprintf(NodeKey, keyword)
	nodes := ac.redisClient.SMembers(ac.ctx, nodeKey).Val()
	for _, node := range nodes {
		oKey := fmt.Sprintf(OutputKey, node)
		removedCount := ac.redisClient.SRem(ac.ctx, oKey, keyword).Val()
		ac.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s) : Count(%d)", keyword, oKey, removedCount))
	}

	delCount := ac.redisClient.Del(ac.ctx, nodeKey).Val()
	ac.logger.Println(fmt.Sprintf("Remove(%s) > DEL key(%s) : Count(%d)", keyword, nodeKey, delCount))

	keywordRunes := []rune(keyword)
	for idx := len(keywordRunes); idx >= 0; idx-- {
		prefix := string(keywordRunes[:idx])
		suffix := utils.Reverse(prefix)

		sKey := fmt.Sprintf(SuffixKey, ac.name)
		kKey := fmt.Sprintf(KeywordKey, ac.name)
		kExists := ac.redisClient.SIsMember(ac.ctx, kKey, prefix).Val()
		if kExists && idx != len(keywordRunes) {
			break
		}

		pKey := fmt.Sprintf(PrefixKey, ac.name)
		pZRank, err := ac.redisClient.ZRank(ac.ctx, pKey, prefix).Result()
		if err == redis.Nil {
			break
		}
		pKeywords, err := ac.redisClient.ZRange(ac.ctx, pKey, pZRank+1, pZRank+1).Result()
		if err == redis.Nil {
			pRemovedCount := ac.redisClient.ZRem(ac.ctx, pKey, prefix).Val()
			ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, pKey, pRemovedCount))

			sRemovedCount := ac.redisClient.ZRem(ac.ctx, sKey, suffix).Val()
			ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, sKey, sRemovedCount))
		} else if len(pKeywords) > 0 {
			pKeyword := pKeywords[0]
			if !strings.HasPrefix(pKeyword, prefix) {
				pRemovedCount := ac.redisClient.ZRem(ac.ctx, pKey, prefix).Val()
				ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, pKey, pRemovedCount))

				sRemovedCount := ac.redisClient.ZRem(ac.ctx, sKey, suffix).Val()
				ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, sKey, sRemovedCount))
			} else {
				break
			}
		}
	}

	kKey := fmt.Sprintf(KeywordKey, ac.name)
	kRemovedCount := ac.redisClient.SRem(ac.ctx, kKey, keyword).Val()
	ac.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s) members(%s) : Count(%d)", keyword, kKey, keyword, kRemovedCount))

	kMemberCount := ac.redisClient.SCard(ac.ctx, kKey).Val()
	ac.logger.Println(fmt.Sprintf("Remove(%s) > SCARD key(%s) : Count(%d)", keyword, kKey, kMemberCount))

	return int(kMemberCount)
}

func (ac *AhoCorasick) Find(text string) []string {
	matched := make([]string, 0)
	state := ""

	for _, char := range text {
		nextState := ac._go(state, char)
		if nextState == "" {
			nextState = ac._fail(state)
			afterNextState := ac._go(nextState, char)
			if afterNextState == "" {
				buffer := bytes.NewBufferString(nextState)
				buffer.WriteRune(char)
				afterNextState = ac._fail(buffer.String())
			}
			nextState = afterNextState
		}

		outputs := ac._output(state)
		matched = append(matched, outputs...)
		state = nextState
	}

	outputs := ac._output(state)
	matched = append(matched, outputs...)
	ac.logger.Println(fmt.Sprintf("Find(%s) > Matched(%s) : Count(%d)", text, matched, len(matched)))

	return matched
}

func (ac *AhoCorasick) Flush() {
	kKey := fmt.Sprintf(KeywordKey, ac.name)
	pKey := fmt.Sprintf(PrefixKey, ac.name)
	sKey := fmt.Sprintf(SuffixKey, ac.name)

	keywords := ac.redisClient.SMembers(ac.ctx, kKey).Val()
	ac.logger.Println(fmt.Sprintf("Flush() > SMEMBERS Key(%s) : Members(%s)", kKey, keywords))

	for _, keyword := range keywords {
		oKey := fmt.Sprintf(OutputKey, keyword)
		oDelCount := ac.redisClient.Del(ac.ctx, oKey).Val()
		ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", oKey, oDelCount))

		nKey := fmt.Sprintf(NodeKey, keyword)
		nDelCount := ac.redisClient.Del(ac.ctx, nKey).Val()
		ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", nKey, nDelCount))
	}

	pDelCount := ac.redisClient.Del(ac.ctx, pKey).Val()
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", pKey, pDelCount))

	sDelCount := ac.redisClient.Del(ac.ctx, sKey).Val()
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", sKey, sDelCount))

	kDelCount := ac.redisClient.Del(ac.ctx, kKey).Val()
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", kKey, kDelCount))
}

func (ac *AhoCorasick) Info() *AhoCorasickInfo {
	kKey := fmt.Sprintf(KeywordKey, ac.name)
	kCount := ac.redisClient.SCard(ac.ctx, kKey).Val()
	ac.logger.Println(fmt.Sprintf("Info() > SCARD Key(%s) : Count(%d)", kKey, kCount))

	nKey := fmt.Sprintf(PrefixKey, ac.name)
	nCount := ac.redisClient.ZCard(ac.ctx, nKey).Val()
	ac.logger.Println(fmt.Sprintf("Info() > ZCARD Key(%s) : Count(%d)", nKey, nCount))

	return &AhoCorasickInfo{
		Keywords: int(kCount),
		Nodes:    int(nCount),
	}
}

func (ac *AhoCorasick) Suggest(input string) []string {
	var pKeywords []string

	results := make([]string, 0)

	kKey := fmt.Sprintf(KeywordKey, ac.name)
	pKey := fmt.Sprintf(PrefixKey, ac.name)
	pZRank := ac.redisClient.ZRank(ac.ctx, pKey, input).Val()

	pKeywords = ac.redisClient.ZRange(ac.ctx, pKey, pZRank, pZRank).Val()
	for len(pKeywords) > 0 {
		pKeyword := pKeywords[0]
		kExists := ac.redisClient.SIsMember(ac.ctx, kKey, pKeyword).Val()
		if kExists && strings.HasPrefix(pKeyword, input) {
			results = append(results, pKeyword)
		}

		pZRank++
		pKeywords = ac.redisClient.ZRange(ac.ctx, pKey, pZRank, pZRank).Val()
	}

	return results
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
		outputs = append(outputs, ac._output(keyword)...)
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

func (ac *AhoCorasick) _go(inState string, input rune) string {
	buffer := bytes.NewBufferString(inState)
	buffer.WriteRune(input)
	nextState := buffer.String()

	pKey := fmt.Sprintf(PrefixKey, ac.name)
	err := ac.redisClient.ZScore(ac.ctx, pKey, nextState).Err()
	if err == redis.Nil {
		return ""
	}
	return nextState
}

func (ac *AhoCorasick) _fail(inState string) string {
	pKey := fmt.Sprintf(PrefixKey, ac.name)
	idx := 0
	inStateRunes := []rune(inState)
	for idx < len(inStateRunes) {
		nextState := string(inStateRunes[idx+1:])
		err := ac.redisClient.ZScore(ac.ctx, pKey, nextState).Err()
		if err != redis.Nil {
			return nextState
		}
		idx++
	}
	return ""
}

func (ac *AhoCorasick) _output(inState string) []string {
	oKey := fmt.Sprintf(OutputKey, inState)
	oKeywords, err := ac.redisClient.SMembers(ac.ctx, oKey).Result()
	if err != redis.Nil {
		return oKeywords
	}
	return make([]string, 0)
}

func (ac *AhoCorasick) _buildTrie(keyword string) {
	keywordRunes := []rune(keyword)
	for idx := range keywordRunes {
		prefix := string(keywordRunes[:idx+1])
		suffix := utils.Reverse(prefix)

		ac.logger.Printf("_buildTrie(%s) > Prefix(%s) Suffix(%s)", keyword, prefix, suffix)

		pKey := fmt.Sprintf(PrefixKey, ac.name)
		err := ac.redisClient.ZScore(ac.ctx, pKey, prefix).Err()
		if err == redis.Nil {
			pMember := &redis.Z{
				Score:  memberScore,
				Member: prefix,
			}
			pAddedCount := ac.redisClient.ZAdd(ac.ctx, pKey, pMember).Val()
			ac.logger.Println(fmt.Sprintf("_buildTrie(%s) > ZADD key(%s) member(%v) : Count(%d)", keyword, pKey, pMember, pAddedCount))

			sKey := fmt.Sprintf(SuffixKey, ac.name)
			sMember := &redis.Z{
				Score:  memberScore,
				Member: suffix,
			}
			sAddedCount := ac.redisClient.ZAdd(ac.ctx, sKey, sMember).Val()
			ac.logger.Println(fmt.Sprintf("_buildTrie(%s) > ZADD key(%s) member(%v) : Count(%d)", keyword, sKey, sMember, sAddedCount))

			ac._rebuildOutput(suffix)
		} else {
			kKey := fmt.Sprintf(KeywordKey, ac.name)
			kExists := ac.redisClient.SIsMember(ac.ctx, kKey, prefix).Val()
			ac.logger.Println(fmt.Sprintf("_buildTrie(%s) > SISMEMBER key(%s) member(%v) : Exist(%t)", keyword, kKey, prefix, kExists))
			if kExists {
				ac._rebuildOutput(suffix)
			}
		}
	}
}

func (ac *AhoCorasick) _rebuildOutput(suffix string) {
	var sKeywords []string

	sKey := fmt.Sprintf(SuffixKey, ac.name)
	sZRank := ac.redisClient.ZRank(ac.ctx, sKey, suffix).Val()

	sKeywords = ac.redisClient.ZRange(ac.ctx, sKey, sZRank, sZRank).Val()
	for len(sKeywords) > 0 {
		ac.logger.Printf("_rebuildOutput(%s) > Key(%s) ZRank(%d) Keywords(%s)", suffix, sKey, sZRank, sKeywords)

		sKeyword := sKeywords[0]
		if strings.HasPrefix(sKeyword, suffix) {
			state := utils.Reverse(sKeyword)
			ac._buildOutput(state)
		} else {
			break
		}

		sZRank++
		sKeywords = ac.redisClient.ZRange(ac.ctx, sKey, sZRank, sZRank).Val()
	}
}

func (ac *AhoCorasick) _buildOutput(state string) {
	outputs := make([]string, 0)

	kKey := fmt.Sprintf(KeywordKey, ac.name)
	kExists := ac.redisClient.SIsMember(ac.ctx, kKey, state).Val()
	if kExists {
		outputs = append(outputs, state)
	}

	failState := ac._fail(state)
	failOutputs := ac._output(failState)
	if len(failOutputs) > 0 {
		outputs = append(outputs, failOutputs...)
	}

	if len(outputs) > 0 {
		oKey := fmt.Sprintf(OutputKey, state)
		args := make([]interface{}, len(outputs))
		for i, v := range outputs {
			args[i] = v
		}
		oAddedCount := ac.redisClient.SAdd(ac.ctx, oKey, args...).Val()
		ac.logger.Println(fmt.Sprintf("_buildOutput(%s) > SADD key(%s) member(%s) : Count(%d)", state, oKey, args, oAddedCount))

		for _, output := range outputs {
			nKey := fmt.Sprintf(NodeKey, output)
			nAddedCount := ac.redisClient.SAdd(ac.ctx, nKey, state).Val()
			ac.logger.Println(fmt.Sprintf("_buildOutput(%s) > SADD key(%s) member(%s) : Count(%d)", state, nKey, state, nAddedCount))
		}
	}
}
