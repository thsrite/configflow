package agents

import (
	"context"
	"testing"

	core "configflow/internal/agents"
	"configflow/internal/deploy/contentsync"
	"configflow/internal/repo"
)

func TestContentPoolFollowsManagerLifetime(t *testing.T) {
	var pool contentPool
	pool.checkers = map[*core.Manager]*contentsync.Checker{}
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	m, err := core.New(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	first := pool.checker(m)
	if first != pool.checker(m) || pool.dispatcher.Count() != 1 {
		t.Fatal("not one checker per manager")
	}
	m.Close()
	if len(pool.checkers) != 0 || pool.dispatcher.Count() != 0 {
		t.Fatal("retained closed manager")
	}
	pool.checker(m)
	if len(pool.checkers) != 0 || pool.dispatcher.Count() != 0 {
		t.Fatal("registered an already closed manager")
	}
}
func TestContentPoolClosesEveryManager(t *testing.T) {
	var pool contentPool
	pool.checkers = map[*core.Manager]*contentsync.Checker{}
	for i := 0; i < 2; i++ {
		r, err := repo.Open(t.TempDir(), repo.Options{})
		if err != nil {
			t.Fatal(err)
		}
		m, err := core.New(context.Background(), r)
		if err != nil {
			t.Fatal(err)
		}
		pool.checker(m)
	}
	if pool.dispatcher.Count() != 2 {
		t.Fatal("missing registration")
	}
	pool.close()
	pool.close()
	if pool.dispatcher.Count() != 0 || len(pool.checkers) != 0 {
		t.Fatal("shutdown registration leak")
	}
}
