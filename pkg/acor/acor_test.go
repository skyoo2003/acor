package acor

import (
	"errors"
	"fmt"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
)

func createTestRedisServer(t *testing.T) *miniredis.Miniredis {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}

	return mr
}

func createAhoCorasick(t *testing.T) (*AhoCorasick, *miniredis.Miniredis) {
	t.Helper()

	mr := createTestRedisServer(t)
	ac, err := Create(&AhoCorasickArgs{
		Addr:     mr.Addr(),
		Password: "",
		DB:       0,
		Name:     "test",
		Debug:    false,
	})
	if err != nil {
		mr.Close()
		t.Fatal(err)
	}

	return ac, mr
}

func TestInitAndFlushAndClose(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	if err := ac.Flush(); err != nil {
		t.Fatal(err)
	}
}

func TestAddAndRemove(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	addedCount, removedCount := 0, 0
	keywords := []string{"her", "he", "his"}

	for _, keyword := range keywords {
		count, err := ac.Add(keyword)
		if err != nil {
			t.Fatal(err)
		}
		addedCount += count
	}
	if addedCount != 3 {
		t.Errorf("The added count is not fit")
	}

	for _, keyword := range keywords {
		count, err := ac.Remove(keyword)
		if err != nil {
			t.Fatal(err)
		}
		removedCount += count
	}
	if removedCount != 3 {
		t.Errorf("The removed count is not fit")
	}
}

func TestSuggest(t *testing.T) {
	var results []string
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatal(err)
		}
	}

	var err error
	results, err = ac.Suggest(input)
	if err != nil {
		t.Fatal(err)
	}
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

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatal(err)
		}
	}

	results, err := ac.SuggestIndex(input)
	if err != nil {
		t.Fatal(err)
	}

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

	emptyResults, err := ac.SuggestIndex("x")
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyResults) != 0 {
		t.Error("results should be empty")
	}
}

func TestFind(t *testing.T) {
	var results []string
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatal(err)
		}
	}
	ac.Debug()

	var err error
	results, err = ac.Find(input)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Find(%s) : Results(%s)", input, results)

	if len(results) != 1 {
		t.Error("results' count is unexpected")
	}
	for _, result := range results {
		if result == input {
			continue
		}
		t.Error("results have invalid data")
	}
}

func TestFindIndex(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	keywords := []string{"her", "he", "his"}
	for _, keyword := range keywords {
		if _, err := ac.Add(keyword); err != nil {
			t.Fatal(err)
		}
	}

	overlapResults, err := ac.FindIndex("her")
	if err != nil {
		t.Fatal(err)
	}
	overlapExpected := map[string][]int{
		"he":  {0},
		"her": {0},
	}
	assertIndexResults(t, overlapResults, overlapExpected)

	repeatedResults, err := ac.FindIndex("hehe")
	if err != nil {
		t.Fatal(err)
	}
	repeatedExpected := map[string][]int{
		"he": {0, 2},
	}
	assertIndexResults(t, repeatedResults, repeatedExpected)

	err = ac.Flush()
	if err != nil {
		t.Fatal(err)
	}
	_, err = ac.Add("한글")
	if err != nil {
		t.Fatal(err)
	}
	unicodeResults, err := ac.FindIndex("가한글")
	if err != nil {
		t.Fatal(err)
	}
	unicodeExpected := map[string][]int{
		"한글": {1},
	}
	assertIndexResults(t, unicodeResults, unicodeExpected)
}

func TestCreateReturnsErrorWhenRedisUnavailable(t *testing.T) {
	mr := createTestRedisServer(t)
	addr := mr.Addr()
	mr.Close()

	ac, err := Create(&AhoCorasickArgs{
		Addr:     addr,
		Password: "",
		DB:       0,
		Name:     "test",
		Debug:    false,
	})
	if err == nil {
		t.Fatal("expected create to return an error")
	}
	if ac != nil {
		t.Fatal("expected create to return nil aho-corasick")
	}
}

func TestFindReturnsErrorWhenRedisUnavailable(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add("he"); err != nil {
		t.Fatal(err)
	}

	mr.Close()

	if _, err := ac.Find("he"); err == nil {
		t.Fatal("expected find to return an error")
	}
}

func TestAddRollsBackPartialTrieWrites(t *testing.T) {
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	hookErr := errors.New("forced build trie failure")
	ac.buildTrieHook = func(prefix string) error {
		if prefix == input {
			return hookErr
		}
		return nil
	}

	addedCount, err := ac.Add(input)
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected add to fail with hook error, got %v", err)
	}
	if addedCount != 0 {
		t.Fatalf("expected add count to be zero on rollback, got %d", addedCount)
	}

	ac.buildTrieHook = nil

	results, err := ac.Find(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected no matches after rollback, got %v", results)
	}

	indexResults, err := ac.FindIndex(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(indexResults) != 0 {
		t.Fatalf("expected no indexed matches after rollback, got %v", indexResults)
	}
}

func TestAddFailedReAddKeepsExistingKeywordState(t *testing.T) {
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()
	defer func() { _ = ac.Flush() }()

	if _, err := ac.Add(input); err != nil {
		t.Fatal(err)
	}

	pKey := fmt.Sprintf(PrefixKey, ac.name)
	if _, err := ac.redisClient.ZRem(ac.ctx, pKey, input).Result(); err != nil {
		t.Fatal(err)
	}

	hookErr := errors.New("forced duplicate rebuild failure")
	ac.buildTrieHook = func(prefix string) error {
		if prefix == input {
			return hookErr
		}
		return nil
	}

	addedCount, err := ac.Add(input)
	if !errors.Is(err, hookErr) {
		t.Fatalf("expected duplicate add to fail with hook error, got %v", err)
	}
	if addedCount != 0 {
		t.Fatalf("expected duplicate add count to be zero, got %d", addedCount)
	}

	ac.buildTrieHook = nil

	results, err := ac.Find(input)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0] != input {
		t.Fatalf("expected existing keyword state to remain after failed re-add, got %v", results)
	}

	indexResults, err := ac.FindIndex(input)
	if err != nil {
		t.Fatal(err)
	}
	assertIndexResults(t, indexResults, map[string][]int{input: {0}})
}

func TestInfoSuggestAndSuggestIndexReturnErrorsWhenRedisUnavailable(t *testing.T) {
	const input = "he"

	ac, mr := createAhoCorasick(t)
	defer func() { _ = ac.Close() }()

	if _, err := ac.Add(input); err != nil {
		t.Fatal(err)
	}

	mr.Close()

	if _, err := ac.Info(); err == nil {
		t.Fatal("expected info to return an error")
	}
	if _, err := ac.Suggest(input); err == nil {
		t.Fatal("expected suggest to return an error")
	}
	if _, err := ac.SuggestIndex(input); err == nil {
		t.Fatal("expected suggest index to return an error")
	}
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
