// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"

	"github.com/go-redis/redis/v8"
)

// --- Lua scripts (precompiled with redis.NewScript for EVALSHA optimization) ---

var addV2Script = redis.NewScript(`
	local trieKey = KEYS[1]
	local outputsKey = KEYS[2]
	local oldVersion = ARGV[1]
	local newVersion = ARGV[2]
	local keywords = ARGV[3]
	local prefixes = ARGV[4]
	local suffixes = ARGV[5]
	local outputsJson = ARGV[6]

	local currentVersion = redis.call('HGET', trieKey, 'version')
	if currentVersion and currentVersion ~= oldVersion then
		return 0
	end

	redis.call('HSET', trieKey, 'keywords', keywords, 'prefixes', prefixes, 'suffixes', suffixes, 'version', newVersion)

	local outputs = cjson.decode(outputsJson)
	for state, jsonOuts in pairs(outputs) do
		redis.call('HSET', outputsKey, state, jsonOuts)
	end

	return 1
`)

var removeV2Script = redis.NewScript(`
	local trieKey = KEYS[1]
	local outputsKey = KEYS[2]
	local oldVersion = ARGV[1]
	local newVersion = ARGV[2]
	local keywords = ARGV[3]
	local prefixes = ARGV[4]
	local suffixes = ARGV[5]
	local outputsJson = ARGV[6]

	local currentVersion = redis.call('HGET', trieKey, 'version')
	if currentVersion and currentVersion ~= oldVersion then
		return 0
	end

	redis.call('HSET', trieKey, 'keywords', keywords, 'prefixes', prefixes, 'suffixes', suffixes, 'version', newVersion)

	local outputs = cjson.decode(outputsJson)
	redis.call('DEL', outputsKey)
	for state, jsonOuts in pairs(outputs) do
		redis.call('HSET', outputsKey, state, jsonOuts)
	end

	return 1
`)

// --- Lua script helpers ---

// validateScriptArgs checks that the args map contains valid string values
// for "trieKey" and "outputsKey". Returns an error if either key is missing
// or not a string.
func validateScriptArgs(args map[string]interface{}) error {
	trieKey, ok := args["trieKey"].(string)
	if !ok || trieKey == "" {
		return fmt.Errorf("validateScriptArgs: missing or invalid trieKey")
	}
	outputsKey, ok := args["outputsKey"].(string)
	if !ok || outputsKey == "" {
		return fmt.Errorf("validateScriptArgs: missing or invalid outputsKey")
	}
	return nil
}

func (o *v2Operations) runAddV2Script(ctx context.Context, client redis.UniversalClient, args map[string]interface{}) (*redis.Cmd, error) {
	if err := validateScriptArgs(args); err != nil {
		return nil, err
	}
	return addV2Script.Run(ctx, client,
		[]string{args["trieKey"].(string), args["outputsKey"].(string)},
		args["oldVersion"], args["newVersion"], args["keywords"],
		args["prefixes"], args["suffixes"], args["outputs"]), nil
}

func (o *v2Operations) runRemoveV2Script(ctx context.Context, client redis.UniversalClient, args map[string]interface{}) (*redis.Cmd, error) {
	if err := validateScriptArgs(args); err != nil {
		return nil, err
	}
	return removeV2Script.Run(ctx, client,
		[]string{args["trieKey"].(string), args["outputsKey"].(string)},
		args["oldVersion"], args["newVersion"], args["keywords"],
		args["prefixes"], args["suffixes"], args["outputs"]), nil
}
