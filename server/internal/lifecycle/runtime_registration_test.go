package lifecycle

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestRuntimeDirectRegistrationAndStop(t *testing.T) {
	rt := NewRuntime(Options{Grace: time.Second, Registry: NewRegistry()})

	var mu sync.Mutex
	var events []string
	record := func(e string) {
		mu.Lock()
		events = append(events, e)
		mu.Unlock()
	}

	rt.RegisterStartup(Startup{Name: "s1", Run: func(context.Context) error {
		record("s1")
		return nil
	}})
	rt.RegisterStartup(Startup{Name: "s2", Run: func(context.Context) error {
		record("s2")
		return nil
	}})
	ran := make(chan struct{}, 1)
	rt.RegisterPeriodic(Periodic{
		Name:     "p1",
		Start:    AtBoot,
		Interval: func(context.Context) time.Duration { return time.Hour },
		Round: func(context.Context) error {
			select {
			case ran <- struct{}{}:
			default:
			}
			return nil
		},
	})
	rt.RegisterShutdown(ShutdownHook{Name: "h1", Phase: PhaseStopSignal, Run: func(context.Context) { record("h1") }})
	rt.RegisterShutdown(ShutdownHook{Name: "h2", Phase: PhaseClose, Run: func(context.Context) { record("h2") }})

	names := rt.Names()
	if !slices.Equal(names, []string{"p1", "s1", "s2"}) {
		t.Fatalf("Names() = %v, want [p1 s1 s2]", names)
	}

	ctx := context.Background()
	rt.RunStartup(ctx)
	rt.Start(ctx)
	select {
	case <-ran:
	case <-time.After(2 * time.Second):
		t.Fatal("periodic loop did not run")
	}
	rt.Stop(time.Second)

	mu.Lock()
	got := append([]string(nil), events...)
	mu.Unlock()
	if !slices.Equal(got, []string{"s1", "s2", "h1", "h2"}) {
		t.Fatalf("events = %v, want [s1 s2 h1 h2]", got)
	}
}
