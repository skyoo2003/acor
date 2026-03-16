package acor

import (
	"encoding/json"
)

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
