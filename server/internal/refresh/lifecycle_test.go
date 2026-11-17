package refresh

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/substore"
)

func TestRoundJoinsThreeWorkersAndStopsAdmission(t *testing.T) {
	f := setup(t, jsonx.Int(60))
	f.updateShared(func(shared *jsonx.Object) {
		shared.Set("rule_library", []any{})
		subs := []any{}
		for i := range 6 {
			subs = append(subs, jsonx.Obj("id", fmt.Sprint(i), "url", fmt.Sprintf("https://example.com/%d", i), "interval", jsonx.Int(60)))
		}
		shared.Set("subscriptions", subs)
	})
	entered, release := make(chan struct{}, 6), make(chan struct{})
	var active, peak atomic.Int32
	f.env.FetchSubscription = func(ctx context.Context, _, _ string, _ substore.FetchOptions) (*substore.SubscriptionResult, error) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- struct{}{}
		<-release
		return nil, errors.New("synthetic download failure")
	}
	clock := lifecycletest.NewClock(f.clock.Now())
	reg := lifecycle.NewRegistry()
	reg.RegisterPeriodic(periodic())
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock, Mode: lifecycle.ModeManual})
	runner.Start(f.ctx)
	done := make(chan error, 1)
	go func() { done <- runner.RunRound(f.ctx, "scheduled-refresh") }()
	for range 3 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			t.Fatal("worker did not start")
		}
	}
	if peak.Load() != Workers {
		t.Fatal("wrong worker limit", peak.Load())
	}
	select {
	case err := <-done:
		t.Fatal("round returned before joining", err)
	default:
	}
	rows, err := SubscriptionRows(f.ctx, jsonx.NewObject())
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Value("running") != true {
			t.Fatal("claimed key absent from rows", row)
		}
	}
	// Shutdown closes admission immediately; current downloads can still finish.
	stopped := make(chan struct{})
	go func() { defer close(stopped); runner.Shutdown(context.Background()) }()
	awaitRefresh(t, func() bool { return clock.Pending() == 2 })
	close(release)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("round leaked")
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown leaked")
	}
	if peak.Load() > Workers || len(entered) != 0 {
		t.Fatal("work admitted after stopping")
	}
	for i := range 6 {
		if IsRunning(SubscriptionKey(fmt.Sprint(i))) {
			t.Fatal("held key leaked")
		}
	}
}

func TestRunNowSurvivesRequestAndReceivesShutdownCancel(t *testing.T) {
	f := setup(t, jsonx.Int(60))
	clock := lifecycletest.NewClock(f.clock.Now())
	if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry(), Clock: clock}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		lifecycle.Stop(time.Second)
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})
	entered, cancelled := make(chan struct{}), make(chan struct{})
	f.env.FetchSubscription = func(ctx context.Context, _, _ string, _ substore.FetchOptions) (*substore.SubscriptionResult, error) {
		close(entered)
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}
	request, cancel := context.WithCancel(f.ctx)
	cancel() // A disconnected request must still acquire the key and start work.
	key, err := RunNow(request, "subscription", "s")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("download not started")
	}
	select {
	case <-cancelled:
		t.Fatal("request cancelled download")
	default:
	}
	stopped := make(chan struct{})
	go func() { defer close(stopped); lifecycle.Stop(10 * time.Second) }()
	awaitRefresh(t, func() bool { return clock.Pending() == 2 })
	clock.Advance(6 * time.Second)
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown cancellation did not reach I/O")
	}
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("tracked job not joined")
	}
	if IsRunning(key) {
		t.Fatal("key not released after cancellation")
	}
	// Rejected admission must release the key too.
	if _, err := RunNow(f.ctx, "subscription", "s"); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatal(err)
	}
	if IsRunning(key) {
		t.Fatal("key leaked after rejected spawn")
	}
}
