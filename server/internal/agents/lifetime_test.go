package agents

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestManagerCloseRunsDependentsOnce(t *testing.T) {
	var m Manager
	var calls atomic.Int32
	if !m.OnClose(func() { calls.Add(1) }) {
		t.Fatal("not registered")
	}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); m.Close() }()
	}
	wg.Wait()
	if calls.Load() != 1 || m.OnClose(func() { calls.Add(1) }) {
		t.Fatal("lifetime leak")
	}
}
