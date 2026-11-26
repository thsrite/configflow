package lifecycle_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"configflow/internal/lifecycle"
)

func TestSpawnerFromKeepsOrigin(t *testing.T) {
	if _, ok := lifecycle.SpawnerFrom(t.Context()).(lifecycle.ContextSpawner); !ok {
		t.Fatal("unbound context must use ContextSpawner")
	}
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(t.Context())
	defer rt.Shutdown(t.Context())
	ctx := rt.BindRequest(t.Context())
	if lifecycle.SpawnerFrom(ctx) != rt {
		t.Fatal("origin Runtime lost")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	if err := lifecycle.SpawnerFrom(ctx).Spawn(ctx, "origin", func(work context.Context) { close(entered); <-release }); err != nil {
		t.Fatal(err)
	}
	<-entered
	wait, cancel := context.WithCancel(t.Context())
	cancel()
	if err := rt.WaitIdle(wait); !errors.Is(err, context.Canceled) {
		t.Fatal("origin did not track work", err)
	}
}

func TestSpawnerFromStoppedGenerationNeverFallsBack(t *testing.T) {
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(t.Context())
	old := rt.BindRequest(t.Context())
	rt.Shutdown(t.Context())
	for _, restart := range []bool{false, true} {
		if restart {
			rt.Start(t.Context())
			defer rt.Shutdown(t.Context())
		}
		var called atomic.Bool
		err := lifecycle.SpawnerFrom(old).Spawn(old, "stale-origin", func(context.Context) { called.Store(true) })
		if !errors.Is(err, lifecycle.ErrStopping) || called.Load() {
			t.Fatalf("stale generation moved to a live Runtime: restart=%t err=%v", restart, err)
		}
	}
}
