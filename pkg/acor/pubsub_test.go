package acor

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

func TestPubSub_Invalidate(t *testing.T) {
	mr := miniredis.RunT(t)

	ac1, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-pubsub",
		EnableCache: true,
	})
	if err != nil {
		t.Fatalf("Create ac1 failed: %v", err)
	}
	defer func() { _ = ac1.Close() }()

	_, _ = ac1.Add("hello")
	_, _ = ac1.Find("hello world")

	if _, _, valid := ac1.cache.get(); !valid {
		t.Fatal("expected cache to be valid after Find()")
	}

	ac2, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-pubsub",
		EnableCache: true,
	})
	if err != nil {
		t.Fatalf("Create ac2 failed: %v", err)
	}
	defer func() { _ = ac2.Close() }()

	_, _ = ac2.Add("world")

	time.Sleep(100 * time.Millisecond)

	if _, _, valid := ac1.cache.get(); valid {
		t.Error("expected ac1 cache to be invalidated after ac2.Add()")
	}
}
