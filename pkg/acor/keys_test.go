// SPDX-License-Identifier: Apache-2.0

package acor

import "testing"

// The on-Redis V2 format is part of the v1 compatibility promise, additively:
// new keys and fields may appear, but the name and meaning of an existing one is
// fixed for the whole v1 line (docs/content/reference/compatibility.md).
//
// No API-diff tool can see that — Redis key strings are not Go symbols. These
// tests are what makes the rule mechanical: renaming a key or a hash field fails
// here, while adding one only requires appending a case. A mixed-version fleet
// mid-rolling-deploy is what breaks if a rename ships, and it breaks in
// production rather than in CI.
//
// What this does not prove: that an old ACOR can actually read what a new ACOR
// writes. Only a two-version harness shows that. Renames are caught; interop is
// not.

func TestV2KeyNames(t *testing.T) {
	const name = "test"

	tests := []struct {
		name string
		got  string
		want string
	}{
		// The hash tag keeps every key of a collection on one cluster slot, so the
		// braces are load-bearing rather than decorative.
		{"keyPrefix", keyPrefix(name), "{test}"},
		{"keywordKey", keywordKey(name), "{test}:keyword"},
		{"prefixKey", prefixKey(name), "{test}:prefix"},
		{"suffixKey", suffixKey(name), "{test}:suffix"},
		{"trieKey", trieKey(name), "{test}:trie"},
		{"outputsKey", outputsKey(name), "{test}:outputs"},
		{"nodesKey", nodesKey(name), "{test}:nodes"},
		{"outputKey", outputKey(name, "s1"), "{test}:output:s1"},
		{"nodeKey", nodeKey(name, "he"), "{test}:node:he"},
		{"migrationLockKey", (&AhoCorasick{name: name}).migrationLockKey(), "{test}:migration_lock"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

func TestV2FieldNames(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{"fieldKeywords", fieldKeywords, "keywords"},
		{"fieldPrefixes", fieldPrefixes, "prefixes"},
		{"fieldVersion", fieldVersion, "version"},
	}

	for _, tt := range tests {
		if tt.got != tt.want {
			t.Errorf("%s = %q, want %q", tt.name, tt.got, tt.want)
		}
	}
}

// TestEmptyTrieFieldNames pins what a freshly created V2 collection writes. An
// extra field here is legal and only needs a line added; a missing or renamed one
// is a version older than the change failing to find what it expects.
func TestEmptyTrieFieldNames(t *testing.T) {
	fields := emptyTrieFields()

	want := []string{fieldKeywords, fieldPrefixes, fieldVersion}
	for _, f := range want {
		if _, ok := fields[f]; !ok {
			t.Errorf("emptyTrieFields() is missing field %q", f)
		}
	}
	if len(fields) != len(want) {
		t.Errorf("emptyTrieFields() has %d fields, want %d: %v", len(fields), len(want), fields)
	}

	// The empty values are read by every instance that opens the collection, so
	// their JSON shape is as fixed as the field names.
	if got := fields[fieldKeywords]; got != emptyKeywordsJSON {
		t.Errorf("empty %s = %v, want %q", fieldKeywords, got, emptyKeywordsJSON)
	}
	if got := fields[fieldPrefixes]; got != emptyStringArrayJSON {
		t.Errorf("empty %s = %v, want %q", fieldPrefixes, got, emptyStringArrayJSON)
	}
}
