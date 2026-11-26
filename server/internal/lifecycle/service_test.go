package lifecycle_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

// A service does not hold the drain open, keeps running while the drain
// lasts, is cancelled afterwards and is joined before the close hooks.
func TestSpawnServiceOutlivesDrainAndIsJoined(t *testing.T) {
	registry := lifecycle.NewRegistry()
	order := make(chan string, 8)
	drained := make(chan struct{})
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: registry, Grace: 5 * time.Second, Drain: func(context.Context) error {
		order <- "drain"
		close(drained)
		return nil
	}})
	registry.RegisterShutdown(lifecycle.ShutdownHook{Name: "after", Phase: lifecycle.PhaseAfterDrain, Run: func(context.Context) { order <- "after-drain" }})
	registry.RegisterShutdown(lifecycle.ShutdownHook{Name: "close", Phase: lifecycle.PhaseClose, Run: func(context.Context) { order <- "close" }})
	runner.Start(context.Background())
	running := make(chan struct{})
	if err := runner.SpawnService(context.Background(), "svc", func(ctx context.Context) {
		close(running)
		<-ctx.Done()
		select {
		case <-drained:
			order <- "service-cancelled-after-drain"
		default:
			order <- "service-cancelled-early"
		}
		time.Sleep(50 * time.Millisecond) // graceful stop of what it owns
		order <- "service-done"
	}); err != nil {
		t.Fatal(err)
	}
	<-running
	if err := runner.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	runner.Shutdown(context.Background())
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Fatalf("shutdown waited %v: the service held the drain open", elapsed)
	}
	close(order)
	var got []string
	for event := range order {
		got = append(got, event)
	}
	at := map[string]int{}
	for i, event := range got {
		at[event] = i
	}
	if len(got) != 5 || got[0] != "drain" || got[4] != "close" {
		t.Fatalf("order %v", got)
	}
	if _, early := at["service-cancelled-early"]; early {
		t.Fatalf("service cancelled before the drain ended: %v", got)
	}
	if at["service-cancelled-after-drain"] > at["service-done"] || at["after-drain"] > at["close"] {
		t.Fatalf("order %v", got)
	}
	if err := runner.SpawnService(context.Background(), "late", func(context.Context) {}); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatalf("spawn after shutdown: %v", err)
	}
}

// A service that ignores cancellation is bounded by the overall grace.
func TestSpawnServiceBoundedByGrace(t *testing.T) {
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 300 * time.Millisecond})
	runner.Start(context.Background())
	release := make(chan struct{})
	defer close(release)
	if err := runner.SpawnService(context.Background(), "stuck", func(context.Context) { <-release }); err != nil {
		t.Fatal(err)
	}
	begin := time.Now()
	runner.Shutdown(context.Background())
	if elapsed := time.Since(begin); elapsed > 2*time.Second {
		t.Fatalf("shutdown took %v", elapsed)
	}
}

// A service that ignores cancellation neither takes the after-drain hooks'
// budget nor the close hooks' last 0.5 s.
func TestSpawnServiceDoesNotStarveHooks(t *testing.T) {
	registry := lifecycle.NewRegistry()
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: registry, Grace: 1500 * time.Millisecond})
	hooks := make(chan error, 2)
	registry.RegisterShutdown(lifecycle.ShutdownHook{Name: "after", Phase: lifecycle.PhaseAfterDrain, Run: func(ctx context.Context) { hooks <- ctx.Err() }})
	registry.RegisterShutdown(lifecycle.ShutdownHook{Name: "close", Phase: lifecycle.PhaseClose, Run: func(ctx context.Context) { hooks <- ctx.Err() }})
	runner.Start(context.Background())
	release := make(chan struct{})
	defer close(release)
	if err := runner.SpawnService(context.Background(), "stuck", func(context.Context) { <-release }); err != nil {
		t.Fatal(err)
	}
	runner.Shutdown(context.Background())
	for _, phase := range []string{"after-drain", "close"} {
		select {
		case err := <-hooks:
			if err != nil {
				t.Fatalf("%s hook ran with %v", phase, err)
			}
		default:
			t.Fatalf("%s hook did not run", phase)
		}
	}
}
