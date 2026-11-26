package lifecycle

import (
	"context"
	"slices"
	"testing"
)

func TestRegistrationReportsOrderAndReplacements(t *testing.T) {
	rt := NewRuntime(Options{Mode: ModeManual, Registry: NewRegistry()})
	noop := func(context.Context) error { return nil }
	rt.RegisterStartup(Startup{Name: "a", Run: noop})
	rt.RegisterStartup(Startup{Name: "b", Run: noop})
	rt.RegisterPeriodic(Periodic{Name: "loop", Start: AfterFirstRequest, Round: noop})
	rt.RegisterShutdown(ShutdownHook{Name: "close", Phase: PhaseClose, Run: func(context.Context) {}})

	got := rt.Registration()
	if !slices.Equal(got.Startups, []string{"a", "b"}) || !slices.Equal(got.Periodics, []string{"loop"}) || !slices.Equal(got.Shutdowns, []string{"close"}) {
		t.Fatalf("Registration = %+v", got)
	}
	if len(got.Replaced) != 0 {
		t.Fatalf("Replaced = %v, want none", got.Replaced)
	}

	rt.RegisterStartup(Startup{Name: "a", Run: noop})
	rt.RegisterPeriodic(Periodic{Name: "loop", Start: AfterFirstRequest, Round: noop})
	rt.RegisterShutdown(ShutdownHook{Name: "close", Phase: PhaseClose, Run: func(context.Context) {}})
	got = rt.Registration()
	if want := []string{"startup a", "periodic loop", "shutdown close"}; !slices.Equal(got.Replaced, want) {
		t.Fatalf("Replaced = %v, want %v", got.Replaced, want)
	}
	if !slices.Equal(got.Startups, []string{"a", "b"}) {
		t.Fatalf("replacement changed startup order: %v", got.Startups)
	}
}
