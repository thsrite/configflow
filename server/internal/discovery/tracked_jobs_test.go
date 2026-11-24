package discovery

import (
	"context"
	"errors"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

type refusedJobSpawner struct{}

func (refusedJobSpawner) Spawn(context.Context, string, func(context.Context)) error {
	return lifecycle.ErrStopping
}

func TestAsyncJobUsesRuntimeWithoutRetainingRequestValues(t *testing.T) {
	type key struct{}
	svc, ctx := jobFixture(t)
	request, cancel := context.WithCancel(context.WithValue(repo.WithScope(ctx), key{}, "request-only"))
	cancel()
	original := repo.ScopeOf(request)
	clock := lifecycletest.NewClock(time.Now())
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Clock: clock})
	runner.Start(ctx)
	forceExit := make(chan struct{})
	defer func() { close(forceExit); runner.Shutdown(context.Background()) }()
	entered, exited := make(chan struct{}), make(chan struct{})
	job, _, err := svc.startJobOn(request, "default", "probe", false, func(worker context.Context, _ func(int, int)) (outcome, error) {
		if worker.Err() != nil || worker.Value(key{}) != nil || repo.ScopeOf(worker) == original || lifecycle.ClockFrom(worker) != clock {
			t.Error("request retained or runtime lost")
		}
		close(entered)
		select {
		case <-worker.Done():
		case <-forceExit:
		}
		close(exited)
		return outcome{}, worker.Err()
	}, runner)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not start")
	}
	cancelled, stopWait := context.WithCancel(ctx)
	stopWait()
	if err := runner.WaitIdle(cancelled); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	select {
	case <-exited:
		t.Fatal("wait cancellation stopped work")
	default:
	}
	stopped := make(chan struct{})
	go func() { defer close(stopped); runner.Shutdown(context.Background()) }()
	deadline := time.Now().Add(5 * time.Second)
	for clock.Pending() != 2 {
		if time.Now().After(deadline) {
			t.Fatal("not draining")
		}
		time.Sleep(time.Millisecond)
	}
	clock.Advance(6 * time.Second)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("worker leaked after cancellation")
	}
	select {
	case <-exited:
	default:
		t.Fatal("I/O did not receive cancellation")
	}
	if saved := svc.GetJob(job.ID, "default"); saved.Status != "failed" || saved.Message != msgFailed || saved.FinishedAt == nil {
		t.Fatal(saved)
	}
	if lockx.Violations() != 0 {
		t.Fatal("lock order violation")
	}
}

func TestAsyncJobRefusalAndPanicReleaseAdmission(t *testing.T) {
	svc, ctx := jobFixture(t)
	if _, _, err := svc.startJobOn(ctx, "default", "probe", false, func(context.Context, func(int, int)) (outcome, error) {
		t.Error("refused work ran")
		return outcome{}, nil
	}, refusedJobSpawner{}); !errors.Is(err, lifecycle.ErrStopping) {
		t.Fatal(err)
	}
	if svc.runningJob("") != nil {
		t.Fatal("refused job left running")
	}
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(ctx)
	defer runner.Shutdown(context.Background())
	job, _, err := svc.startJobOn(ctx, "default", "probe", false, func(context.Context, func(int, int)) (outcome, error) { panic("synthetic secret must not be logged") }, runner)
	if err != nil {
		t.Fatal(err)
	}
	wait, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := runner.WaitIdle(wait); err != nil {
		t.Fatal(err)
	}
	if saved := svc.GetJob(job.ID, "default"); saved.Status != "failed" || saved.Message != msgFailed {
		t.Fatal(saved)
	}
	_, release, ok := svc.mutex().TryLock(ctx)
	if !ok {
		t.Fatal("panic leaked admission lock")
	}
	release()
}
