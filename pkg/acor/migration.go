// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"errors"
	"fmt"
	"log"
	"time"

	redis "github.com/redis/go-redis/v9"
)

var (
	// ErrAlreadyV2 is returned when attempting to migrate a collection that is
	// already using the V2 schema.
	ErrAlreadyV2 = errors.New("collection is already on V2 schema")
	// ErrNoDataToMigrate is returned when no V1 data is found to migrate.
	// This typically means the collection doesn't exist or was created with V2.
	ErrNoDataToMigrate = errors.New("no V1 data found to migrate")
	// ErrMigrationInProg is returned when a migration is already in progress
	// for the specified collection. Only one migration can run at a time.
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

// requireRedisBacked rejects the migration entry points in preset mode, where
// createPresetRedis leaves redisClient nil — every Redis call below it would
// otherwise dereference a nil interface.
func (ac *AhoCorasick) requireRedisBacked() error {
	if ac.mode != modeOriginal || ac.redisClient == nil {
		return ErrMigrationRequiresRedis
	}
	return nil
}

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
// V2 offers better performance and uses only 3 Redis keys instead of many.
//
// The migration process:
//  1. Acquires a migration lock to prevent concurrent migrations
//  2. Collects the V1 data V2 needs (keywords, prefixes, outputs, nodes)
//  3. Writes V2 structure to temporary keys
//  4. Atomically swaps to V2 keys and optionally deletes V1 keys
//  5. Releases the migration lock
//
// Use DryRun=true in opts to preview the migration without writing anything.
// The lock has a 5-minute TTL to handle client crashes.
//
// On success the instance switches to V2 operations and becomes writable, since
// the read-only rule is V1's. It runs uncached: EnableCache is refused on a V1
// instance (ErrCacheRequiresV2), so there is no cache to carry over and this
// call does not start one. Create a new instance with EnableCache, or a Preset,
// to read the migrated collection locally.
//
// Example:
//
//	result, err := ac.MigrateV1ToV2(&acor.MigrationOptions{
//	    Progress: func(done, total int, msg string) {
//	        fmt.Printf("[%d/%d] %s\n", done, total, msg)
//	    },
//	})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Printf("Migrated %d keywords in %dms\n", result.Keywords, result.DurationMs)
//
// Returns ErrMigrationRequiresRedis when the instance was created with a Preset:
// preset mode holds no Redis client for the V1 key walk.
func (ac *AhoCorasick) MigrateV1ToV2(opts *MigrationOptions) (*MigrationResult, error) { //nolint:gocyclo,funlen // Complex migration logic with multiple stages
	if err := ac.requireRedisBacked(); err != nil {
		return nil, err
	}
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
	defer func() {
		if releaseErr := ac.releaseMigrationLock(); releaseErr != nil {
			if ac.logger != nil {
				ac.logger.Printf("warning: failed to release migration lock: %v", releaseErr)
			}
		}
	}()

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

	tempSuffix := fmt.Sprintf(":tmp:%d", time.Now().UnixNano())
	tempTrieKey := trieKey(ac.name) + tempSuffix
	tempOutputsKey := outputsKey(ac.name) + tempSuffix
	tempNodesKey := nodesKey(ac.name) + tempSuffix

	cleanup := func() {
		if _, delErr := ac.redisClient.Del(ac.ctx, tempTrieKey, tempOutputsKey, tempNodesKey).Result(); delErr != nil {
			if ac.logger != nil {
				ac.logger.Printf("migration cleanup failed: %v", delErr)
			} else {
				log.Printf("acor: migration cleanup failed: %v", delErr)
			}
		}
	}

	trieFields := map[string]interface{}{
		fieldVersion: time.Now().UnixNano(),
	}
	if keywordsJSON, marshalErr := toJSON(keywords); marshalErr != nil {
		return result, fmt.Errorf("migration: failed to marshal keywords: %w", marshalErr)
	} else {
		trieFields[fieldKeywords] = keywordsJSON
	}
	if prefixesJSON, marshalErr := toJSON(prefixes); marshalErr != nil {
		return result, fmt.Errorf("migration: failed to marshal prefixes: %w", marshalErr)
	} else {
		trieFields[fieldPrefixes] = prefixesJSON
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
			jsonOuts, marshalErr := toJSON(outs)
			if marshalErr != nil {
				cleanup()
				result.Status = migrationStatusError
				result.ErrorMessage = marshalErr.Error()
				return result, fmt.Errorf("migration: failed to marshal outputs: %w", marshalErr)
			}
			outputFields[state] = jsonOuts
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
			jsonStates, marshalErr := toJSON(states)
			if marshalErr != nil {
				cleanup()
				result.Status = migrationStatusError
				result.ErrorMessage = marshalErr.Error()
				return result, fmt.Errorf("migration: failed to marshal nodes: %w", marshalErr)
			}
			nodeFields[kw] = jsonStates
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

	// Swap ops to v2Operations so the instance uses V2 schema operations
	// going forward. The cache is already set up if EnableCache was true.
	ac.ops = ac.newV2Ops(ac.cache)

	result.Status = migrationStatusSuccess
	result.DurationMs = time.Since(start).Milliseconds()

	return result, nil
}

// RollbackToV1 reverts the collection from V2 schema back to V1 schema.
// This is only possible if the V1 keys were preserved during migration
// (KeepOldKeys=true in MigrationOptions). Without them it returns an error and
// changes nothing.
//
// It deletes the V2 keys and switches the instance to V1 operations, which costs
// more than the keywords:
//
//   - Any keywords added after the migration to V2 are lost. They live only in
//     the V2 keys this deletes, and the preserved V1 keys predate them.
//   - The collection becomes read-only. V1 takes no writes, so Add and Remove
//     return ErrV1ReadOnly from here on, and the only ways forward are
//     MigrateV1ToV2 again or Flush. Rollback is a way to read the old data with
//     an old client, not a way to keep working on V1.
//   - Local caching stops. The listener is shut down and the cache dropped,
//     because V1 does not support one.
//
// Weigh it against simply not migrating: rolling back is not free and does not
// restore the state the instance had before MigrateV1ToV2 ran.
//
// Returns ErrMigrationRequiresRedis when the instance was created with a Preset,
// for the reason MigrateV1ToV2 documents.
func (ac *AhoCorasick) RollbackToV1() error {
	if err := ac.requireRedisBacked(); err != nil {
		return err
	}
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

	// Swap ops to v1Operations so the instance uses V1 schema operations
	// going forward. Cache is not supported in V1, so stop the listener
	// and clear the cache.
	ac.stopCacheListener()
	ac.cache = nil
	ac.ops = ac.newV1Ops()

	return nil
}
