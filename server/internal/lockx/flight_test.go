package lockx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/logx"
)

type testSpawner struct {
	wg    sync.WaitGroup
	calls atomic.Int64
}

func (s *testSpawner) Spawn(ctx context.Context, _ string, fn func(context.Context)) error {
	s.calls.Add(1)
	s.wg.Go(func() { fn(ctx) })
	return nil
}

func newTestSpawner(t *testing.T) *testSpawner {
	t.Helper()
	s := new(testSpawner)
	t.Cleanup(s.wg.Wait)
	return s
}

func gate(t *testing.T) (<-chan struct{}, func()) {
	t.Helper()
	ch := make(chan struct{})
	var once sync.Once
	closeGate := func() { once.Do(func() { close(ch) }) }
	t.Cleanup(closeGate)
	return ch, closeGate
}

type flightResult struct {
	value  int
	err    error
	shared bool
}

func TestFlightCoalescesAndRemovesCompletedCalls(t *testing.T) {
	s := newTestSpawner(t)
	f := NewFlight[string, int](s, "provider-render")
	start := make(chan struct{})
	workGate, finish := gate(t)
	var runs atomic.Int64
	fn := func(context.Context) (int, error) {
		if runs.Add(1) == 1 {
			close(start)
		}
		<-workGate
		return 42, nil
	}
	results := make(chan flightResult, 50)
	for range 50 {
		go func() {
			value, err, shared := f.Do(context.Background(), "one", fn)
			results <- flightResult{value, err, shared}
		}()
	}
	await(t, start)
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); c := f.calls["one"]; return c != nil && c.joiners == 49 })
	finish()
	for range 50 {
		r := await(t, results)
		if r.value != 42 || r.err != nil || !r.shared {
			t.Fatalf("coalesced result: %#v", r)
		}
	}
	if runs.Load() != 1 || s.calls.Load() != 1 {
		t.Fatalf("ran more than once: %d", runs.Load())
	}
	f.mu.Lock()
	count := len(f.calls)
	f.mu.Unlock()
	if count != 0 {
		t.Fatalf("completed flight retained: %d", count)
	}
	value, err, shared := f.Do(context.Background(), "one", func(context.Context) (int, error) { return 99, nil })
	if value != 99 || err != nil || shared || s.calls.Load() != 2 {
		t.Fatalf("later call reused completed work: %d %v %v", value, err, shared)
	}
}

func TestFlightIndependentKeys(t *testing.T) {
	s := newTestSpawner(t)
	f := NewFlight[int, int](s, "per-key")
	workGate, finish := gate(t)
	started := make(chan struct{})
	first := make(chan flightResult, 1)
	go func() {
		value, err, shared := f.Do(context.Background(), 1, func(context.Context) (int, error) { close(started); <-workGate; return 1, nil })
		first <- flightResult{value, err, shared}
	}()
	await(t, started)
	second := make(chan flightResult, 1)
	go func() {
		value, err, shared := f.Do(context.Background(), 2, func(context.Context) (int, error) { return 2, nil })
		second <- flightResult{value, err, shared}
	}()
	if r := await(t, second); r.value != 2 || r.err != nil || r.shared {
		t.Fatalf("other key blocked or shared: %#v", r)
	}
	finish()
	if r := await(t, first); r.value != 1 || r.err != nil {
		t.Fatalf("first key: %#v", r)
	}
}

type flightValueKey struct{}

func TestFlightFirstCallerCancellationDoesNotCancelWork(t *testing.T) {
	s := newTestSpawner(t)
	f := NewFlight[string, int](s, "detached")
	base := context.WithValue(context.Background(), flightValueKey{}, "kept")
	ctx, cancel := context.WithTimeout(base, time.Hour)
	defer cancel()
	workGate, finish := gate(t)
	started := make(chan struct{})
	first := make(chan flightResult, 1)
	fn := func(ctx context.Context) (int, error) {
		close(started)
		<-workGate
		_, hasDeadline := ctx.Deadline()
		if ctx.Value(flightValueKey{}) != "kept" || ctx.Err() != nil || ctx.Done() != nil || hasDeadline {
			return 0, errors.New("worker lost context values or inherited request cancellation")
		}
		return 7, nil
	}
	go func() { value, err, shared := f.Do(ctx, "key", fn); first <- flightResult{value, err, shared} }()
	await(t, started)
	cancel()
	if r := await(t, first); !errors.Is(r.err, context.Canceled) {
		t.Fatalf("first caller cannot cancel: %#v", r)
	}
	later := make(chan flightResult, 1)
	go func() {
		value, err, shared := f.Do(context.Background(), "key", func(context.Context) (int, error) { return -1, errors.New("unexpected replacement worker") })
		later <- flightResult{value, err, shared}
	}()
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); c := f.calls["key"]; return c != nil && c.joiners == 1 })
	finish()
	r := await(t, later)
	if r.value != 7 || r.err != nil || !r.shared || s.calls.Load() != 1 {
		t.Fatalf("cancelled request stopped shared work: %#v", r)
	}
}

func TestFlightAllWaitersCancelWithoutLeaking(t *testing.T) {
	baseline := runtime.NumGoroutine()
	s := newTestSpawner(t)
	f := NewFlight[int, int](s, "cancelled-waiters")
	workGate, finish := gate(t)
	started := make(chan struct{})
	var workStarts sync.Once
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	results := make(chan error, 100)
	for range 100 {
		go func() {
			_, err, _ := f.Do(ctx, 1, func(context.Context) (int, error) {
				workStarts.Do(func() { close(started) })
				<-workGate
				return 1, nil
			})
			results <- err
		}()
	}
	await(t, started)
	eventually(t, func() bool { f.mu.Lock(); defer f.mu.Unlock(); c := f.calls[1]; return c != nil && c.joiners == 99 })
	cancel()
	for range 100 {
		if err := await(t, results); !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	}
	f.mu.Lock()
	count := len(f.calls)
	f.mu.Unlock()
	if count != 1 {
		t.Fatal("all cancelled waiters discarded running work")
	}
	finish()
	s.wg.Wait()
	f.mu.Lock()
	count = len(f.calls)
	f.mu.Unlock()
	if count != 0 {
		t.Fatal("finished work retained")
	}
	eventually(t, func() bool { return runtime.NumGoroutine() <= baseline+2 })
}

type rejectingSpawner struct {
	entered chan struct{}
	resume  <-chan struct{}
	cause   error
}

func (s *rejectingSpawner) Spawn(context.Context, string, func(context.Context)) error {
	close(s.entered)
	<-s.resume
	return s.cause
}

func TestFlightSpawnerRejectionWakesAllWaiters(t *testing.T) {
	resume, release := gate(t)
	cause := errors.New("runtime stopping")
	s := &rejectingSpawner{entered: make(chan struct{}), resume: resume, cause: cause}
	f := NewFlight[string, int](s, "rejected")
	results := make(chan error, 20)
	fn := func(context.Context) (int, error) { t.Error("rejected work started"); return 0, nil }
	go func() { _, err, _ := f.Do(context.Background(), "same", fn); results <- err }()
	await(t, s.entered)
	for range 19 {
		go func() { _, err, _ := f.Do(context.Background(), "same", fn); results <- err }()
	}
	eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		c := f.calls["same"]
		return c != nil && c.joiners == 19
	})
	release()
	for range 20 {
		if err := await(t, results); !errors.Is(err, cause) {
			t.Fatalf("lost rejection error: %v", err)
		}
	}
	f.mu.Lock()
	count := len(f.calls)
	f.mu.Unlock()
	if count != 0 {
		t.Fatal("rejected flight retained")
	}
}

func TestFlightFailureProcess(t *testing.T) {
	if os.Getenv("CONFIGFLOW_LOCKX_FLIGHT_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestFlightFailureProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_LOCKX_FLIGHT_HELPER=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("flight child: %v\n%s", err, out)
		}
		return
	}
	var output strings.Builder
	if _, err := logx.Setup(logx.Config{Level: logx.Error, Console: &output}); err != nil {
		t.Fatal(err)
	}
	s := newTestSpawner(t)
	f := NewFlight[string, int](s, "panic-test")
	workGate, finish := gate(t)
	started := make(chan struct{})
	results := make(chan error, 10)
	go func() {
		_, err, _ := f.Do(context.Background(), "panic", func(context.Context) (int, error) { close(started); <-workGate; panic("synthetic-private-payload") })
		results <- err
	}()
	await(t, started)
	for range 9 {
		go func() {
			_, err, _ := f.Do(context.Background(), "panic", func(context.Context) (int, error) { return -1, nil })
			results <- err
		}()
	}
	eventually(t, func() bool {
		f.mu.Lock()
		defer f.mu.Unlock()
		c := f.calls["panic"]
		return c != nil && c.joiners == 9
	})
	finish()
	for range 10 {
		if err := await(t, results); !errors.Is(err, ErrPanic) {
			t.Fatalf("panic waiter: %v", err)
		}
	}
	cause := errors.New("ordinary worker failure")
	if _, err, _ := f.Do(context.Background(), "error", func(context.Context) (int, error) { return 0, cause }); !errors.Is(err, cause) {
		t.Fatal(err)
	}
	s.wg.Wait()
	if strings.Count(output.String(), " - backend.lockx - ERROR - ") != 1 || strings.Contains(output.String(), "synthetic-private-payload") {
		t.Fatalf("panic logging duplicated or leaked payload: %s", output.String())
	}
	if value, err, shared := f.Do(context.Background(), "panic", func(context.Context) (int, error) { return 123, nil }); value != 123 || err != nil || shared {
		t.Fatalf("panic prevented reuse: %d %v %v", value, err, shared)
	}
}

func TestFlightZeroValue(t *testing.T) {
	var f Flight[string, int]
	if value, err, shared := f.Do(context.Background(), "key", nil); value != 0 || err == nil || err.Error() != msgMissingSpawner || shared {
		t.Fatalf("unconfigured Flight: %d %v %v", value, err, shared)
	}
}

type shutdownSpawner struct {
	testSpawner
	cancel context.CancelFunc
}

func (s *shutdownSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	ctx, s.cancel = context.WithCancel(ctx)
	return s.testSpawner.Spawn(ctx, name, fn)
}

func TestFlightReceivesSpawnerShutdownContext(t *testing.T) {
	s := &shutdownSpawner{}
	defer s.wg.Wait()
	f := NewFlight[string, int](s, "shutdown")
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		_, err, _ := f.Do(context.Background(), "key", func(ctx context.Context) (int, error) { close(started); <-ctx.Done(); return 0, ctx.Err() })
		result <- err
	}()
	await(t, started)
	s.cancel()
	if err := await(t, result); !errors.Is(err, context.Canceled) {
		t.Fatalf("shutdown context ignored: %v", err)
	}
}
