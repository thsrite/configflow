package webdav

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
)

type workerClock struct {
	*lifecycletest.Clock
	created chan time.Duration
}

func (c *workerClock) NewTimer(d time.Duration) lifecycle.Timer {
	timer := c.Clock.NewTimer(d)
	c.created <- d
	return timer
}
func awaitDebounce(t *testing.T, c *workerClock, d time.Duration) {
	t.Helper()
	for {
		select {
		case got := <-c.created:
			if got == d {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatal("worker timer not created")
		}
	}
}
func workerRuntime(t *testing.T, w *Worker, clock lifecycle.Clock) *lifecycle.Runtime {
	t.Helper()
	reg := lifecycle.NewRegistry()
	r := lifecycle.NewRuntime(lifecycle.Options{Registry: reg, Clock: clock, Grace: 20 * time.Millisecond})
	reg.RegisterStartup(workerStartup(w, r, reg.RegisterShutdown))
	return r
}
func receiveUpload(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case text := <-ch:
		return text
	case <-time.After(5 * time.Second):
		t.Fatal("upload not completed")
		return ""
	}
}

func TestWorkerStartupRestartsClockAndExplicitCloseStaysTerminal(t *testing.T) {
	clock := &workerClock{Clock: lifecycletest.NewClock(time.Now()), created: make(chan time.Duration, 32)}
	uploads := make(chan string, 4)
	w := newAutomaticWorker(func(_ context.Context, s Snapshot) error { uploads <- string(s.Document); return nil })
	runner := workerRuntime(t, w, clock)
	defer runner.Shutdown(context.Background())
	source := &memorySource{}
	for generation := range 2 {
		runner.Start(context.Background())
		want := fmt.Sprintf(`{"generation":%d}`, generation)
		source.set(want, true)
		if !w.Schedule(source) {
			t.Fatal("startup did not reactivate owner")
		}
		awaitDebounce(t, clock, time.Second)
		clock.Advance(999 * time.Millisecond)
		select {
		case <-uploads:
			t.Fatal("early debounce")
		default:
		}
		clock.Advance(time.Millisecond)
		if got := receiveUpload(t, uploads); got != want {
			t.Fatal(got)
		}
		waitWorker(t, w)
		runner.Shutdown(context.Background())
	}
	if err := w.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if w.activate(workerOptions{}) != nil || w.Schedule(source) {
		t.Fatal("terminal Close was reopened")
	}
}

func TestWorkerUploadGateSurvivesOldRuntimeAndLateStop(t *testing.T) {
	first := &memorySource{}
	first.set(`{"generation":1}`, true)
	second := &memorySource{}
	second.set(`{"generation":2}`, true)
	entered := make(chan string, 4)
	release := make(chan struct{})
	var once sync.Once
	finish := func() { once.Do(func() { close(release) }) }
	defer finish()
	var active, peak atomic.Int32
	w := newWorker(0, func(_ context.Context, s Snapshot) error {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		entered <- string(s.Document)
		if string(s.Document) == `{"generation":1}` {
			<-release
		} // deliberately ignores old cancellation
		return nil
	}, workerOptions{})
	old := workerRuntime(t, w, nil)
	old.Start(context.Background())
	if !w.Schedule(first) {
		t.Fatal("initial schedule refused")
	}
	if receiveUpload(t, entered) != `{"generation":1}` {
		t.Fatal("wrong first upload")
	}
	w.mu.Lock()
	oldGeneration := w.generation
	w.mu.Unlock()
	old.Shutdown(context.Background())
	next := workerRuntime(t, w, nil)
	next.Start(context.Background())
	defer next.Shutdown(context.Background())
	if !w.Schedule(second) {
		t.Fatal("new generation refused")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	_ = w.stopGeneration(cancelled, oldGeneration) // stale hook must not stop current generation
	select {
	case <-entered:
		t.Fatal("uploads overlapped")
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	if got := receiveUpload(t, entered); got != `{"generation":2}` {
		t.Fatal(got)
	}
	waitWorker(t, w)
	if peak.Load() != 1 {
		t.Fatal("cross-generation overlap", peak.Load())
	}
}

func TestWorkerIdleIsOwnerScoped(t *testing.T) {
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	release := make(chan struct{})
	defer close(release)
	if err := runner.Spawn(context.Background(), "unrelated", func(ctx context.Context) {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}); err != nil {
		t.Fatal(err)
	}
	w := newWorker(0, func(context.Context, Snapshot) error { return nil }, workerOptions{Spawner: runner})
	source := &memorySource{}
	source.set(`{}`, true)
	if !w.Schedule(source) {
		t.Fatal("schedule refused")
	}
	waitWorker(t, w)
	wait, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if runner.WaitIdle(wait) == nil {
		t.Fatal("unrelated work unexpectedly idle")
	}
}

type refusingWorkerSpawner struct{}

func (refusingWorkerSpawner) Spawn(context.Context, string, func(context.Context)) error {
	return lifecycle.ErrStopping
}

func TestWorkerRefusedSpawnIsIdleAndCanActivateAgain(t *testing.T) {
	source := &memorySource{}
	source.set(`{}`, true)
	w := newWorker(0, func(context.Context, Snapshot) error { panic("synthetic") }, workerOptions{Spawner: refusingWorkerSpawner{}})
	if w.Schedule(source) {
		t.Fatal("refused schedule accepted")
	}
	waitWorker(t, w)
	runner := workerRuntime(t, w, nil)
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	if !w.Schedule(source) {
		t.Fatal("new activation refused")
	}
	waitWorker(t, w)
	wait, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := runner.WaitIdle(wait); err != nil {
		t.Fatal(err)
	}
	w.mu.Lock()
	active, held := w.workers, len(w.gate)
	w.mu.Unlock()
	if active != 0 || held != 0 {
		t.Fatal("panic retained worker or upload gate", active, held)
	}
}

func TestWorkerShutdownCancellationReachesHTTP(t *testing.T) {
	entered, cancelled, force := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var startOnce, cancelOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		startOnce.Do(func() { close(entered) })
		select {
		case <-req.Context().Done():
			cancelOnce.Do(func() { close(cancelled) })
		case <-force:
		}
	}))
	defer server.Close()
	defer close(force)
	service := NewService(server.Client()) // no test override of the production I/O timeout
	source := &memorySource{value: Snapshot{Document: []byte(`{}`), Settings: Settings{
		URL: server.URL, Username: "fixture-user", Password: "fixture-secret", Path: "/backups/", Auto: true,
	}}}
	w := newWorker(0, func(ctx context.Context, snapshot Snapshot) error {
		_, err := service.Upload(ctx, snapshot.Document, snapshot.Settings, true)
		return err
	}, workerOptions{})
	runner := workerRuntime(t, w, nil)
	runner.Start(context.Background())
	defer runner.Shutdown(context.Background())
	if !w.Schedule(source) {
		t.Fatal("schedule refused")
	}
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("HTTP request not started")
	}
	runner.Shutdown(context.Background())
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown cancellation was stripped before HTTP")
	}
	waitWorker(t, w)
}
