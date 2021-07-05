package acor

import (
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	redis "github.com/go-redis/redis/v8"
)

func createTestRedisClient() *redis.Client {
	mr, err := miniredis.Run()
	if err != nil {
		panic(err)
	}
	return redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
}

func createAhoCorasick() *AhoCorasick {
	ac := Create(&AhoCorasickArgs{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
		Name:     "test",
		Debug:    false,
	})
	ac.redisClient = createTestRedisClient()
	return ac
}

func TestInitAndFlushAndClose(t *testing.T) {
	ac := createAhoCorasick()
	defer ac.Close()
	ac.Flush()
}

func TestAddAndRemove(t *testing.T) {
	ac := createAhoCorasick()
	defer ac.Close()
	defer ac.Flush()

	addedCount, removedCount := 0, 0
	keywords := []string{"her", "he", "his"}

	for _, keyword := range keywords {
		addedCount += ac.Add(keyword)
	}
	if addedCount != 3 {
		t.Errorf("The added count is not fit")
	}

	for _, keyword := range keywords {
		removedCount += ac.Remove(keyword)
	}
	if removedCount != 3 {
		t.Errorf("The removed count is not fit")
	}
}

func TestSuggest(t *testing.T) {
	var results []string

	ac := createAhoCorasick()
	defer ac.Close()
	defer ac.Flush()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		ac.Add(keyword)
	}

	input := "he"
	results = ac.Suggest(input)
	t.Logf("Suggest(%s) : Results(%s)", input, results)

	if len(results) != 2 {
		t.Error("results' count is unexpected")
	}
	for _, result := range results {
		switch result {
		case "her", "he":
			continue
		}
		t.Error("results have invalid data")
	}
}

func TestFind(t *testing.T) {
	var results []string

	ac := createAhoCorasick()
	defer ac.Close()
	defer ac.Flush()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		ac.Add(keyword)
	}
	ac.Debug()

	input := "he"
	results = ac.Find(input)
	t.Logf("Find(%s) : Results(%s)", input, results)

	if len(results) != 1 {
		t.Error("results' count is unexpected")
	}
	for _, result := range results {
		switch result {
		case "he":
			continue
		}
		t.Error("results have invalid data")
	}
}
