// SPDX-License-Identifier: Apache-2.0

package acor

import "strings"

// This file holds the pure, side-effect-free half of a V2 write: given a trie
// snapshot and a keyword, work out the new snapshot and the output lists that
// changed. Both V2 callers — v2Operations (Redis-resident reads) and
// redisBackedAC (preset mode, local engine) — plan identically and differ only
// in what they do after the Lua script commits, so the planning lives here once.

// planAdd computes the trie mutation for inserting keyword. On success snap is
// updated in place and outputs maps each state whose output list changed to its
// new value. changed is false when the keyword is already present, in which case
// snap is untouched and no write is needed.
func planAdd(snap *trieSnapshot, keyword string) (outputs map[string][]string, changed bool) {
	keywordSet := make(map[string]struct{}, len(snap.Keywords)+1)
	for _, kw := range snap.Keywords {
		keywordSet[kw] = struct{}{}
	}
	if _, exists := keywordSet[keyword]; exists {
		return nil, false
	}

	prefixSet := make(map[string]struct{}, len(snap.Prefixes))
	for _, p := range snap.Prefixes {
		prefixSet[p] = struct{}{}
	}

	keywordRunes := []rune(keyword)
	var newPrefixes []string
	for i := range keywordRunes {
		prefix := string(keywordRunes[:i+1])
		if _, exists := prefixSet[prefix]; !exists {
			newPrefixes = append(newPrefixes, prefix)
			prefixSet[prefix] = struct{}{}
		}
	}

	keywordSet[keyword] = struct{}{}
	outputs = make(map[string][]string)
	for _, prefix := range newPrefixes {
		if outs := computeOutputs(prefix, prefixSet, keywordSet); len(outs) > 0 {
			outputs[prefix] = outs
		}
	}
	// Pre-existing states can gain an output too: the new keyword may be a
	// suffix of one of them, so every old prefix is recomputed.
	for _, prefix := range snap.Prefixes {
		if prefix == "" {
			continue
		}
		if outs := computeOutputs(prefix, prefixSet, keywordSet); len(outs) > 0 {
			outputs[prefix] = outs
		}
	}

	snap.Keywords = append(snap.Keywords, keyword)
	snap.Prefixes = append(snap.Prefixes, newPrefixes...)
	return outputs, true
}

// planRemove computes the trie mutation for deleting keyword. On success snap is
// updated in place and outputs holds the full replacement output set (the remove
// script clears the outputs hash first, so this is not a delta). changed is false
// when the keyword is absent.
func planRemove(snap *trieSnapshot, keyword string) (outputs map[string][]string, changed bool) {
	newKeywords := make([]string, 0, len(snap.Keywords))
	found := false
	for _, kw := range snap.Keywords {
		if kw == keyword {
			found = true
			continue
		}
		newKeywords = append(newKeywords, kw)
	}
	if !found {
		return nil, false
	}

	keywordSet := make(map[string]struct{}, len(newKeywords))
	for _, kw := range newKeywords {
		keywordSet[kw] = struct{}{}
	}

	// Keep only the prefixes still reachable from a surviving keyword.
	newPrefixes := []string{""}
	for _, prefix := range snap.Prefixes {
		if prefix == "" {
			continue
		}
		for kw := range keywordSet {
			if strings.HasPrefix(kw, prefix) {
				newPrefixes = append(newPrefixes, prefix)
				break
			}
		}
	}

	prefixSet := make(map[string]struct{}, len(newPrefixes))
	for _, p := range newPrefixes {
		prefixSet[p] = struct{}{}
	}

	outputs = make(map[string][]string)
	for _, prefix := range newPrefixes {
		if prefix == "" {
			continue
		}
		if outs := computeOutputs(prefix, prefixSet, keywordSet); len(outs) > 0 {
			outputs[prefix] = outs
		}
	}

	snap.Keywords = newKeywords
	snap.Prefixes = newPrefixes
	return outputs, true
}

// computeOutputs returns every keyword that ends at state: state itself when it
// is a keyword, plus each proper suffix of state that is both a live prefix and
// a keyword (the outputs reachable by following failure links).
func computeOutputs(state string, prefixSet, keywordSet map[string]struct{}) []string {
	var outputs []string

	if _, isKeyword := keywordSet[state]; isKeyword {
		outputs = append(outputs, state)
	}

	stateRunes := []rune(state)
	for i := 1; i < len(stateRunes); i++ {
		failState := string(stateRunes[i:])
		if _, isPrefix := prefixSet[failState]; !isPrefix {
			continue
		}
		if _, isKeyword := keywordSet[failState]; isKeyword {
			outputs = append(outputs, failState)
		}
	}

	return outputs
}
