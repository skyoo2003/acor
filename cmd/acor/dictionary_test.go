// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"

	"github.com/skyoo2003/acor/pkg/acor"
)

func TestDictionaryCLI(t *testing.T) {
	server := miniredis.RunT(t)
	invoke := func(input string, args ...string) []byte {
		t.Helper()
		all := append([]string{"-addr", server.Addr(), "-name", "cli-v3", "dictionary"}, args...)
		var out, stderr bytes.Buffer
		if code := runWithInput(all, strings.NewReader(input), &out, &stderr, createService); code != 0 {
			t.Fatalf("code=%d err=%s", code, stderr.String())
		}
		return out.Bytes()
	}
	var status acor.VersionedStatus
	if err := json.Unmarshal(invoke("", "status"), &status); err != nil {
		t.Fatal(err)
	}
	var written acor.WriteResult
	if err := json.Unmarshal(invoke(`["hello","한국"]`, "replace", "--expected-version", string(status.ServingVersion)), &written); err != nil {
		t.Fatal(err)
	}
	var page acor.DictionaryPage
	if err := json.Unmarshal(invoke("", "list", "--limit", "1"), &page); err != nil || len(page.Keywords) != 1 || page.NextCursor == "" {
		t.Fatal(page, err)
	}
	invoke("", "list", "--cursor", page.NextCursor)
	invoke(`["hello"]`, "diff")
	invoke("", "prune")
	invoke(`[]`, "replace", "--allow-empty", "--expected-version", string(written.Version))
}
func TestDictionaryCLIGuards(t *testing.T) {
	cases := [][]string{
		{}, {"bogus"}, {"replace"}, {"replace", "--expected-version", "token"}, {"copy-v2", "--expected-version", "token"}, {"list", "--limit", "0"}}
	for _, args := range cases {
		var stderr bytes.Buffer
		if _, err := parseDictionary(args, strings.NewReader(`[]`), &stderr); err == nil {
			t.Fatal("accepted", args)
		}
	}
	for _, input := range []string{`["a"] []`, `[1]`, `no`} {
		var stderr bytes.Buffer
		if _, err := parseDictionary([]string{"diff"}, strings.NewReader(input), &stderr); err == nil {
			t.Fatal("accepted", input)
		}
	}
}
