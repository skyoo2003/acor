// SPDX-License-Identifier: Apache-2.0

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

	if _, err = ac1.Add("hello"); err != nil {
		t.Fatal(err)
	}
	if _, err = ac1.Find("hello world"); err != nil {
		t.Fatal(err)
	}

	if _, _, valid := ac1.cache.get(); !valid {
		t.Fatal("expected cache to be valid after Find()")
	}

	ac2, err := Create(&AhoCorasickArgs{
		Addr:        mr.Addr(),
		Name:        "test-pubsub",
		EnableCache: false,
	})
	if err != nil {
		t.Fatalf("Create ac2 failed: %v", err)
	}
	defer func() { _ = ac2.Close() }()

	if _, err := ac2.Add("world"); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if _, _, valid := ac1.cache.get(); !valid {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Error("expected ac1 cache to be invalidated after ac2.Add()")
}
