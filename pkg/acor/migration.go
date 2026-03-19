package acor

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	redis "github.com/go-redis/redis/v8"
)

var (
	// ErrAlreadyV2 is returned when attempting to migrate a collection that is already on V2 schema.
	ErrAlreadyV2 = errors.New("collection is already on V2 schema")
	// ErrNoDataToMigrate is returned when no V1 data is found to migrate.
	ErrNoDataToMigrate = errors.New("no V1 data found to migrate")
	// ErrMigrationInProg is returned when a migration is already in progress.
	ErrMigrationInProg = errors.New("migration already in progress")
)

const (
	migrationStatusError   = "error"
	migrationStatusSuccess = "success"
	migrationStatusDryRun  = "dry-run"

	migrationTotalSteps  = 5
	stepCollectKeywords  = 1
	stepCollectPrefixes  = 2
	stepCollectOutputs   = 3
	stepCollectNodes     = 4
	stepWriteV2Structure = 5
	keysBaseCount        = 2
	v2KeyCount           = 3

	migrationLockKeySuffix = ":migration_lock"
	migrationLockTTL       = 300 * time.Second
)

func (ac *AhoCorasick) migrationLockKey() string {
	return keyPrefix(ac.name) + migrationLockKeySuffix
}

func (ac *AhoCorasick) acquireMigrationLock() (bool, error) {
	lockKey := ac.migrationLockKey()
	result, err := ac.redisClient.SetNX(ac.ctx, lockKey, "migrating", migrationLockTTL).Result()
	if err != nil {
		return false, fmt.Errorf("failed to acquire migration lock: %w", err)
	}
	return result, nil
}

func (ac *AhoCorasick) releaseMigrationLock() error {
	lockKey := ac.migrationLockKey()
	_, err := ac.redisClient.Del(ac.ctx, lockKey).Result()
	if err != nil {
		return fmt.Errorf("failed to release migration lock: %w", err)
	}
	return nil
}

// MigrateV1ToV2 migrates the collection from V1 schema to V2 schema.
func (ac *AhoCorasick) MigrateV1ToV2(opts *MigrationOptions) (*MigrationResult, error) { //nolint:gocyclo,funlen // Complex migration logic with multiple stages
	if opts == nil {
		opts = DefaultMigrationOptions()
	}

	acquired, err := ac.acquireMigrationLock()
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, ErrMigrationInProg
	}
	defer func() { _ = ac.releaseMigrationLock() }()

	start := time.Now()
	result := &MigrationResult{
		Collection: ac.name,
		FromSchema: SchemaV1,
		ToSchema:   SchemaV2,
		DryRun:     opts.DryRun,
	}

	trieExists, err := ac.redisClient.Exists(ac.ctx, trieKey(ac.name)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to check V2 keys: %w", err)
	}
	if trieExists > 0 {
		return nil, ErrAlreadyV2
	}

	prefixExists, err := ac.redisClient.Exists(ac.ctx, prefixKey(ac.name)).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to check V1 keys: %w", err)
	}
	if prefixExists == 0 {
		return nil, ErrNoDataToMigrate
	}

	if opts.Progress != nil {
		opts.Progress(stepCollectKeywords, migrationTotalSteps, "Collecting keywords")
	}

	keywords, err := ac.redisClient.SMembers(ac.ctx, keywordKey(ac.name)).Result()
	if err != nil {
		result.Status = migrationStatusError
		result.ErrorMessage = err.Error()
		return result, err
	}
	result.Keywords = len(keywords)

	if opts.Progress != nil {
		opts.Progress(stepCollectPrefixes, migrationTotalSteps, "Collecting prefixes")
	}

	prefixes, err := ac.redisClient.ZRange(ac.ctx, prefixKey(ac.name), 0, -1).Result()
	if err != nil {
		result.Status = migrationStatusError
		result.ErrorMessage = err.Error()
		return result, err
	}
	result.Prefixes = len(prefixes)

	suffixes, err := ac.redisClient.ZRange(ac.ctx, suffixKey(ac.name), 0, -1).Result()
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
		outs, outErr := ac.redisClient.SMembers(ac.ctx, outputKey(ac.name, prefix)).Result()
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
		n, nodeErr := ac.redisClient.SMembers(ac.ctx, nodeKey(ac.name, kw)).Result()
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
	tempTrieKey := trieKey(ac.name) + tempSuffix
	tempOutputsKey := outputsKey(ac.name) + tempSuffix
	tempNodesKey := nodesKey(ac.name) + tempSuffix

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
			pipe.Del(ac.ctx, keywordKey(ac.name), prefixKey(ac.name), suffixKey(ac.name))
			for _, p := range prefixes {
				pipe.Del(ac.ctx, outputKey(ac.name, p))
			}
			for _, kw := range keywords {
				pipe.Del(ac.ctx, nodeKey(ac.name, kw))
			}
		}

		pipe.Rename(ac.ctx, tempTrieKey, trieKey(ac.name))
		if len(outputs) > 0 {
			pipe.Rename(ac.ctx, tempOutputsKey, outputsKey(ac.name))
		}
		if len(nodes) > 0 {
			pipe.Rename(ac.ctx, tempNodesKey, nodesKey(ac.name))
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

// RollbackToV1 reverts the collection from V2 schema back to V1 schema.
func (ac *AhoCorasick) RollbackToV1() error {
	v1Exists, err := ac.redisClient.Exists(ac.ctx, keywordKey(ac.name)).Result()
	if err != nil {
		return fmt.Errorf("failed to check V1 keys: %w", err)
	}
	if v1Exists == 0 {
		return errors.New("V1 keys not found - rollback not possible")
	}

	if _, err := ac.redisClient.Del(ac.ctx, trieKey(ac.name), outputsKey(ac.name), nodesKey(ac.name)).Result(); err != nil {
		return fmt.Errorf("failed to delete V2 keys: %w", err)
	}

	ac.schemaVersion = SchemaV1

	return nil
}

func parseJSON(data string, v interface{}) error {
	return json.Unmarshal([]byte(data), v)
}
