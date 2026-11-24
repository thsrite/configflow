package health

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"configflow/internal/ctxx"
)

type lifetimeRunner func(context.Context, Command) (Result, error)

func (f lifetimeRunner) Run(ctx context.Context, command Command) (Result, error) {
	return f(ctx, command)
}

func TestDNSOwnerKeepsWorkParentForIO(t *testing.T) {
	entered, canceled := make(chan struct{}), make(chan error, 1)
	runner := lifetimeRunner(func(ctx context.Context, _ Command) (Result, error) {
		if err := ctx.Err(); err != nil {
			t.Error("request disconnect reached worker", err)
		}
		close(entered)
		<-ctx.Done()
		canceled <- ctx.Err()
		return Result{}, ctx.Err()
	})
	t.Cleanup(SetProbeEnv(testEnv(runner, nil)))
	request, disconnect := context.WithCancel(context.Background())
	job, shutdown := context.WithCancel(context.Background())
	defer shutdown()
	bound := ctxx.WithParent(request, job)
	disconnect()
	done := make(chan error, 1)
	go func() { _, err := CheckDNSSettingsJSON(bound, nil, json.RawMessage(`{"mode":"system"}`)); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("DNS worker did not enter")
	}
	select {
	case err := <-done:
		t.Fatal("request disconnect aborted DNS owner", err)
	default:
	}
	shutdown()
	select {
	case err := <-canceled:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not reach DNS I/O")
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("canceled worker succeeded")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("DNS owner leaked")
	}
}
