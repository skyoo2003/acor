// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"

	"github.com/redis/go-redis/v9"
)

// v2WriteScript commits a planned trie mutation under optimistic locking:
// it rejects the write (returns 0) when another client has already moved the
// version on, and otherwise swaps in the new trie fields and output states.
//
// Add and remove differ only in whether stale output states must be dropped
// first: add rewrites the states it touched, remove replaces the whole set.
// That is the clearOutputs flag.
//
// Precompiled with redis.NewScript so calls go out as EVALSHA.
var v2WriteScript = redis.NewScript(`
	local trieKey = KEYS[1]
	local outputsKey = KEYS[2]
	local oldVersion = ARGV[1]
	local newVersion = ARGV[2]
	local keywords = ARGV[3]
	local prefixes = ARGV[4]
	local outputsJson = ARGV[5]
	local clearOutputs = ARGV[6] == '1'

	local currentVersion = redis.call('HGET', trieKey, 'version')
	if currentVersion and currentVersion ~= oldVersion then
		return 0
	end

	redis.call('HSET', trieKey, 'keywords', keywords, 'prefixes', prefixes, 'version', newVersion)

	-- Decode before the DEL: a cjson error aborts the script without rolling
	-- back the commands already run, so nothing destructive may precede it.
	local outputs = cjson.decode(outputsJson)

	if clearOutputs then
		redis.call('DEL', outputsKey)
	end

	for state, jsonOuts in pairs(outputs) do
		redis.call('HSET', outputsKey, state, jsonOuts)
	end

	return 1
`)

// v2ScriptArgs is the complete argument set for v2WriteScript, built by
// marshalTrieArgs. Keeping the arguments typed here means a missing or mistyped
// one is a compile error rather than a runtime assertion.
type v2ScriptArgs struct {
	TrieKey    string
	OutputsKey string
	OldVersion int64
	NewVersion int64
	Keywords   string // JSON array of keywords
	Prefixes   string // JSON array of trie prefixes
	Outputs    string // JSON object: state -> JSON array of matched keywords
	// ClearOutputs drops the outputs hash before writing, for removes where a
	// state's output list may have shrunk to nothing.
	ClearOutputs bool
}

// runV2Script evaluates v2WriteScript and returns its reply: 1 when the write
// committed, 0 when the optimistic-lock version check failed.
//
// ClearOutputs goes out as a bool: go-redis encodes it as the "1"/"0" the
// script compares against, so there is no flag string to keep in sync.
func runV2Script(ctx context.Context, client redis.UniversalClient, args *v2ScriptArgs) (int64, error) {
	return v2WriteScript.Run(ctx, client,
		[]string{args.TrieKey, args.OutputsKey},
		args.OldVersion, args.NewVersion, args.Keywords,
		args.Prefixes, args.Outputs, args.ClearOutputs).Int64()
}
