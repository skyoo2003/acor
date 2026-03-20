package acor

import (
	"context"
	"encoding/json"
	"sync"
)

type trieCache struct {
	mu       sync.RWMutex
	prefixes []string
	outputs  map[string][]string
	valid    bool
}

func (c *trieCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = false
}

func (c *trieCache) set(prefixes []string, outputs map[string][]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefixes = prefixes
	c.outputs = outputs
	c.valid = true
}

func (c *trieCache) get() (prefixes []string, outputs map[string][]string, valid bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.prefixes, c.outputs, c.valid
}

func (ac *AhoCorasick) loadCache(ctx context.Context) error {
	pipe := ac.redisClient.Pipeline()
	trieCmd := pipe.HGetAll(ctx, trieKey(ac.name))
	outputsCmd := pipe.HGetAll(ctx, outputsKey(ac.name))
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}

	trieData := trieCmd.Val()
	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		if err := json.Unmarshal([]byte(data), &prefixes); err != nil {
			return err
		}
	}

	outputsRaw := outputsCmd.Val()
	outputs := make(map[string][]string)
	for state, jsonArr := range outputsRaw {
		var arr []string
		if err := json.Unmarshal([]byte(jsonArr), &arr); err != nil {
			return err
		}
		outputs[state] = arr
	}

	ac.cache.set(prefixes, outputs)
	return nil
}

func (ac *AhoCorasick) getOrLoadCache() ([]string, map[string][]string, error) {
	if ac.cache == nil {
		return ac.loadCacheFromRedis()
	}

	if prefixes, outputs, valid := ac.cache.get(); valid {
		return prefixes, outputs, nil
	}

	if err := ac.loadCache(ac.ctx); err != nil {
		return nil, nil, err
	}

	prefixes, outputs, _ := ac.cache.get()
	return prefixes, outputs, nil
}

func (ac *AhoCorasick) loadCacheFromRedis() ([]string, map[string][]string, error) {
	pipe := ac.redisClient.Pipeline()
	trieCmd := pipe.HGetAll(ac.ctx, trieKey(ac.name))
	outputsCmd := pipe.HGetAll(ac.ctx, outputsKey(ac.name))
	_, err := pipe.Exec(ac.ctx)
	if err != nil {
		return nil, nil, err
	}

	trieData := trieCmd.Val()
	var prefixes []string
	if data, ok := trieData["prefixes"]; ok {
		if err := json.Unmarshal([]byte(data), &prefixes); err != nil {
			return nil, nil, err
		}
	}

	outputsRaw := outputsCmd.Val()
	outputs := make(map[string][]string)
	for state, jsonArr := range outputsRaw {
		var arr []string
		if err := json.Unmarshal([]byte(jsonArr), &arr); err != nil {
			return nil, nil, err
		}
		outputs[state] = arr
	}

	return prefixes, outputs, nil
}
