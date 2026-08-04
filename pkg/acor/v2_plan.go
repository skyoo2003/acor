// SPDX-License-Identifier: Apache-2.0

package acor

import "strings"

// This file holds the pure, side-effect-free half of a V2 write: given a trie
// snapshot and one or more keywords, work out the new snapshot and the output
// lists that changed. Both V2 callers — v2Operations (Redis-resident reads) and
// redisBackedAC (preset mode, local engine) — plan identically and differ only
// in what they do after the Lua script commits, so the planning lives here once.
//
// The *Many variants are the primitives and the single-keyword forms wrap them.
// Planning a batch in one pass is what keeps AddMany linear: every plan call
// rebuilds the keyword and prefix sets and recomputes outputs for every existing
// prefix, so doing that per keyword made a batch quadratic.

// planAdd computes the trie mutation for inserting keyword. On success snap is
// updated in place and outputs maps each state whose output list changed to its
// new value. changed is false when the keyword is already present, in which case
// snap is untouched and no write is needed.
func planAdd(snap *trieSnapshot, keyword string) (outputs map[string][]string, changed bool) {
	outputs, added := planAddMany(snap, []string{keyword})
	return outputs, len(added) > 0
}

// planAddMany computes one trie mutation covering every keyword in keywords. It
// returns the states whose output lists changed and the keywords that were
// actually new; added is empty (and outputs nil) when every keyword was already
// present, in which case snap is untouched.
//
// The result is identical to applying planAdd to each keyword in turn, since the
// single-keyword path also recomputes every prefix against the final keyword set.
// Folding N passes into one changes cost, not outcome.
func planAddMany(snap *trieSnapshot, keywords []string) (outputs map[string][]string, added []string) {
	keywordSet := make(map[string]struct{}, len(snap.Keywords)+len(keywords))
	for _, kw := range snap.Keywords {
		keywordSet[kw] = struct{}{}
	}
	prefixSet := make(map[string]struct{}, len(snap.Prefixes))
	for _, p := range snap.Prefixes {
		prefixSet[p] = struct{}{}
	}

	var newPrefixes []string
	addPrefix := func(prefix string) {
		if _, exists := prefixSet[prefix]; !exists {
			newPrefixes = append(newPrefixes, prefix)
			prefixSet[prefix] = struct{}{}
		}
	}
	for _, keyword := range keywords {
		if keyword == "" {
			continue
		}
		if _, exists := keywordSet[keyword]; exists {
			continue
		}
		keywordSet[keyword] = struct{}{}
		added = append(added, keyword)

		// Ranging a string yields exactly the rune start offsets, so slicing at each
		// of them (past 0) plus the whole keyword enumerates every prefix. Not
		// byteOff+utf8.RuneLen(r): invalid UTF-8 decodes to RuneError, whose RuneLen
		// is 3, which slices past the end of the string and panics.
		for byteOff := range keyword {
			if byteOff == 0 {
				continue
			}
			addPrefix(keyword[:byteOff])
		}
		addPrefix(keyword)
	}
	if len(added) == 0 {
		return nil, nil
	}

	outputs = make(map[string][]string)
	for _, prefix := range newPrefixes {
		if outs := computeOutputs(prefix, prefixSet, keywordSet); len(outs) > 0 {
			outputs[prefix] = outs
		}
	}
	// Pre-existing states can gain an output too: a new keyword may be a suffix
	// of one of them, so every old prefix is recomputed.
	for _, prefix := range snap.Prefixes {
		if prefix == "" {
			continue
		}
		if outs := computeOutputs(prefix, prefixSet, keywordSet); len(outs) > 0 {
			outputs[prefix] = outs
		}
	}

	snap.Keywords = append(snap.Keywords, added...)
	snap.Prefixes = append(snap.Prefixes, newPrefixes...)
	return outputs, added
}

// planRemove computes the trie mutation for deleting keyword. On success snap is
// updated in place and outputs holds the full replacement output set (the remove
// script clears the outputs hash first, so this is not a delta). changed is false
// when the keyword is absent.
func planRemove(snap *trieSnapshot, keyword string) (outputs map[string][]string, changed bool) {
	outputs, removed := planRemoveMany(snap, []string{keyword})
	return outputs, len(removed) > 0
}

// planRemoveMany computes one trie mutation deleting every keyword in keywords,
// returning the full replacement output set and the keywords actually present.
// removed is empty (and outputs nil) when none of them were.
func planRemoveMany(snap *trieSnapshot, keywords []string) (outputs map[string][]string, removed []string) {
	doomed := make(map[string]struct{}, len(keywords))
	for _, kw := range keywords {
		doomed[kw] = struct{}{}
	}

	newKeywords := make([]string, 0, len(snap.Keywords))
	for _, kw := range snap.Keywords {
		if _, drop := doomed[kw]; drop {
			removed = append(removed, kw)
			continue
		}
		newKeywords = append(newKeywords, kw)
	}
	if len(removed) == 0 {
		return nil, nil
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
	return outputs, removed
}

// computeOutputs returns every keyword that ends at state: state itself when it
// is a keyword, plus each proper suffix of state that is both a live prefix and
// a keyword (the outputs reachable by following failure links).
func computeOutputs(state string, prefixSet, keywordSet map[string]struct{}) []string {
	var outputs []string

	if _, isKeyword := keywordSet[state]; isKeyword {
		outputs = append(outputs, state)
	}

	// Proper suffixes, sliced out of state rather than rebuilt from its runes. A Go
	// substring shares the original backing array, so this allocates nothing, where
	// string([]rune(state)[i:]) allocated a fresh string per suffix — and this
	// function runs once per prefix on every write.
	for byteOff := range state {
		if byteOff == 0 {
			continue
		}
		failState := state[byteOff:]
		if _, isPrefix := prefixSet[failState]; !isPrefix {
			continue
		}
		if _, isKeyword := keywordSet[failState]; isKeyword {
			outputs = append(outputs, failState)
		}
	}

	return outputs
}
