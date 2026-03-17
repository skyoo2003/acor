// Package acor means Aho-Corasick automation working On Redis, Written in Go
package acor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

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
	ErrRedisAlreadyClosed       = errors.New("redis client was already closed")
	ErrRedisConflictingTopology = errors.New("redis topology settings are conflicting")
	ErrRedisSentinelAddrs       = errors.New("redis sentinel requires at least one address")
	ErrRedisClusterDB           = errors.New("redis cluster does not support DB selection")
	ErrRedisRingAddrs           = errors.New("redis ring requires at least one shard address")
)

type Logger interface {
	Printf(format string, v ...interface{})
}

type AhoCorasickArgs struct {
<<	Addr          string // redis server address (ex) localhost:6379
	Addrs         []string
	MasterName    string
	RingAddrs     map[string]string
	Password      string // redis password
	DB            int    // redis db number
	Name          string // pattern's collection name
	Debug         bool   // debug flag
	SchemaVersion int    // 1: V1 (Legacy), 2 or 0: V2 (default)
	Logger        Logger // Optional custom logger
}

type AhoCorasick struct {
	ctx           context.Context
	redisClient   redis.UniversalClient // redis client
	name          string                // Pattern's collection name
	logger        *log.Logger           // logger
	buildTrieHook func(string) error
	schemaVersion int // detected schema version
}

type AhoCorasickInfo struct {
	Keywords int // Aho-Corasick keywords count
	Nodes    int // Aho-Corasick nodes count
}

func Create(args *AhoCorasickArgs) (*AhoCorasick, error) {
	logger := log.New(io.Discard, "ACOR: ", log.LstdFlags|log.Lshortfile)
	if args.Debug {
		logger.SetOutput(os.Stdout)
	}

	redisClient, err := newRedisClient(args)
	if err != nil {
		return nil, err
	}

	ac := &AhoCorasick{
		redisClient:   redisClient,
		ctx:           context.Background(),
		name:          args.Name,
		logger:        logger,
		schemaVersion: args.SchemaVersion,
	}

	if ac.schemaVersion == 0 {
		ac.schemaVersion = SchemaV2
	}

	if err := ac.init(); err != nil {
		_ = ac.redisClient.Close()
		return nil, err
	}
	return ac, nil
}

func newRedisClient(args *AhoCorasickArgs) (redis.UniversalClient, error) {
	addrs := normalizeAddrs(args.Addr, args.Addrs)
	ringAddrs := normalizeRingAddrs(args.RingAddrs)

	if err := validateRedisTopology(args, addrs, ringAddrs); err != nil {
		return nil, err
	}

	switch {
	case len(ringAddrs) > 0:
		return newRingRedisClient(args, ringAddrs), nil
	case strings.TrimSpace(args.MasterName) != "":
		return newSentinelRedisClient(args, addrs), nil
	case len(args.Addrs) > 0:
		return newClusterRedisClient(args, addrs)
	default:
		return newStandaloneRedisClient(args, addrs), nil
	}
}

func validateRedisTopology(args *AhoCorasickArgs, addrs []string, ringAddrs map[string]string) error {
	if strings.TrimSpace(args.Addr) != "" && len(args.Addrs) > 0 {
		return ErrRedisConflictingTopology
	}

	hasSentinel := strings.TrimSpace(args.MasterName) != ""
	hasRing := len(ringAddrs) > 0 || len(args.RingAddrs) > 0
	hasCluster := !hasSentinel && len(args.Addrs) > 0

	selectedTopologies := 0
	if hasSentinel {
		selectedTopologies++
	}
	if hasRing {
		selectedTopologies++
	}
	if hasCluster {
		selectedTopologies++
	}
	if selectedTopologies > 1 {
		return ErrRedisConflictingTopology
	}
	if hasSentinel && len(addrs) == 0 {
		return ErrRedisSentinelAddrs
	}
	if hasRing && len(ringAddrs) == 0 {
		return ErrRedisRingAddrs
	}
	if hasCluster && args.DB != 0 {
		return ErrRedisClusterDB
	}

	return nil
}

func newRingRedisClient(args *AhoCorasickArgs, ringAddrs map[string]string) redis.UniversalClient {
	return redis.NewRing(&redis.RingOptions{
		Addrs:    ringAddrs,
		Password: args.Password,
		DB:       args.DB,
	})
}

func newSentinelRedisClient(args *AhoCorasickArgs, addrs []string) redis.UniversalClient {
	return redis.NewFailoverClient(&redis.FailoverOptions{
		SentinelAddrs: addrs,
		MasterName:    strings.TrimSpace(args.MasterName),
		Password:      args.Password,
		DB:            args.DB,
	})
}

func newClusterRedisClient(args *AhoCorasickArgs, addrs []string) (redis.UniversalClient, error) {
	return redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:    addrs,
		Password: args.Password,
	}), nil
}

func newStandaloneRedisClient(args *AhoCorasickArgs, addrs []string) redis.UniversalClient {
	addr := strings.TrimSpace(args.Addr)
	if addr == "" && len(addrs) > 0 {
		addr = addrs[0]
	}
	return redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: args.Password,
		DB:       args.DB,
	})
}

func normalizeAddrs(addr string, addrs []string) []string {
	normalized := make([]string, 0, len(addrs)+1)
	seen := make(map[string]struct{}, len(addrs)+1)
	appendAddr := func(value string) {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return
		}
		if _, exists := seen[trimmed]; exists {
			return
		}
		seen[trimmed] = struct{}{}
		normalized = append(normalized, trimmed)
	}

	appendAddr(addr)
	for _, candidate := range addrs {
		appendAddr(candidate)
	}

	return normalized
}

func normalizeRingAddrs(addrs map[string]string) map[string]string {
	normalized := make(map[string]string, len(addrs))
	for name, addr := range addrs {
		trimmedName := strings.TrimSpace(name)
		trimmedAddr := strings.TrimSpace(addr)
		if trimmedName == "" || trimmedAddr == "" {
			continue
		}
		normalized[trimmedName] = trimmedAddr
	}
	return normalized
}

func (ac *AhoCorasick) keyPrefix() string {
	return fmt.Sprintf("{%s}", ac.name)
}

func (ac *AhoCorasick) keywordKey() string {
	return fmt.Sprintf("%s:keyword", ac.keyPrefix())
}

func (ac *AhoCorasick) prefixKey() string {
	return fmt.Sprintf("%s:prefix", ac.keyPrefix())
}

func (ac *AhoCorasick) suffixKey() string {
	return fmt.Sprintf("%s:suffix", ac.keyPrefix())
}

func (ac *AhoCorasick) outputKey(state string) string {
	return fmt.Sprintf("%s:output:%s", ac.keyPrefix(), state)
}

func (ac *AhoCorasick) nodeKey(keyword string) string {
	return fmt.Sprintf("%s:node:%s", ac.keyPrefix(), keyword)
}

func (ac *AhoCorasick) trieKey() string {
	return fmt.Sprintf("%s:trie", ac.keyPrefix())
}

func (ac *AhoCorasick) outputsKey() string {
	return fmt.Sprintf("%s:outputs", ac.keyPrefix())
}

func (ac *AhoCorasick) nodesKey() string {
	return fmt.Sprintf("%s:nodes", ac.keyPrefix())
}

func (ac *AhoCorasick) SchemaVersion() int {
	return ac.schemaVersion
}

func (ac *AhoCorasick) init() error {
	if ac.schemaVersion == SchemaV2 {
		exists, err := ac.redisClient.Exists(ac.ctx, ac.trieKey()).Result()
		if err != nil {
			return fmt.Errorf("failed to check trie key: %w", err)
		}
		if exists == 0 {
			_, err := ac.redisClient.HSet(ac.ctx, ac.trieKey(), map[string]interface{}{
				"keywords": "[]",
				"prefixes": "[\"\"]",
				"suffixes": "[\"\"]",
				"version":  time.Now().UnixNano(),
			}).Result()
			if err != nil {
				return err
			}
		}
		return nil
	}

	prefixKey := ac.prefixKey()
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
	if ac.schemaVersion == SchemaV2 {
		return ac.addV2(keyword)
	}

	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	keywordKey := ac.keywordKey()
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
	if ac.schemaVersion == SchemaV2 {
		return ac.removeV2(keyword)
	}

	keyword = strings.TrimSpace(keyword)
	keyword = strings.ToLower(keyword)

	nodeKey := ac.nodeKey(keyword)
	nodes, err := ac.redisClient.SMembers(ac.ctx, nodeKey).Result()
	if err != nil {
		return 0, err
	}
	var removedCount int64
	for _, node := range nodes {
		oKey := ac.outputKey(node)
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

	kKey := ac.keywordKey()
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

		kKey := ac.keywordKey()
		kExists, err := ac.redisClient.SIsMember(ac.ctx, kKey, prefix).Result()
		if err != nil {
			return err
		}
		if kExists && idx != len(keywordRunes) {
			break
		}

		pKey := ac.prefixKey()
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
	pKey := ac.prefixKey()
	pRemovedCount, err := ac.redisClient.ZRem(ac.ctx, pKey, prefix).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, pKey, pRemovedCount))

	sKey := ac.suffixKey()
	sRemovedCount, err := ac.redisClient.ZRem(ac.ctx, sKey, suffix).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Remove(%s) > ZREM key(%s) : Count(%d)", keyword, sKey, sRemovedCount))

	return nil
}

func (ac *AhoCorasick) Find(text string) ([]string, error) {
	if ac.schemaVersion == SchemaV2 {
		return ac.findV2(text)
	}

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
	if ac.schemaVersion == SchemaV2 {
		return ac.findIndexV2(text)
	}

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
	if ac.schemaVersion == SchemaV2 {
		return ac.flushV2()
	}

	kKey := ac.keywordKey()
	pKey := ac.prefixKey()
	sKey := ac.suffixKey()

	keywords, err := ac.redisClient.SMembers(ac.ctx, kKey).Result()
	if err != nil {
		return err
	}
	ac.logger.Println(fmt.Sprintf("Flush() > SMEMBERS Key(%s) : Members(%s)", kKey, keywords))

	for _, keyword := range keywords {
		oKey := ac.outputKey(keyword)
		var oDelCount int64
		oDelCount, err = ac.redisClient.Del(ac.ctx, oKey).Result()
		if err != nil {
			return err
		}
		ac.logger.Println(fmt.Sprintf("Flush() > DEL Key(%s) : Count(%d)", oKey, oDelCount))

		nKey := ac.nodeKey(keyword)
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
	if ac.schemaVersion == SchemaV2 {
		return ac.infoV2()
	}

	kKey := ac.keywordKey()
	kCount, err := ac.redisClient.SCard(ac.ctx, kKey).Result()
	if err != nil {
		return nil, err
	}
	ac.logger.Println(fmt.Sprintf("Info() > SCARD Key(%s) : Count(%d)", kKey, kCount))

	nKey := ac.prefixKey()
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
	if ac.schemaVersion == SchemaV2 {
		return ac.suggestV2(input)
	}

	var pKeywords []string

	results := make([]string, 0)

	kKey := ac.keywordKey()
	pKey := ac.prefixKey()
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
	if ac.schemaVersion == SchemaV2 {
		return ac.suggestIndexV2(input)
	}

	var pKeywords []string

	results := make(map[string][]int)

	kKey := ac.keywordKey()
	pKey := ac.prefixKey()
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
	kKey := ac.keywordKey()
	fmt.Println("-", ac.redisClient.SMembers(ac.ctx, kKey))

	pKey := ac.prefixKey()
	fmt.Println("-", ac.redisClient.ZRange(ac.ctx, pKey, 0, -1))

	sKey := ac.suffixKey()
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
		nKey := ac.nodeKey(keyword)
		nKeywords := ac.redisClient.SMembers(ac.ctx, nKey).Val()
		nodes = append(nodes, nKeywords...)
	}
	fmt.Println("-", nodes)
}

func (ac *AhoCorasick) _go(inState string, input rune) (string, error) {
	buffer := bytes.NewBufferString(inState)
	buffer.WriteRune(input)
	nextState := buffer.String()

	pKey := ac.prefixKey()
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
	pKey := ac.prefixKey()
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
	oKey := ac.outputKey(inState)
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

		pKey := ac.prefixKey()
		err := ac.redisClient.ZScore(ac.ctx, pKey, prefix).Err()
		if err == redis.Nil {
			sKey := ac.suffixKey()
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
			kKey := ac.keywordKey()
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

	sKey := ac.suffixKey()
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

	kKey := ac.keywordKey()
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
		oKey := ac.outputKey(state)
		args := make([]interface{}, len(outputs))
		for i, v := range outputs {
			args[i] = v
		}
		if _, pipeErr := ac.redisClient.TxPipelined(ac.ctx, func(pipe redis.Pipeliner) error {
			pipe.SAdd(ac.ctx, oKey, args...)
			for _, output := range outputs {
				nKey := ac.nodeKey(output)
				pipe.SAdd(ac.ctx, nKey, state)
			}
			return nil
		}); pipeErr != nil {
			return pipeErr
		}
	}

	return nil
}
