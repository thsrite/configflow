package lifecycle_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/lifecycle"
)

// A service does not hold the drain open, keeps running while the drain
// lasts, is cancelled afterwards and is joined before after-drain hooks.
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
	want := []string{"drain", "service-cancelled-after-drain", "service-done", "after-drain"}
	if len(got) != len(want) {
		t.Fatalf("order %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order %v, want %v", got, want)
		}
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
