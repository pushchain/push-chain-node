package mock

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestMockTransportSend(t *testing.T) {
	a := New("alice")
	b := New("bob")
	Link(a, b)

	ctx := context.Background()
	var wg sync.WaitGroup
	wg.Add(1)
	if err := b.RegisterHandler(func(ctx context.Context, sender string, payload []byte) error {
		if sender != "alice" {
			t.Fatalf("expected sender alice got %s", sender)
		}
		if string(payload) != "hello" {
			t.Fatalf("unexpected payload %s", payload)
		}
		wg.Done()
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.RegisterHandler(func(ctx context.Context, sender string, payload []byte) error {
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.EnsurePeer("bob", nil); err != nil {
		t.Fatal(err)
	}

	if err := a.Send(ctx, "bob", []byte("hello")); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for message delivery")
	}
}
