package acor

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redis "github.com/go-redis/redis/v8"
)

var (
	ErrAlreadyV2       = errors.New("collection is already on V2 schema")
	ErrNoDataToMigrate = errors.New("no V1 data found to migrate")
)

func (ac *AhoCorasick) MigrateV1ToV2(opts *MigrationOptions) (*MigrationResult, error) {
	if opts == nil {
		opts = DefaultMigrationOptions()
	}

	start := time.Now()
	result := &MigrationResult{
		Collection: ac.name,
		FromSchema: SchemaV1,
		ToSchema:   SchemaV2,
		DryRun:     opts.DryRun,
	}

	if ac.redisClient.Exists(ac.ctx, ac.trieKey()).Val() > 0 {
		return nil, ErrAlreadyV2
	}

	if ac.redisClient.Exists(ac.ctx, ac.prefixKey()).Val() == 0 {
		return nil, ErrNoDataToMigrate
	}

	if opts.Progress != nil {
		opts.Progress(0, 4, "Collecting keywords")
	}

	keywords, err := ac.redisClient.SMembers(ac.ctx, ac.keywordKey()).Result()
	if err != nil {
		result.Status = "error"
		result.ErrorMessage = err.Error()
		return result, err
	}
	result.Keywords = len(keywords)

	if opts.Progress != nil {
		opts.Progress(1, 4, "Collecting prefixes")
	}

	prefixes, err := ac.redisClient.ZRange(ac.ctx, ac.prefixKey(), 0, -1).Result()
	if err != nil {
		result.Status = "error"
		result.ErrorMessage = err.Error()
		return result, err
	}
	result.Prefixes = len(prefixes)

	suffixes, err := ac.redisClient.ZRange(ac.ctx, ac.suffixKey(), 0, -1).Result()
	if err != nil {
		result.Status = "error"
		result.ErrorMessage = err.Error()
		return result, err
	}

	if opts.Progress != nil {
		opts.Progress(2, 4, "Collecting outputs")
	}

	outputs := make(map[string][]string)
	outputCount := 0
	for _, prefix := range prefixes {
		outs, err := ac.redisClient.SMembers(ac.ctx, ac.outputKey(prefix)).Result()
		if err != nil && err != redis.Nil {
			result.Status = "error"
			result.ErrorMessage = err.Error()
			return result, err
		}
		if len(outs) > 0 {
			outputs[prefix] = outs
			outputCount++
		}
	}
	result.OutputsKeys = outputCount

	if opts.Progress != nil {
		opts.Progress(3, 4, "Collecting nodes")
	}

	nodes := make(map[string][]string)
	nodeCount := 0
	for _, kw := range keywords {
		n, err := ac.redisClient.SMembers(ac.ctx, ac.nodeKey(kw)).Result()
		if err != nil && err != redis.Nil {
			result.Status = "error"
			result.ErrorMessage = err.Error()
			return result, err
		}
		if len(n) > 0 {
			nodes[kw] = n
			nodeCount++
		}
	}
	result.NodesKeys = nodeCount

	result.KeysBefore = 2 + result.Prefixes + result.Keywords
	result.KeysAfter = 3

	if opts.DryRun {
		result.Status = "dry-run"
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	if opts.Progress != nil {
		opts.Progress(4, 4, "Writing V2 structure")
	}

	tempSuffix := fmt.Sprintf(":tmp:%d", time.Now().Unix())

	trieFields := map[string]interface{}{
		"keywords": mustJSON(keywords),
		"prefixes": mustJSON(prefixes),
		"suffixes": mustJSON(suffixes),
		"version":  time.Now().Unix(),
	}
	if err := ac.redisClient.HSet(ac.ctx, ac.trieKey()+tempSuffix, trieFields).Err(); err != nil {
		result.Status = "error"
		result.ErrorMessage = err.Error()
		return result, err
	}

	if len(outputs) > 0 {
		outputFields := make(map[string]interface{})
		for state, outs := range outputs {
			outputFields[state] = mustJSON(outs)
		}
		if err := ac.redisClient.HSet(ac.ctx, ac.outputsKey()+tempSuffix, outputFields).Err(); err != nil {
			result.Status = "error"
			result.ErrorMessage = err.Error()
			return result, err
		}
	}

	if len(nodes) > 0 {
		nodeFields := make(map[string]interface{})
		for kw, states := range nodes {
			nodeFields[kw] = mustJSON(states)
		}
		if err := ac.redisClient.HSet(ac.ctx, ac.nodesKey()+tempSuffix, nodeFields).Err(); err != nil {
			result.Status = "error"
			result.ErrorMessage = err.Error()
			return result, err
		}
	}

	_, err = ac.redisClient.TxPipelined(ac.ctx, func(pipe redis.Pipeliner) error {
		if !opts.KeepOldKeys {
			pipe.Del(ac.ctx, ac.keywordKey(), ac.prefixKey(), ac.suffixKey())
			for _, p := range prefixes {
				pipe.Del(ac.ctx, ac.outputKey(p))
			}
			for _, kw := range keywords {
				pipe.Del(ac.ctx, ac.nodeKey(kw))
			}
		}

		pipe.Rename(ac.ctx, ac.trieKey()+tempSuffix, ac.trieKey())
		if len(outputs) > 0 {
			pipe.Rename(ac.ctx, ac.outputsKey()+tempSuffix, ac.outputsKey())
		}
		if len(nodes) > 0 {
			pipe.Rename(ac.ctx, ac.nodesKey()+tempSuffix, ac.nodesKey())
		}

		return nil
	})

	if err != nil {
		result.Status = "error"
		result.ErrorMessage = err.Error()
		return result, err
	}

	ac.schemaVersion = SchemaV2

	result.Status = "success"
	result.DurationMs = time.Since(start).Milliseconds()

	return result, nil
}

func (ac *AhoCorasick) RollbackToV1() error {
	if ac.redisClient.Exists(ac.ctx, ac.keywordKey()).Val() == 0 {
		return errors.New("V1 keys not found - rollback not possible")
	}

	ac.redisClient.Del(ac.ctx, ac.trieKey(), ac.outputsKey(), ac.nodesKey())

	ac.schemaVersion = SchemaV1

	return nil
}

func parseJSON(data string, v interface{}) {
	json.Unmarshal([]byte(data), v)
}
