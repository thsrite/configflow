package substore

import (
	"context"
	"testing"
)

func TestServiceOnContentChangeIsolationAndUnsubscribe(t *testing.T) {
	first := NewService(NewEnv())
	second := NewService(NewEnv())

	var firstCalls, secondCalls int
	unsub := first.OnContentChange(func(context.Context) error {
		firstCalls++
		return nil
	})
	second.OnContentChange(func(context.Context) error {
		secondCalls++
		return nil
	})

	first.NotifyContentChange(context.Background())
	if firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("expected isolated notification (1, 0), got (%d, %d)", firstCalls, secondCalls)
	}

	unsub()
	first.NotifyContentChange(context.Background())
	if firstCalls != 1 {
		t.Fatalf("expected unsubscribed callback not to fire, got %d", firstCalls)
	}

	second.NotifyContentChange(context.Background())
	if secondCalls != 1 {
		t.Fatalf("expected second service callback to fire once, got %d", secondCalls)
	}
}
