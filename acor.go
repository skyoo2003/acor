package acor

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/golang/example/stringutil"
	redis "gopkg.in/redis.v5"
)

// Key type constants
const (
	KEYWORD_KEY = "%s:keyword"
	PREFIX_KEY  = "%s:prefix"
	SUFFIX_KEY  = "%s:suffix"
	OUTPUT_KEY  = "%s:output"
	NODE_KEY    = "%s:node"
)

type AhoCorasickArgs struct {
	Addr     string // redis server address (ex) localhost:6379
	Password string // redis password
	DB       int    // redis db number
	Name     string // pattern's collection name
}

type AhoCorasick struct {
	RedisClient *redis.Client // redis client
	Name        string        // Pattern's collection name
	logger      *log.Logger   // logger
}

type AhoCorasickInfo struct {
	Keywords int // Aho-Corasick keywords count
	Nodes    int //Aho-Corasick nodes count
}

func Create(args *AhoCorasickArgs) *AhoCorasick {
	return &AhoCorasick{
		RedisClient: redis.NewClient(&redis.Options{
			Addr:     args.Addr,
			Password: args.Password,
			DB:       args.DB,
		}),
		Name:   args.Name,
		logger: log.New(os.Stdout, "AhoCorasick: ", log.Lshortfile),
	}
}

func (ac *AhoCorasick) Init() {
	// Init trie root
	pKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
	member := redis.Z{
		Score:  0.0,
		Member: "",
	}
	addedCount := ac.RedisClient.ZAdd(pKey, member).Val()
	ac.logger.Println(fmt.Sprintf("Init() > ZADD key(%s) members(%s) : Count(%d)", pKey, member, addedCount))

	return
}

func (ac *AhoCorasick) Close() error {
	if ac.RedisClient != nil {
		return ac.RedisClient.Close()
	}
	return errors.New("redis client was already closed")
}

func (ac *AhoCorasick) Add(keyword string) int {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
	addedCount := ac.RedisClient.SAdd(kKey, keyword).Val()
	ac.logger.Println(fmt.Sprintf("Add(%s) > SADD key(%s) members(%s) : Count(%d)", keyword, kKey, keyword, addedCount))

	ac._buildTrie(keyword)

	resultCount := ac.RedisClient.SCard(kKey).Val()
	ac.logger.Println(fmt.Sprintf("Add(%s) > SCARD key(%s) : Count(%d)", keyword, kKey, resultCount))

	return int(resultCount)
}

func (ac *AhoCorasick) Remove(keyword string) int {
	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	nKey := fmt.Sprintf(NODE_KEY, keyword)
	nodes := ac.RedisClient.SMembers(nKey).Val()
	for _, node := range nodes {
		oKey := fmt.Sprintf(OUTPUT_KEY, node)
		removedCount := ac.RedisClient.SRem(oKey, keyword).Val()
		ac.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s) : Count(%d)", keyword, oKey, removedCount))
	}

	delCount := ac.RedisClient.Del(nKey).Val()
	ac.logger.Println(fmt.Sprintf("Remove(%s) > DEL key(%s) : Count(%d)", keyword, nKey, delCount))

	keywordRunes := []rune(keyword)
	for idx := len(keywordRunes); idx >= 0; idx-- {
		prefix := string(keywordRunes[:idx])
		suffix := stringutil.Reverse(prefix)

		sKey := fmt.Sprintf(SUFFIX_KEY, ac.Name)
		kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
		kExists := ac.RedisClient.SIsMember(kKey, prefix).Val()
		if kExists && idx != len(keywordRunes) {
			break
		}

		pKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
		pZRank, err := ac.RedisClient.ZRank(pKey, prefix).Result()
		if err == redis.Nil {
			break
		}
		pKeywords, err := ac.RedisClient.ZRange(pKey, pZRank+1, pZRank+1).Result()
		if err == redis.Nil {
			pRemovedCount := ac.RedisClient.ZRem(pKey, prefix).Val()
			ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, pKey, pRemovedCount))

			sRemovedCount := ac.RedisClient.ZRem(sKey, suffix).Val()
			ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, sKey, sRemovedCount))
		} else {
			if len(pKeywords) > 0 {
				pKeyword := pKeywords[0]
				if !strings.HasPrefix(pKeyword, prefix) {
					pRemovedCount := ac.RedisClient.ZRem(pKey, prefix).Val()
					ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, pKey, pRemovedCount))

					sRemovedCount := ac.RedisClient.ZRem(sKey, suffix).Val()
					ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, sKey, sRemovedCount))
				} else {
					break
				}
			}
		}
	}

	kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
	kRemovedCount := ac.RedisClient.SRem(kKey, keyword).Val()
	ac.logger.Println(fmt.Sprintf("Remove(%s) > SREM key(%s) members(%s) : Count(%d)", keyword, kKey, keyword, kRemovedCount))

	kMemberCount := ac.RedisClient.SCard(kKey).Val()
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
	kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
	pKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
	sKey := fmt.Sprintf(SUFFIX_KEY, ac.Name)

	keywords := ac.RedisClient.SMembers(kKey).Val()
	ac.logger.Println(fmt.Sprintf("Flush() > SMEMBERS Key(%s) : Members(%s)", kKey, keywords))

	for _, keyword := range keywords {
		oKey := fmt.Sprintf(OUTPUT_KEY, keyword)
		oDelCount := ac.RedisClient.Del(oKey).Val()
		ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", oKey, oDelCount))

		nKey := fmt.Sprintf(NODE_KEY, keyword)
		nDelCount := ac.RedisClient.Del(nKey).Val()
		ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", nKey, nDelCount))
	}

	pDelCount := ac.RedisClient.Del(pKey).Val()
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", pKey, pDelCount))

	sDelCount := ac.RedisClient.Del(sKey).Val()
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", sKey, sDelCount))

	kDelCount := ac.RedisClient.Del(kKey).Val()
	ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", kKey, kDelCount))

	return
}

func (ac *AhoCorasick) Info() *AhoCorasickInfo {
	kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
	kCount := ac.RedisClient.SCard(kKey).Val()
	ac.logger.Println(fmt.Sprintf("Info() > SCARD Key(%s) : Count(%d)", kKey, kCount))

	nKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
	nCount := ac.RedisClient.ZCard(nKey).Val()
	ac.logger.Println(fmt.Sprintf("Info() > ZCARD Key(%s) : Count(%d)", nKey, nCount))

	return &AhoCorasickInfo{
		Keywords: int(kCount),
		Nodes:    int(nCount),
	}
}

func (ac *AhoCorasick) Suggest(input string) []string {
	var pKeywords []string
	var err error

	results := make([]string, 0)

	kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
	pKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
	pZRank := ac.RedisClient.ZRank(pKey, input).Val()

	pKeywords, err = ac.RedisClient.ZRange(pKey, pZRank, pZRank).Result()
	for err != redis.Nil && len(pKeywords) > 0 {
		pKeyword := pKeywords[0]
		kExists := ac.RedisClient.SIsMember(kKey, pKeyword).Val()
		if kExists && strings.HasPrefix(pKeyword, input) {
			results = append(results, pKeyword)
		}

		pZRank = pZRank + 1
		pKeywords, err = ac.RedisClient.ZRange(pKey, pZRank, pZRank).Result()
	}

	return results
}

func (ac *AhoCorasick) Debug() {
	kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
	fmt.Println("-", ac.RedisClient.SMembers(kKey))

	pKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
	fmt.Println("-", ac.RedisClient.ZRange(pKey, 0, -1))

	sKey := fmt.Sprintf(SUFFIX_KEY, ac.Name)
	fmt.Println("-", ac.RedisClient.ZRange(sKey, 0, -1))

	outputs := make([]string, 0)
	pKeywords := ac.RedisClient.ZRange(pKey, 0, -1).Val()
	for _, keyword := range pKeywords {
		outputs = append(outputs, ac._output(keyword)...)
	}
	fmt.Println("-", outputs)

	nodes := make([]string, 0)
	kKeywords := ac.RedisClient.SMembers(kKey).Val()
	for _, keyword := range kKeywords {
		nKey := fmt.Sprintf(NODE_KEY, keyword)
		nKeywords := ac.RedisClient.SMembers(nKey).Val()
		nodes = append(nodes, nKeywords...)
	}
	fmt.Println("-", nodes)

	return
}

func (ac *AhoCorasick) _go(inState string, input rune) string {
	buffer := bytes.NewBufferString(inState)
	buffer.WriteRune(input)
	outState := buffer.String()

	pKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
	err := ac.RedisClient.ZScore(pKey, outState).Err()
	if err == redis.Nil {
		return ""
	}
	return outState
}

func (ac *AhoCorasick) _fail(inState string) string {
	idx := 1
	pKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
	inStateRunes := []rune(inState)
	for idx < len(inStateRunes)+1 {
		outState := string(inStateRunes[idx:])
		err := ac.RedisClient.ZScore(pKey, outState).Err()
		if err != redis.Nil {
			return outState
		}
		idx = idx + 1
	}
	return ""
}

func (ac *AhoCorasick) _output(inState string) []string {
	oKey := fmt.Sprintf(OUTPUT_KEY, inState)
	oKeywords, err := ac.RedisClient.SMembers(oKey).Result()
	if err != redis.Nil {
		return oKeywords
	}
	return make([]string, 0)
}

func (ac *AhoCorasick) _buildTrie(keyword string) {
	keywordRunes := []rune(keyword)
	for idx := range keywordRunes {
		prefix := string(keywordRunes[:idx+1])
		suffix := stringutil.Reverse(prefix)

		ac.logger.Printf("_buildTrie(%s) > Prefix(%s) Suffix(%s)", keyword, prefix, suffix)

		pKey := fmt.Sprintf(PREFIX_KEY, ac.Name)
		err := ac.RedisClient.ZScore(pKey, prefix).Err()
		if err == redis.Nil {
			pMember := redis.Z{
				Score:  1.0,
				Member: prefix,
			}
			pAddedCount := ac.RedisClient.ZAdd(pKey, pMember).Val()
			ac.logger.Println(fmt.Sprintf("_buildTrie(%s) > ZADD key(%s) member(%s) : Count(%d)", keyword, pKey, pMember, pAddedCount))

			sKey := fmt.Sprintf(SUFFIX_KEY, ac.Name)
			sMember := redis.Z{
				Score:  1.0,
				Member: suffix,
			}
			sAddedCount := ac.RedisClient.ZAdd(sKey, sMember).Val()
			ac.logger.Println(fmt.Sprintf("_buildTrie(%s) > ZADD key(%s) member(%s) : Count(%d)", keyword, sKey, sMember, sAddedCount))

			ac._rebuildOutput(suffix)
		} else {
			kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
			kExists := ac.RedisClient.SIsMember(kKey, prefix).Val()
			ac.logger.Println(fmt.Sprintf("_buildTrie(%s) > SISMEMBER key(%s) member(%s) : Exist(%s)", keyword, kKey, prefix, kExists))
			if kExists {
				ac._rebuildOutput(suffix)
			}
		}
	}
}

func (ac *AhoCorasick) _rebuildOutput(suffix string) {
	var sKeywords []string
	var sErr error

	sKey := fmt.Sprintf(SUFFIX_KEY, ac.Name)
	sZRank := ac.RedisClient.ZRank(sKey, suffix).Val()

	sKeywords, sErr = ac.RedisClient.ZRange(sKey, sZRank, sZRank).Result()
	for sErr != redis.Nil && len(sKeywords) > 0 {
		ac.logger.Printf("_rebuildOutput(%s) > Key(%s) ZRank(%d) Keywords(%s)", suffix, sKey, sZRank, sKeywords)

		sKeyword := sKeywords[0]
		if strings.HasPrefix(sKeyword, suffix) {
			state := stringutil.Reverse(sKeyword)
			ac._buildOutput(state)
		} else {
			break
		}

		sZRank = sZRank + 1
		sKeywords, sErr = ac.RedisClient.ZRange(sKey, sZRank, sZRank).Result()
	}

	return
}

func (ac *AhoCorasick) _buildOutput(state string) {
	outputs := make([]string, 0)

	kKey := fmt.Sprintf(KEYWORD_KEY, ac.Name)
	kExists := ac.RedisClient.SIsMember(kKey, state).Val()
	if kExists {
		outputs = append(outputs, state)
	}

	failState := ac._fail(state)
	failOutputs := ac._output(failState)
	if len(failOutputs) > 0 {
		outputs = append(outputs, failOutputs...)
	}

	if len(outputs) > 0 {
		oKey := fmt.Sprintf(OUTPUT_KEY, state)
		args := make([]interface{}, len(outputs))
		for i, v := range outputs {
			args[i] = v
		}
		oAddedCount := ac.RedisClient.SAdd(oKey, args...).Val()
		ac.logger.Println(fmt.Sprintf("_buildOutput(%s) > SADD key(%s) member(%s) : Count(%d)", state, oKey, args, oAddedCount))

		for _, output := range outputs {
			nKey := fmt.Sprintf(OUTPUT_KEY, output)
			nAddedCount := ac.RedisClient.SAdd(nKey, state).Val()
			ac.logger.Println(fmt.Sprintf("_buildOutput(%s) > SADD key(%s) member(%s) : Count(%d)", state, nKey, state, nAddedCount))
		}
	}

	return
}
