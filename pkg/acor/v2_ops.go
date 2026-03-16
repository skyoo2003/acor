package acor

import (
	"encoding/json"
	"strings"
	"time"

	redis "github.com/go-redis/redis/v8"
	"github.com/skyoo2003/acor/internal/pkg/utils"
)

func mustJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func (ac *AhoCorasick) findV2(text string) ([]string, error) {
	if len(text) == 0 {
		return []string{}, nil
	}

	pipe := ac.redisClient.Pipeline()
	trieCmd := pipe.HGetAll(ac.ctx, ac.trieKey())
	outputsCmd := pipe.HGetAll(ac.ctx, ac.outputsKey())
	_, err := pipe.Exec(ac.ctx)
	if err != nil {
		return nil, err
	}

	trieData := trieCmd.Val()
	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		if err := json.Unmarshal([]byte(data), &prefixes); err != nil {
			return nil, err
		}
	}

	outputsRaw := outputsCmd.Val()
	outputs := make(map[string][]string)
	for state, jsonArr := range outputsRaw {
		var arr []string
		if err := json.Unmarshal([]byte(jsonArr), &arr); err != nil {
			return nil, err
		}
		outputs[state] = arr
	}

	return ac.localFind(text, prefixes, outputs), nil
}

func (ac *AhoCorasick) localFind(text string, prefixes []string, outputs map[string][]string) []string {
	prefixSet := make(map[string]struct{})
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}

	matched := make([]string, 0)
	state := ""

	for _, char := range text {
		nextState := string(append([]rune(state), char))

		if _, exists := prefixSet[nextState]; !exists {
			runes := []rune(nextState)
			found := false
			for i := 1; i < len(runes); i++ {
				suffix := string(runes[i:])
				if _, exists := prefixSet[suffix]; exists {
					nextState = suffix
					found = true
					break
				}
			}
			if !found {
				nextState = ""
			}
		}

		state = nextState
		if outs, exists := outputs[state]; exists {
			matched = append(matched, outs...)
		}
	}

	return matched
}

func (ac *AhoCorasick) findIndexV2(text string) (map[string][]int, error) {
	if len(text) == 0 {
		return map[string][]int{}, nil
	}

	pipe := ac.redisClient.Pipeline()
	trieCmd := pipe.HGetAll(ac.ctx, ac.trieKey())
	outputsCmd := pipe.HGetAll(ac.ctx, ac.outputsKey())
	_, err := pipe.Exec(ac.ctx)
	if err != nil {
		return nil, err
	}

	trieData := trieCmd.Val()
	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		json.Unmarshal([]byte(data), &prefixes)
	}

	outputsRaw := outputsCmd.Val()
	outputs := make(map[string][]string)
	for state, jsonArr := range outputsRaw {
		var arr []string
		json.Unmarshal([]byte(jsonArr), &arr)
		outputs[state] = arr
	}

	return ac.localFindIndex(text, prefixes, outputs), nil
}

func (ac *AhoCorasick) localFindIndex(text string, prefixes []string, outputs map[string][]string) map[string][]int {
	prefixSet := make(map[string]struct{})
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}

	matched := make(map[string][]int)
	state := ""
	runeIndex := 0

	for _, char := range text {
		nextState := string(append([]rune(state), char))

		if _, exists := prefixSet[nextState]; !exists {
			runes := []rune(nextState)
			found := false
			for i := 1; i < len(runes); i++ {
				suffix := string(runes[i:])
				if _, exists := prefixSet[suffix]; exists {
					nextState = suffix
					found = true
					break
				}
			}
			if !found {
				nextState = ""
			}
		}

		state = nextState
		if outs, exists := outputs[state]; exists {
			for _, out := range outs {
				startIdx := runeIndex - len([]rune(out)) + 1
				matched[out] = append(matched[out], startIdx)
			}
		}
		runeIndex++
	}

	return matched
}

func (ac *AhoCorasick) infoV2() (*AhoCorasickInfo, error) {
	trieData := ac.redisClient.HGetAll(ac.ctx, ac.trieKey()).Val()

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		json.Unmarshal([]byte(data), &keywords)
	}

	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		json.Unmarshal([]byte(data), &prefixes)
	}

	return &AhoCorasickInfo{
		Keywords: len(keywords),
		Nodes:    len(prefixes),
	}, nil
}

func (ac *AhoCorasick) suggestV2(input string) ([]string, error) {
	input = strings.ToLower(strings.TrimSpace(input))
	if input == "" {
		return []string{}, nil
	}

	trieData := ac.redisClient.HGetAll(ac.ctx, ac.trieKey()).Val()

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		json.Unmarshal([]byte(data), &keywords)
	}

	results := []string{}
	for _, kw := range keywords {
		if strings.HasPrefix(kw, input) {
			results = append(results, kw)
		}
	}

	return results, nil
}

func (ac *AhoCorasick) suggestIndexV2(input string) (map[string][]int, error) {
	results, err := ac.suggestV2(input)
	if err != nil {
		return nil, err
	}

	indexed := make(map[string][]int)
	for _, kw := range results {
		indexed[kw] = []int{0}
	}
	return indexed, nil
}

func (ac *AhoCorasick) addV2(keyword string) (int, error) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return 0, nil
	}

	pipe := ac.redisClient.Pipeline()
	trieCmd := pipe.HGetAll(ac.ctx, ac.trieKey())
	outputsCmd := pipe.HGetAll(ac.ctx, ac.outputsKey())
	_, err := pipe.Exec(ac.ctx)
	if err != nil {
		return 0, err
	}

	trieData := trieCmd.Val()

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		json.Unmarshal([]byte(data), &keywords)
	}
	keywordSet := make(map[string]struct{})
	for _, kw := range keywords {
		keywordSet[kw] = struct{}{}
	}
	if _, exists := keywordSet[keyword]; exists {
		return 0, nil
	}

	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		json.Unmarshal([]byte(data), &prefixes)
	}
	prefixSet := make(map[string]struct{})
	for _, p := range prefixes {
		prefixSet[p] = struct{}{}
	}

	outputsRaw := outputsCmd.Val()
	outputs := make(map[string][]string)
	for state, jsonArr := range outputsRaw {
		var arr []string
		json.Unmarshal([]byte(jsonArr), &arr)
		outputs[state] = arr
	}

	keywordRunes := []rune(keyword)
	newPrefixes := []string{}
	newSuffixes := []string{}

	for i := 0; i < len(keywordRunes); i++ {
		prefix := string(keywordRunes[:i+1])
		if _, exists := prefixSet[prefix]; !exists {
			newPrefixes = append(newPrefixes, prefix)
			newSuffixes = append(newSuffixes, utils.Reverse(prefix))
			prefixSet[prefix] = struct{}{}
		}
	}

	newOutputs := make(map[string][]string)
	keywordSet[keyword] = struct{}{}
	for _, prefix := range newPrefixes {
		prefixOutputs := ac.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(prefixOutputs) > 0 {
			newOutputs[prefix] = prefixOutputs
		}
	}

	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		updatedOutputs := ac.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(updatedOutputs) > 0 {
			newOutputs[prefix] = updatedOutputs
		}
	}

	keywords = append(keywords, keyword)
	prefixes = append(prefixes, newPrefixes...)

	var suffixes []string
	if data, ok := trieData["suffixes"]; ok {
		json.Unmarshal([]byte(data), &suffixes)
	}
	suffixes = append(suffixes, newSuffixes...)

	_, err = ac.redisClient.TxPipelined(ac.ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ac.ctx, ac.trieKey(), map[string]interface{}{
			"keywords": mustJSON(keywords),
			"prefixes": mustJSON(prefixes),
			"suffixes": mustJSON(suffixes),
			"version":  time.Now().Unix(),
		})

		for state, outs := range newOutputs {
			pipe.HSet(ac.ctx, ac.outputsKey(), state, mustJSON(outs))
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	return 1, nil
}

func (ac *AhoCorasick) computeOutputsV2(state string, prefixSet, keywordSet map[string]struct{}) []string {
	outputs := []string{}

	if _, isKeyword := keywordSet[state]; isKeyword {
		outputs = append(outputs, state)
	}

	stateRunes := []rune(state)
	for i := 1; i < len(stateRunes); i++ {
		failState := string(stateRunes[i:])
		if _, isPrefix := prefixSet[failState]; isPrefix {
			if _, isKeyword := keywordSet[failState]; isKeyword {
				outputs = append(outputs, failState)
			}
		}
	}

	return outputs
}

func (ac *AhoCorasick) removeV2(keyword string) (int, error) {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	if keyword == "" {
		return 0, nil
	}

	pipe := ac.redisClient.Pipeline()
	trieCmd := pipe.HGetAll(ac.ctx, ac.trieKey())
	_, err := pipe.Exec(ac.ctx)
	if err != nil {
		return 0, err
	}

	trieData := trieCmd.Val()

	var keywords []string
	if data, ok := trieData["keywords"]; ok {
		json.Unmarshal([]byte(data), &keywords)
	}

	keywordExists := false
	newKeywords := make([]string, 0, len(keywords)-1)
	for _, kw := range keywords {
		if kw == keyword {
			keywordExists = true
		} else {
			newKeywords = append(newKeywords, kw)
		}
	}

	if !keywordExists {
		return len(keywords), nil
	}

	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		json.Unmarshal([]byte(data), &prefixes)
	}

	keywordSet := make(map[string]struct{})
	for _, kw := range newKeywords {
		keywordSet[kw] = struct{}{}
	}

	newPrefixes := []string{""}
	for _, prefix := range prefixes {
		if prefix == "" {
			continue
		}
		keep := false
		for kw := range keywordSet {
			if strings.HasPrefix(kw, prefix) {
				keep = true
				break
			}
		}
		if keep {
			newPrefixes = append(newPrefixes, prefix)
		}
	}

	prefixSet := make(map[string]struct{})
	for _, p := range newPrefixes {
		prefixSet[p] = struct{}{}
	}

	newSuffixes := make([]string, len(newPrefixes))
	for i, p := range newPrefixes {
		newSuffixes[i] = utils.Reverse(p)
	}

	newOutputs := make(map[string][]string)
	for _, prefix := range newPrefixes {
		if prefix == "" {
			continue
		}
		outs := ac.computeOutputsV2(prefix, prefixSet, keywordSet)
		if len(outs) > 0 {
			newOutputs[prefix] = outs
		}
	}

	_, err = ac.redisClient.TxPipelined(ac.ctx, func(pipe redis.Pipeliner) error {
		pipe.HSet(ac.ctx, ac.trieKey(), map[string]interface{}{
			"keywords": mustJSON(newKeywords),
			"prefixes": mustJSON(newPrefixes),
			"suffixes": mustJSON(newSuffixes),
			"version":  time.Now().Unix(),
		})

		pipe.Del(ac.ctx, ac.outputsKey())
		for state, outs := range newOutputs {
			pipe.HSet(ac.ctx, ac.outputsKey(), state, mustJSON(outs))
		}

		return nil
	})

	if err != nil {
		return 0, err
	}

	return len(newKeywords), nil
}

func (ac *AhoCorasick) flushV2() error {
	_, err := ac.redisClient.Del(ac.ctx, ac.trieKey(), ac.outputsKey(), ac.nodesKey()).Result()
	return err
}
