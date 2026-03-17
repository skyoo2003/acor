package acor

import (
	"context"
	"fmt"
	"testing"
	"time"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

const benchmarkInputText = "ushers hello world benchmark test"

func BenchmarkFindV1(b *testing.B) {
	mr := miniredis.RunT(b)

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	_ = client.ZAdd(context.Background(), "{bench}:prefix", &redis.Z{Score: 0, Member: ""}).Err()
	_ = client.Close()

	args := &AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "bench",
	}

	ac, err := Create(args)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}
	for _, kw := range keywords {
		_, _ = ac.Add(kw)
	}

	input := benchmarkInputText

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ac.Find(input)
	}
}

func BenchmarkFindV2(b *testing.B) {
	mr := miniredis.RunT(b)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	keywords := []string{"he", "she", "his", "hers", "hello", "world", "benchmark"}
	prefixes := []string{
		"", "h", "he", "s", "sh", "she", "hi", "his", "her", "hers",
		"hel", "hell", "hello", "w", "wo", "wor", "worl", "world",
		"b", "be", "ben", "benc", "bench", "benchm", "benchma", "benchmar", "benchmark",
	}
	suffixes := []string{
		"", "e", "eh", "s", "hs", "ehs", "i", "ih", "si", "sih",
		"r", "reh", "sreh", "l", "ll", "leh", "lleh", "d", "dl", "dor", "drow",
		"k", "kc", "kram", "kcehc", "kramdneb",
	}

	client.HSet(context.Background(), "{bench}:trie", map[string]interface{}{
		"keywords": mustJSON(keywords),
		"prefixes": mustJSON(prefixes),
		"suffixes": mustJSON(suffixes),
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

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	_ = client.ZAdd(context.Background(), "{bench}:prefix", &redis.Z{Score: 0, Member: ""}).Err()
	_ = client.Close()

	args := &AhoCorasickArgs{
		Addr: mr.Addr(),
		Name: "bench",
	}

	ac, err := Create(args)
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = ac.Close() }()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ac.Add(fmt.Sprintf("keyword%d", i))
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

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	_ = client.HSet(context.Background(), "{bench}:trie", map[string]interface{}{
		"keywords": "[]",
		"prefixes": `[""]`,
		"suffixes": `[""]`,
		"version":  time.Now().Unix(),
	}).Err()
	_ = client.Close()
	ac.schemaVersion = SchemaV2

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = ac.Add(fmt.Sprintf("keyword%d", i))
	}
}
