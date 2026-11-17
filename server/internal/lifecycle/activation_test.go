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
