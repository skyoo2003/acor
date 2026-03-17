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

const (
	migrationStatusError   = "error"
	migrationStatusSuccess = "success"
	migrationStatusDryRun  = "dry-run"

	migrationTotalSteps  = 4
	stepCollectKeywords  = 0
	stepCollectPrefixes  = 1
	stepCollectOutputs   = 2
	stepCollectNodes     = 3
	stepWriteV2Structure = 4
	keysBaseCount        = 2
	v2KeyCount           = 3
)

func (ac *AhoCorasick) MigrateV1ToV2(opts *MigrationOptions) (*MigrationResult, error) { //nolint:gocyclo,funlen // Complex migration logic with multiple stages
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
		opts.Progress(stepCollectKeywords, migrationTotalSteps, "Collecting keywords")
	}

	keywords, err := ac.redisClient.SMembers(ac.ctx, ac.keywordKey()).Result()
	if err != nil {
		result.Status = migrationStatusError
		result.ErrorMessage = err.Error()
		return result, err
	}
	result.Keywords = len(keywords)

	if opts.Progress != nil {
		opts.Progress(stepCollectPrefixes, migrationTotalSteps, "Collecting prefixes")
	}

	prefixes, err := ac.redisClient.ZRange(ac.ctx, ac.prefixKey(), 0, -1).Result()
	if err != nil {
		result.Status = migrationStatusError
		result.ErrorMessage = err.Error()
		return result, err
	}
	result.Prefixes = len(prefixes)

	suffixes, err := ac.redisClient.ZRange(ac.ctx, ac.suffixKey(), 0, -1).Result()
	if err != nil {
		result.Status = migrationStatusError
		result.ErrorMessage = err.Error()
		return result, err
	}

	if opts.Progress != nil {
		opts.Progress(stepCollectOutputs, migrationTotalSteps, "Collecting outputs")
	}

	outputs := make(map[string][]string)
	outputCount := 0
	for _, prefix := range prefixes {
		outs, outErr := ac.redisClient.SMembers(ac.ctx, ac.outputKey(prefix)).Result()
		if outErr != nil && outErr != redis.Nil {
			result.Status = migrationStatusError
			result.ErrorMessage = outErr.Error()
			return result, outErr
		}
		if len(outs) > 0 {
			outputs[prefix] = outs
			outputCount++
		}
	}
	result.OutputsKeys = outputCount

	if opts.Progress != nil {
		opts.Progress(stepCollectNodes, migrationTotalSteps, "Collecting nodes")
	}

	nodes := make(map[string][]string)
	nodeCount := 0
	for _, kw := range keywords {
		n, nodeErr := ac.redisClient.SMembers(ac.ctx, ac.nodeKey(kw)).Result()
		if nodeErr != nil && nodeErr != redis.Nil {
			result.Status = migrationStatusError
			result.ErrorMessage = nodeErr.Error()
			return result, nodeErr
		}
		if len(n) > 0 {
			nodes[kw] = n
			nodeCount++
		}
	}
	result.NodesKeys = nodeCount

	result.KeysBefore = keysBaseCount + result.Prefixes + result.Keywords
	result.KeysAfter = v2KeyCount

	if opts.DryRun {
		result.Status = migrationStatusDryRun
		result.DurationMs = time.Since(start).Milliseconds()
		return result, nil
	}

	if opts.Progress != nil {
		opts.Progress(stepWriteV2Structure, migrationTotalSteps, "Writing V2 structure")
	}

	tempSuffix := fmt.Sprintf(":tmp:%d", time.Now().Unix())
	tempTrieKey := ac.trieKey() + tempSuffix
	tempOutputsKey := ac.outputsKey() + tempSuffix
	tempNodesKey := ac.nodesKey() + tempSuffix

	cleanup := func() {
		ac.redisClient.Del(ac.ctx, tempTrieKey, tempOutputsKey, tempNodesKey)
	}

	trieFields := map[string]interface{}{
		"keywords": mustJSON(keywords),
		"prefixes": mustJSON(prefixes),
		"suffixes": mustJSON(suffixes),
		"version":  time.Now().Unix(),
	}
	if hsetErr := ac.redisClient.HSet(ac.ctx, tempTrieKey, trieFields).Err(); hsetErr != nil {
		cleanup()
		result.Status = migrationStatusError
		result.ErrorMessage = hsetErr.Error()
		return result, hsetErr
	}

	if len(outputs) > 0 {
		outputFields := make(map[string]interface{})
		for state, outs := range outputs {
			outputFields[state] = mustJSON(outs)
		}
		if outputsErr := ac.redisClient.HSet(ac.ctx, tempOutputsKey, outputFields).Err(); outputsErr != nil {
			cleanup()
			result.Status = migrationStatusError
			result.ErrorMessage = outputsErr.Error()
			return result, outputsErr
		}
	}

	if len(nodes) > 0 {
		nodeFields := make(map[string]interface{})
		for kw, states := range nodes {
			nodeFields[kw] = mustJSON(states)
		}
		if nodesErr := ac.redisClient.HSet(ac.ctx, tempNodesKey, nodeFields).Err(); nodesErr != nil {
			cleanup()
			result.Status = migrationStatusError
			result.ErrorMessage = nodesErr.Error()
			return result, nodesErr
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

		pipe.Rename(ac.ctx, tempTrieKey, ac.trieKey())
		if len(outputs) > 0 {
			pipe.Rename(ac.ctx, tempOutputsKey, ac.outputsKey())
		}
		if len(nodes) > 0 {
			pipe.Rename(ac.ctx, tempNodesKey, ac.nodesKey())
		}

		return nil
	})

	if err != nil {
		cleanup()
		result.Status = migrationStatusError
		result.ErrorMessage = err.Error()
		return result, err
	}

	ac.schemaVersion = SchemaV2

	result.Status = migrationStatusSuccess
	result.DurationMs = time.Since(start).Milliseconds()

	return result, nil
}

func (ac *AhoCorasick) RollbackToV1() error {
	if ac.redisClient.Exists(ac.ctx, ac.keywordKey()).Val() == 0 {
		return errors.New("V1 keys not found - rollback not possible")
	}

	if _, err := ac.redisClient.Del(ac.ctx, ac.trieKey(), ac.outputsKey(), ac.nodesKey()).Result(); err != nil {
		return fmt.Errorf("failed to delete V2 keys: %w", err)
	}

	ac.schemaVersion = SchemaV1

	return nil
}

func parseJSON(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}
