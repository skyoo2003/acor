// SPDX-License-Identifier: Apache-2.0

package acor

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestIsSelfEcho(t *testing.T) {
	const name = "coll"

	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{"own publish", name + ":msg-1", true},
		{"unknown id", name + ":msg-other", false},
		{"other collection", "other:msg-1", false},
		{"no separator", name, false},
		{"empty id", name + ":", false},
		{"id keeps trailing segments", name + ":msg-1:extra", false},
		{"empty payload", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var skip selfSkipSet
			skip.add("msg-1")

			if got := isSelfEcho(tt.payload, name, &skip); got != tt.want {
				t.Errorf("isSelfEcho(%q, %q) = %v, want %v", tt.payload, name, got, tt.want)
			}
		})
	}
}

// TestIsSelfEcho_RoundTripsGeneratedID guards the load-bearing detail that a real
// invalidation ID itself contains a ':': only the limited SplitN in isSelfEcho
// recovers it intact. A plain strings.Split here would silently turn every
// self-echo into a redundant invalidation, and the literal IDs above would not
// catch it.
func TestIsSelfEcho_RoundTripsGeneratedID(t *testing.T) {
	const name = "coll"

	var skip selfSkipSet
	id := newInvalidationID()
	skip.add(id)

	if !isSelfEcho(invalidationPayload(name, id), name, &skip) {
		t.Errorf("isSelfEcho did not recognize its own payload for generated ID %q", id)
	}
}

// stubSubscription is a Subscription whose message channel the test drives.
type stubSubscription struct {
	msgCh      chan PubSubMessage
	receiveErr error
	closed     chan struct{}
}

func newStubSubscription() *stubSubscription {
	return &stubSubscription{msgCh: make(chan PubSubMessage), closed: make(chan struct{}, 1)}
}

func (s *stubSubscription) Receive(context.Context) error { return s.receiveErr }
func (s *stubSubscription) Channel() <-chan PubSubMessage { return s.msgCh }
func (s *stubSubscription) Close() error                  { s.closed <- struct{}{}; return nil }

// stubSubStorage only supports Subscribe; the embedded nil interface panics if
// subscribeInvalidations ever reaches for another method.
type stubSubStorage struct {
	KVStorage
	sub Subscription
}

func (s stubSubStorage) Subscribe(context.Context, ...string) Subscription { return s.sub }

func TestSubscribeInvalidations_DeliversPayloads(t *testing.T) {
	sub := newStubSubscription()
	got := make(chan string, 1)
	stopCh := make(chan struct{})

	pubsub, err := subscribeInvalidations(context.Background(), stubSubStorage{sub: sub},
		"coll", stopCh, func(payload string) { got <- payload })
	if err != nil {
		t.Fatalf("subscribeInvalidations failed: %v", err)
	}
	// Closing the stub Subscription does not close its channel, so the listener
	// goroutine only exits via stopCh.
	t.Cleanup(func() { close(stopCh) })
	defer func() { _ = pubsub.Close() }()

	sub.msgCh <- PubSubMessage{Payload: "coll:msg-1"}

	select {
	case payload := <-got:
		if payload != "coll:msg-1" {
			t.Errorf("got payload %q, want %q", payload, "coll:msg-1")
		}
	case <-time.After(time.Second):
		t.Fatal("onMessage was never called")
	}
}

func TestSubscribeInvalidations_ReceiveErrorClosesSubscription(t *testing.T) {
	wantErr := errors.New("subscribe confirm failed")
	sub := newStubSubscription()
	sub.receiveErr = wantErr

	pubsub, err := subscribeInvalidations(context.Background(), stubSubStorage{sub: sub},
		"coll", make(chan struct{}), func(string) { t.Error("onMessage called after Receive error") })
	if !errors.Is(err, wantErr) {
		t.Fatalf("got error %v, want %v", err, wantErr)
	}
	if pubsub != nil {
		t.Error("expected nil Subscription on Receive error")
	}
	select {
	case <-sub.closed:
	default:
		t.Error("expected the subscription to be closed on Receive error")
	}
}

func TestSubscribeInvalidations_StopsOnStopCh(t *testing.T) {
	sub := newStubSubscription()
	stopCh := make(chan struct{})

	if _, err := subscribeInvalidations(context.Background(), stubSubStorage{sub: sub},
		"coll", stopCh, func(string) {}); err != nil {
		t.Fatalf("subscribeInvalidations failed: %v", err)
	}

	close(stopCh)
	assertListenerStopped(t, sub.msgCh)
}

func TestSubscribeInvalidations_StopsOnContextCancel(t *testing.T) {
	sub := newStubSubscription()
	ctx, cancel := context.WithCancel(context.Background())

	if _, err := subscribeInvalidations(ctx, stubSubStorage{sub: sub},
		"coll", make(chan struct{}), func(string) {}); err != nil {
		t.Fatalf("subscribeInvalidations failed: %v", err)
	}

	cancel()
	assertListenerStopped(t, sub.msgCh)
}

// assertListenerStopped verifies the listener goroutine has returned: a send on
// its unbuffered message channel only completes while it is still selecting.
// A live goroutine may deliver one more message before it notices the stop
// signal (select picks a ready case at random), so retry until a send blocks.
func assertListenerStopped(t *testing.T, msgCh chan PubSubMessage) {
	t.Helper()

	const attempts = 20
	for i := 0; i < attempts; i++ {
		select {
		case msgCh <- PubSubMessage{Payload: "coll:msg-1"}:
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
	t.Fatalf("listener still consuming messages after %d sends past the stop signal", attempts)
}
