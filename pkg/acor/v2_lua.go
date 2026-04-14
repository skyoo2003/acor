package acor

import (
	"context"

	"github.com/go-redis/redis/v8"
)

// --- Lua script helpers ---

func (o *v2Operations) addV2Script(ctx context.Context, client redis.UniversalClient, args map[string]interface{}) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx, "eval", `
			local trieKey = KEYS[1]
			local outputsKey = KEYS[2]
			local oldVersion = tonumber(ARGV[1])
			local newVersion = tonumber(ARGV[2])
			local keywords = ARGV[3]
			local prefixes = ARGV[4]
			local suffixes = ARGV[5]
			local outputsJson = ARGV[6]

			local currentVersion = redis.call('HGET', trieKey, 'version')
			if currentVersion and tonumber(currentVersion) ~= oldVersion then
				return 0
			end

			redis.call('HSET', trieKey, 'keywords', keywords, 'prefixes', prefixes, 'suffixes', suffixes, 'version', newVersion)

			local outputs = cjson.decode(outputsJson)
			for state, jsonOuts in pairs(outputs) do
				redis.call('HSET', outputsKey, state, jsonOuts)
			end

			return 1
		`, luaKeys, args["trieKey"], args["outputsKey"],
		args["oldVersion"], args["newVersion"], args["keywords"],
		args["prefixes"], args["suffixes"], args["outputs"])
	if err := client.Process(ctx, cmd); err != nil {
		return cmd
	}
	return cmd
}

func (o *v2Operations) removeV2Script(ctx context.Context, client redis.UniversalClient, args map[string]interface{}) *redis.IntCmd {
	cmd := redis.NewIntCmd(ctx, "eval", `
			local trieKey = KEYS[1]
			local outputsKey = KEYS[2]
			local oldVersion = tonumber(ARGV[1])
			local newVersion = tonumber(ARGV[2])
			local keywords = ARGV[3]
			local prefixes = ARGV[4]
			local suffixes = ARGV[5]
			local outputsJson = ARGV[6]

			local currentVersion = redis.call('HGET', trieKey, 'version')
			if currentVersion and tonumber(currentVersion) ~= oldVersion then
				return 0
			end

			redis.call('HSET', trieKey, 'keywords', keywords, 'prefixes', prefixes, 'suffixes', suffixes, 'version', newVersion)

			redis.call('DEL', outputsKey)

			local outputs = cjson.decode(outputsJson)
			for state, jsonOuts in pairs(outputs) do
				redis.call('HSET', outputsKey, state, jsonOuts)
			end

			return 1
		`, luaKeys, args["trieKey"], args["outputsKey"],
		args["oldVersion"], args["newVersion"], args["keywords"],
		args["prefixes"], args["suffixes"], args["outputs"])
	if err := client.Process(ctx, cmd); err != nil {
		return cmd
	}
	return cmd
}
