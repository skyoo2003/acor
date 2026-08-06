// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/redis/go-redis/v9"
)

const benchmarkInputText = "ushers hello world benchmark test"

func toJSONOrFatal(tb testing.TB, v interface{}) string {
	tb.Helper()
	result, err := toJSON(v)
	if err != nil {
		tb.Fatalf("toJSON(%T) failed: %v", v, err)
	}
	return result
}

func BenchmarkFindV1(b *testing.B) {
	mr := miniredis.RunT(b)

	client := newTestRedisClient(mr.Addr())
	_ = client.ZAdd(context.Background(), "{bench}:prefix", redis.Z{Score: 0, Member: ""}).Err()
	_ = client.Close()

	args := &AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "bench",
		SchemaVersion: SchemaV1,
	}

	ac, err := Create(args)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ac.Close() }()
	ac = v1Writable(b, ac)

	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}
	for _, kw := range keywords {
		if _, err := ac.Add(kw); err != nil {
			b.Fatalf("Add(%q) error: %v", kw, err)
		}
	}

	input := benchmarkInputText

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ac.Find(input)
	}
}

func BenchmarkFindV2(b *testing.B) {
	mr := miniredis.RunT(b)
	client := newTestRedisClient(mr.Addr())
	defer func() { _ = client.Close() }()

	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}
	prefixes := []string{
		"", "h", "he", "s", "sh", "she", "hi", "his", "her", "hers",
		"hel", "hell", "hello", "w", "wo", "wor", "worl", "world",
		"b", "be", "ben", "benc", "bench", "benchm", "benchma", "benchmar", "benchmark",
	}
	client.HSet(context.Background(), "{bench}:trie", map[string]interface{}{
		"keywords": toJSONOrFatal(b, keywords),
		"prefixes": toJSONOrFatal(b, prefixes),
		"version":  time.Now().Unix(),
	})

	outputs := map[string]interface{}{
		"he":        `["he"]`,
		"she":       `["he","she"]`,
		"his":       `["his"]`,
		"hers":      `["he","her","hers"]`,
		"hello":     `["hello"]`,
		"world":     `["world"]`,
		"benchmark": `["benchmark"]`,
	}
	client.HSet(context.Background(), "{bench}:outputs", outputs)

	args := &AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "bench",
	}

	ac, err := Create(args)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	input := benchmarkInputText

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ac.Find(input)
	}
}

func BenchmarkAddV1(b *testing.B) {
	mr := miniredis.RunT(b)

	client := newTestRedisClient(mr.Addr())
	_ = client.ZAdd(context.Background(), "{bench}:prefix", redis.Z{Score: 0, Member: ""}).Err()
	_ = client.Close()

	args := &AhoCorasickArgs{
		Addr:          mr.Addr(),
		Name:          "bench",
		SchemaVersion: SchemaV1,
	}

	ac, err := Create(args)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ac.Close() }()
	// V1 takes no new keywords, so the writes under measurement go through the
	// fixture writer. See BenchmarkRealServerAdd for why V1 stays in the comparison.
	ac = v1Writable(b, ac)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ac.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			b.Fatalf("Add() error: %v", err)
		}
	}
}

func BenchmarkAddV2(b *testing.B) {
	mr := miniredis.RunT(b)

	args := &AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "bench",
	}

	ac, err := Create(args)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	client := newTestRedisClient(mr.Addr())
	_ = client.HSet(context.Background(), "{bench}:trie", map[string]interface{}{
		"keywords": "[]",
		"prefixes": `[""]`,
		"version":  time.Now().Unix(),
	}).Err()
	_ = client.Close()
	ac.schemaVersion = SchemaV2

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ac.Add(fmt.Sprintf("keyword%d", i))
	}
}

func BenchmarkFind_WithCache(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "bench-cache",
		EnableCache: true,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	for i := 0; i < 100; i++ {
		if _, err := ac.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			b.Fatal(err)
		}
	}

	text := strings.Repeat("keyword50 keyword25 keyword75 ", 100)

	// Warm the cache before timing
	if _, err := ac.Find(text); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ac.Find(text); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkFind_WithoutCache(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatal(err)
	}
	defer mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "bench-no-cache",
		EnableCache: false,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	for i := 0; i < 100; i++ {
		if _, err := ac.Add(fmt.Sprintf("keyword%d", i)); err != nil {
			b.Fatal(err)
		}
	}

	text := strings.Repeat("keyword50 keyword25 keyword75 ", 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ac.Find(text); err != nil {
			b.Fatal(err)
		}
	}
}
