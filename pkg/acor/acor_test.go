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
	defer func() { _ = ac.Close() }()
	ac.Flush()
}

func TestAddAndRemove(t *testing.T) {
	ac := createAhoCorasick()
	defer func() { _ = ac.Close() }()
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
	const input = "he"

	ac := createAhoCorasick()
	defer func() { _ = ac.Close() }()
	defer ac.Flush()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		ac.Add(keyword)
	}

	results = ac.Suggest(input)
	t.Logf("Suggest(%s) : Results(%s)", input, results)

	if len(results) != 2 {
		t.Error("results' count is unexpected")
	}
	for _, result := range results {
		switch result {
		case "her", input:
			continue
		}
		t.Error("results have invalid data")
	}
}

func TestSuggestIndex(t *testing.T) {
	const input = "he"

	ac := createAhoCorasick()
	defer func() { _ = ac.Close() }()
	defer ac.Flush()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		ac.Add(keyword)
	}

	results := ac.SuggestIndex(input)

	if len(results) != 2 {
		t.Error("results' count is unexpected")
	}

	expected := map[string][]int{
		"he":  {0},
		"her": {0},
	}
	for keyword, indexes := range expected {
		actualIndexes, ok := results[keyword]
		if !ok {
			t.Errorf("results are missing %s", keyword)
			continue
		}
		if len(actualIndexes) != len(indexes) {
			t.Errorf("results for %s have unexpected count", keyword)
			continue
		}
		for idx, actualIndex := range actualIndexes {
			if actualIndex != indexes[idx] {
				t.Errorf("results for %s have invalid index", keyword)
			}
		}
	}

	emptyResults := ac.SuggestIndex("x")
	if len(emptyResults) != 0 {
		t.Error("results should be empty")
	}
}

func TestFind(t *testing.T) {
	var results []string
	const input = "he"

	ac := createAhoCorasick()
	defer func() { _ = ac.Close() }()
	defer ac.Flush()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		ac.Add(keyword)
	}
	ac.Debug()

	results = ac.Find(input)
	t.Logf("Find(%s) : Results(%s)", input, results)

	if len(results) != 1 {
		t.Error("results' count is unexpected")
	}
	for _, result := range results {
		if result == "he" {
			continue
		}
		t.Error("results have invalid data")
	}
}

func TestFindIndex(t *testing.T) {
	ac := createAhoCorasick()
	defer func() { _ = ac.Close() }()
	defer ac.Flush()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		ac.Add(keyword)
	}

	overlapResults := ac.FindIndex("her")
	overlapExpected := map[string][]int{
		"he":  {0},
		"her": {0},
	}
	assertIndexResults(t, overlapResults, overlapExpected)

	repeatedResults := ac.FindIndex("hehe")
	repeatedExpected := map[string][]int{
		"he": {0, 2},
	}
	assertIndexResults(t, repeatedResults, repeatedExpected)

	ac.Flush()
	ac.Add("한글")
	unicodeResults := ac.FindIndex("가한글")
	unicodeExpected := map[string][]int{
		"한글": {1},
	}
	assertIndexResults(t, unicodeResults, unicodeExpected)
}

func assertIndexResults(t *testing.T, actual, expected map[string][]int) {
	t.Helper()

	if len(actual) != len(expected) {
		t.Errorf("results' count is unexpected")
	}

	for keyword, expectedIndexes := range expected {
		actualIndexes, ok := actual[keyword]
		if !ok {
			t.Errorf("results are missing %s", keyword)
			continue
		}
		if len(actualIndexes) != len(expectedIndexes) {
			t.Errorf("results for %s have unexpected count", keyword)
			continue
		}
		for idx, actualIndex := range actualIndexes {
			if actualIndex != expectedIndexes[idx] {
				t.Errorf("results for %s have invalid index", keyword)
			}
		}
	}
}
