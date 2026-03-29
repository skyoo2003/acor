package acor

import (
	"context"
	"encoding/json"
	"sync"
)

type trieCache struct {
	mu       sync.RWMutex
	loadMu   sync.Mutex
	prefixes []string
	outputs  map[string][]string
	valid    bool
}

func cloneOutputs(in map[string][]string) map[string][]string {
	if in == nil {
		return nil
	}
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}

func (c *trieCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.valid = false
}

func (c *trieCache) set(prefixes []string, outputs map[string][]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prefixes = append([]string(nil), prefixes...)
	c.outputs = cloneOutputs(outputs)
	c.valid = true
}

func (c *trieCache) get() (prefixes []string, outputs map[string][]string, valid bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return append([]string(nil), c.prefixes...), cloneOutputs(c.outputs), c.valid
}

func (ac *AhoCorasick) fetchTrieDataFromRedis(ctx context.Context) (prefixes []string, outputs map[string][]string, err error) {
	pipe := ac.redisClient.Pipeline()
	trieCmd := pipe.HGetAll(ctx, trieKey(ac.name))
	outputsCmd := pipe.HGetAll(ctx, outputsKey(ac.name))
	if _, execErr := pipe.Exec(ctx); execErr != nil {
		return nil, nil, execErr
	}

	trieData := trieCmd.Val()
	if data, ok := trieData["prefixes"]; ok {
		if unmarshalErr := json.Unmarshal([]byte(data), &prefixes); unmarshalErr != nil {
			return nil, nil, unmarshalErr
		}
	}

	outputsRaw := outputsCmd.Val()
	outputs = make(map[string][]string)
	for state, jsonArr := range outputsRaw {
		var arr []string
		if unmarshalErr := json.Unmarshal([]byte(jsonArr), &arr); unmarshalErr != nil {
			return nil, nil, unmarshalErr
		}
		outputs[state] = arr
	}

	return prefixes, outputs, nil
}

func (ac *AhoCorasick) loadCache(ctx context.Context) error {
	prefixes, outputs, err := ac.fetchTrieDataFromRedis(ctx)
	if err != nil {
		return err
	}
	ac.cache.set(prefixes, outputs)
	return nil
}

func (ac *AhoCorasick) getOrLoadCache() (prefixes []string, outputs map[string][]string, err error) {
	if ac.cache == nil {
		return ac.loadCacheFromRedis()
	}

	var valid bool
	prefixes, outputs, valid = ac.cache.get()
	if valid {
		return
	}

	ac.cache.loadMu.Lock()
	defer ac.cache.loadMu.Unlock()

	// Double-check after acquiring lock
	prefixes, outputs, valid = ac.cache.get()
	if valid {
		return
	}

	if err = ac.loadCache(ac.ctx); err != nil {
		return
	}

	prefixes, outputs, _ = ac.cache.get()
	return
}

func (ac *AhoCorasick) loadCacheFromRedis() (prefixes []string, outputs map[string][]string, err error) {
	return ac.fetchTrieDataFromRedis(ac.ctx)
}
