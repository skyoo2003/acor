package acor

import "testing"

func CreateAhoCorasick() *AhoCorasick {
	return Create(&AhoCorasickArgs{
		Addr:     "192.168.99.100:6379",
		Password: "",
		DB:       0,
		Name:     "test",
	})
}

func TestInitAndFlushAndClose(t *testing.T) {
	ac := CreateAhoCorasick()
	ac.Init()
	defer ac.Close()
	ac.Flush()
}

func TestAddAndRemove(t *testing.T) {
	ac := CreateAhoCorasick()
	ac.Init()
	defer ac.Close()

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

	ac.Flush()
}

func TestSuggest(t *testing.T) {
	var results []string

	ac := CreateAhoCorasick()
	ac.Init()
	defer ac.Close()

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

	ac.Flush()
}

func TestKoreanSuggest(t *testing.T) {
	var results []string

	ac := CreateAhoCorasick()
	ac.Init()
	defer ac.Close()

	keywords := []string{"실전게임", "실전고스톱", "실전맞고"}
	for _, keyword := range keywords {
		ac.Add(keyword)
	}

	input := "실전"
	results = ac.Suggest(input)
	t.Logf("Suggest(%s) : Results(%s)", input, results)

	if len(results) != 3 {
		t.Error("results' count is unexpected")
	}
	for _, result := range results {
		switch result {
		case "실전게임", "실전고스톱", "실전맞고":
			continue
		}
		t.Error("results have invalid data")
	}

	ac.Flush()
}
