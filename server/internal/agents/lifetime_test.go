package agents

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"configflow/internal/repo"
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
func TestSingletonReplacementClosesOldManager(t *testing.T) {
	a, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	first, err := Get(repo.WithRepository(context.Background(), a))
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	first.OnClose(func() { calls++ })
	second, err := Get(repo.WithRepository(context.Background(), b))
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || first == second {
		t.Fatal("manager replacement retained dependent")
	}
	second.Close()
}
