package acor

import (
	"strings"
	"sync"
)

// AddMany adds multiple keywords to the Aho-Corasick automaton in batch mode.
func (ac *AhoCorasick) AddMany(keywords []string, opts *BatchOptions) (*BatchResult, error) {
	if opts == nil {
		opts = &BatchOptions{Mode: BatchModeBestEffort}
	}

	result := &BatchResult{
		Added:   make([]string, 0),
		Removed: make([]string, 0),
		Failed:  make([]KeywordError, 0),
		Skipped: make([]string, 0),
	}

	if opts.Mode == BatchModeTransactional {
		return ac.addManyTransactional(keywords, result)
	}
	return ac.addManyBestEffort(keywords, result)
}

func (ac *AhoCorasick) addManyBestEffort(keywords []string, result *BatchResult) (*BatchResult, error) {
	seen := make(map[string]bool)

	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   ErrEmptyKeyword,
			})
			continue
		}

		if seen[keyword] {
			result.Skipped = append(result.Skipped, keyword)
			continue
		}
		seen[keyword] = true

		count, err := ac.Add(keyword)
		if err != nil {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   err,
			})
			continue
		}

		if count == 0 {
			result.Skipped = append(result.Skipped, keyword)
		} else {
			result.Added = append(result.Added, keyword)
		}
	}

	return result, nil
}

func (ac *AhoCorasick) addManyTransactional(keywords []string, result *BatchResult) (*BatchResult, error) {
	added := make([]string, 0)
	seen := make(map[string]bool)

	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			ac.rollbackAdded(added)
			return nil, ErrEmptyKeyword
		}

		if seen[keyword] {
			result.Skipped = append(result.Skipped, keyword)
			continue
		}
		seen[keyword] = true

		count, err := ac.Add(keyword)
		if err != nil {
			ac.rollbackAdded(added)
			return nil, err
		}

		if count > 0 {
			added = append(added, keyword)
		} else {
			result.Skipped = append(result.Skipped, keyword)
		}
	}

	result.Added = added
	return result, nil
}

func (ac *AhoCorasick) rollbackAdded(keywords []string) {
	if len(keywords) == 0 {
		return
	}

	maxWorkers := 10
	if len(keywords) < maxWorkers {
		maxWorkers = len(keywords)
	}

	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup

	for _, keyword := range keywords {
		sem <- struct{}{}
		wg.Add(1)
		go func(k string) {
			defer func() {
				<-sem
				wg.Done()
			}()
			if _, err := ac.Remove(k); err != nil && ac.logger != nil {
				ac.logger.Printf("rollback: failed to remove %q: %v", k, err)
			}
		}(keyword)
	}
	wg.Wait()
}

// RemoveMany removes multiple keywords from the Aho-Corasick automaton.
func (ac *AhoCorasick) RemoveMany(keywords []string) (*BatchResult, error) {
	result := &BatchResult{
		Added:   make([]string, 0),
		Removed: make([]string, 0),
		Failed:  make([]KeywordError, 0),
		Skipped: make([]string, 0),
	}

	seen := make(map[string]bool)

	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}

		if seen[keyword] {
			result.Skipped = append(result.Skipped, keyword)
			continue
		}
		seen[keyword] = true

		_, err := ac.Remove(keyword)
		if err != nil {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   err,
			})
			continue
		}

		result.Removed = append(result.Removed, keyword)
	}

	return result, nil
}

// FindMany searches for keywords in multiple texts and returns a map of text to matches.
// Note: If the same text appears multiple times in the input slice, only one result
// entry will be stored (last occurrence wins).
func (ac *AhoCorasick) FindMany(texts []string) (map[string][]string, error) {
	results := make(map[string][]string)

	for _, text := range texts {
		matches, err := ac.Find(text)
		if err != nil {
			return nil, err
		}
		results[text] = matches
	}

	return results, nil
}
