package acor

import (
	"sync"
	"testing"
)

func TestConcurrentAdd(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	var wg sync.WaitGroup
	keywords := []string{"foo", "bar", "baz", "qux"}

	for i := 0; i < 100; i++ {
		for _, kw := range keywords {
			wg.Add(1)
			go func(keyword string) {
				defer wg.Done()
				_, _ = ac.Add(keyword)
			}(kw)
		}
	}
	wg.Wait()
}

func TestConcurrentAddAndFind(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	_, _ = ac.Add("foo")
	_, _ = ac.Add("bar")

	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, _ = ac.Add("baz")
		}()
		go func() {
			defer wg.Done()
			_, _ = ac.Find("foo bar baz")
		}()
	}
	wg.Wait()
}

func TestConcurrentFind(t *testing.T) {
	ac, mr := createAhoCorasick(t)
	defer mr.Close()
	defer func() { _ = ac.Close() }()

	keywords := []string{"foo", "bar", "baz"}
	for _, kw := range keywords {
		_, _ = ac.Add(kw)
	}

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = ac.Find("foo bar baz qux")
		}()
	}
	wg.Wait()
}
