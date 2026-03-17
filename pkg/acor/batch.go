package acor

import (
	"strings"
	"sync"
)

func (ac *AhoCorasick) AddMany(keywords []string, opts *BatchOptions) (*BatchResult, error) {
	if opts == nil {
		opts = &BatchOptions{Mode: BatchModeBestEffort}
	}

	result := &BatchResult{
		Added:   make([]string, 0),
		Failed:  make([]KeywordError, 0),
		Skipped: make([]string, 0),
	}

	if opts.Mode == BatchModeTransactional {
		return ac.addManyTransactional(keywords, result)
	}
	return ac.addManyBestEffort(keywords, result)
}

func (ac *AhoCorasick) addManyBestEffort(keywords []string, result *BatchResult) (*BatchResult, error) {
	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   ErrEmptyKeyword,
			})
			continue
		}

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

	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			ac.rollbackAdded(added)
			return nil, ErrEmptyKeyword
		}

		count, err := ac.Add(keyword)
		if err != nil {
			ac.rollbackAdded(added)
			return nil, err
		}

		if count > 0 {
			added = append(added, keyword)
		}
	}

	result.Added = added
	return result, nil
}

func (ac *AhoCorasick) rollbackAdded(keywords []string) {
	var wg sync.WaitGroup
	for _, keyword := range keywords {
		wg.Add(1)
		go func(k string) {
			defer wg.Done()
			_, _ = ac.Remove(k)
		}(keyword)
	}
	wg.Wait()
}

func (ac *AhoCorasick) RemoveMany(keywords []string) (*BatchResult, error) {
	result := &BatchResult{
		Added:   make([]string, 0),
		Failed:  make([]KeywordError, 0),
		Skipped: make([]string, 0),
	}

	for _, keyword := range keywords {
		keyword = strings.TrimSpace(keyword)
		if keyword == "" {
			continue
		}

		remaining, err := ac.Remove(keyword)
		if err != nil {
			result.Failed = append(result.Failed, KeywordError{
				Keyword: keyword,
				Error:   err,
			})
			continue
		}

		result.Added = append(result.Added, keyword)
		_ = remaining
	}

	return result, nil
}
