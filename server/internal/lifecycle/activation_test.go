package lifecycle

import (
	"context"
	"testing"
	"time"
)

func TestStartupCanRegisterPeriodicBeforeActivation(t *testing.T) {
	reg := NewRegistry()
	r := NewRuntime(Options{Registry: reg})
	activated := false
	reg.RegisterStartup(Startup{Name: "registration", Run: func(context.Context) error {
		reg.RegisterPeriodic(Periodic{Name: "late", FirstDelay: time.Hour, OnStart: func(context.Context) { activated = true }, Round: func(context.Context) error { return nil }})
		r.startEligible() // same path as global registration during Startup
		return nil
	}})
	r.Start(context.Background())
	if !activated {
		t.Fatal("startup registration did not activate")
	}
	r.Shutdown(context.Background())
}

func TestActivationPanicDoesNotStrandRunner(t *testing.T) {
	reg := NewRegistry()
	ran := make(chan struct{})
	reg.RegisterPeriodic(Periodic{Name: "activation-panic", OnStart: func(context.Context) { panic("synthetic") }, Round: func(context.Context) error { close(ran); return nil }})
	runner := NewRuntime(Options{Registry: reg})
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	select {
	case <-ran:
	case <-time.After(time.Second):
		t.Fatal("activation panic stranded the driver")
	}
	if err := runner.WaitIdle(context.Background()); err != nil {
		t.Fatal(err)
	}
}
