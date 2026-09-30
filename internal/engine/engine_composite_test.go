// SPDX-License-Identifier: Apache-2.0

package engine

import (
	"sync"
	"testing"
	"time"
)

// Concurrent text chunks must share the request's worker limit rather than
// multiplying it by the number of chunk workers.
func TestCompositeSharesWorkerBound(t *testing.T) {
	shards := make([]*Engine, 4)
	for i := range shards {
		shards[i] = New(PresetMemoryEfficient)
		shards[i].Build(map[string]struct{}{"word": {}})
	}
	c := NewComposite(shards, 2).impl.(*compositeEngine)
	entered := make(chan struct{}, 12)
	release := make(chan struct{})
	var done sync.WaitGroup
	for range 3 {
		done.Go(func() { c.eachShard(func(int) { entered <- struct{}{}; <-release }) })
	}
	<-entered
	<-entered
	exceeded := false
	select {
	case <-entered:
		exceeded = true
	case <-time.After(20 * time.Millisecond):
	}
	close(release)
	done.Wait()
	if exceeded {
		t.Fatal("parallel chunks exceeded the shared shard worker bound")
	}
}

// Emitting a match must precede requesting its following rune. Scan relies on
// this boundary for candidate budgets and leftmost-longest window flushing.
func TestCompositeStreamingDoesNotReadAhead(t *testing.T) {
	for _, preset := range []Preset{PresetMemoryEfficient, PresetBalanced, PresetSpeed} {
		t.Run(preset.String(), func(t *testing.T) {
			shards := []*Engine{New(preset), New(preset)}
			shards[0].Build(map[string]struct{}{"she": {}})
			shards[1].Build(map[string]struct{}{"he": {}})
			engine := NewComposite(shards, 2)
			input := []rune("she more input")
			read := 0
			var found string
			engine.Stream(func() (rune, bool) {
				if read == len(input) {
					return 0, false
				}
				r := input[read]
				read++
				return r, true
			}, func(keyword string, start, end int) bool {
				found = keyword
				if start != 0 || end != 3 {
					t.Errorf("span = [%d,%d)", start, end)
				}
				return false
			})
			if read != 3 || found != "she" {
				t.Fatal(read, found)
			}
		})
	}
}
