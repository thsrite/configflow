package lockx

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/logx"
)

func TestMain(m *testing.M) {
	code := m.Run()
	if os.Getenv("CONFIGFLOW_LOCKX_ORDER_HELPER") != "1" && Violations() != 0 {
		logger.Errorf("unexpected lock order violations: %d", Violations())
		code = 1
	}
	os.Exit(code)
}

func lockMutex(t *testing.T, m *Mutex, ctx context.Context) (context.Context, func()) {
	t.Helper()
	held, release, err := m.Lock(ctx, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return held, release
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not finish")
		var zero T
		return zero
	}
}

func eventually(t *testing.T, ready func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ready() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		runtime.Gosched()
	}
}

func TestLockOrderAndReleasedContexts(t *testing.T) {
	ctx := context.Background()
	j := NewMutex(LevelJob, "job")
	jh, jr, ok := j.TryLock(ctx)
	if !ok {
		t.Fatal("job unavailable")
	}
	defer jr()
	dh, dr := lockMutex(t, NewMutex(LevelDevice, "device"), jh)
	defer dr()
	rh, rr := lockMutex(t, NewMutex(LevelRepository, "repository"), dh)
	inner := NewMutex(LevelJob, "commit-guard")
	_, release, ok := inner.TryLock(rh)
	if !ok {
		t.Fatal("try under repository failed")
	}
	release()
	release()
	if _, _, ok := j.TryLock(rh); ok {
		t.Fatal("held job acquired twice")
	}
	rr()
	rr()
	// Released records in an old context must not cause a false positive.
	dr()
	_, release = lockMutex(t, NewMutex(LevelDevice, "after-release"), rh)
	release()
	if Violations() != 0 {
		t.Fatalf("valid acquisition reported violations: %d", Violations())
	}
}

func TestOrderViolationProcess(t *testing.T) {
	if os.Getenv("CONFIGFLOW_LOCKX_ORDER_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestOrderViolationProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_LOCKX_ORDER_HELPER=1")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("checker child: %v\n%s", err, out)
		}
		return
	}
	var output strings.Builder
	if _, err := logx.Setup(logx.Config{Level: logx.Error, Console: &output}); err != nil {
		t.Fatal(err)
	}
	rh, rr := lockMutex(t, NewMutex(LevelRepository, "repository"), context.Background())
	defer rr()
	for range 2 {
		_, release := lockMutex(t, NewMutex(LevelDevice, "device"), rh)
		release()
	}
	_, release := lockMutex(t, NewMutex(LevelRepository, "other-repository"), rh)
	release()
	if Violations() != 3 {
		t.Fatalf("want 3 counted acquisitions, got %d", Violations())
	}
	if strings.Count(output.String(), " - backend.lockx - ERROR - ") != 2 {
		t.Fatalf("name pairs not logged once: %s", output.String())
	}
}

func TestMutexTryOnlyJob(t *testing.T) {
	for _, limit := range []time.Duration{0, -time.Second, time.Hour} {
		t.Run(limit.String(), func(t *testing.T) {
			m := NewMutex(LevelJob, "job")
			_, release := lockMutex(t, m, context.Background())
			defer release()
			if _, _, ok := m.TryLock(context.Background()); ok {
				t.Fatal("busy TryLock succeeded")
			}
			done := make(chan error, 1)
			go func() {
				_, unlock, err := m.Lock(context.Background(), limit)
				if unlock != nil {
					unlock()
				}
				done <- err
			}()
			if err := await(t, done); !errors.Is(err, ErrBusy) {
				t.Fatalf("job waited or wrong error: %v", err)
			}
			release()
			release()
			_, unlock, ok := m.TryLock(context.Background())
			if !ok {
				t.Fatal("released job remained busy")
			}
			unlock()
		})
	}
}

func TestMutexWaitLimits(t *testing.T) {
	tests := []struct {
		name        string
		limit       time.Duration
		makeContext func() (context.Context, context.CancelFunc)
		want        error
	}{
		{"limit", 15 * time.Millisecond, func() (context.Context, context.CancelFunc) { return context.WithCancel(context.Background()) }, ErrTimeout},
		{"zero-until-deadline", 0, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 15*time.Millisecond)
		}, context.DeadlineExceeded},
		{"negative-until-deadline", -time.Second, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 15*time.Millisecond)
		}, context.DeadlineExceeded},
		{"context-before-limit", time.Hour, func() (context.Context, context.CancelFunc) {
			return context.WithTimeout(context.Background(), 15*time.Millisecond)
		}, context.DeadlineExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewMutex(LevelDevice, "wait")
			_, release := lockMutex(t, m, context.Background())
			defer release()
			ctx, cancel := tt.makeContext()
			defer cancel()
			_, unlock, err := m.Lock(ctx, tt.limit)
			if !errors.Is(err, tt.want) || unlock != nil {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
			release()
			_, unlock = lockMutex(t, m, context.Background())
			unlock()
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	m := NewMutex(LevelDevice, "cancelled")
	if _, _, ok := m.TryLock(ctx); ok {
		t.Fatal("cancelled try succeeded")
	}
	if _, release, err := m.Lock(ctx, 0); !errors.Is(err, context.Canceled) || release != nil {
		t.Fatalf("cancelled wait: %v", err)
	}
	// A waiter is awakened by release, with caller values intact.
	_, release := lockMutex(t, m, context.Background())
	done := make(chan error, 1)
	go func() {
		_, unlock, err := m.Lock(context.Background(), time.Second)
		if unlock != nil {
			unlock()
		}
		done <- err
	}()
	release()
	if err := await(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestMutexConcurrentStress(t *testing.T) {
	m := NewMutex(LevelDevice, "stress")
	value := 0
	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 200 {
				_, release, err := m.Lock(context.Background(), 5*time.Second)
				if err != nil {
					t.Error(err)
					return
				}
				value++
				release()
				release()
			}
		})
	}
	wg.Wait()
	if value != 10000 {
		t.Fatalf("lost updates: %d", value)
	}
}

func TestKeyedCleanupAndIndependentKeys(t *testing.T) {
	k := NewKeyed[int](LevelDevice, "keyed")
	for key := range 10000 {
		if k.Held(key) {
			t.Fatal("absent key held")
		}
		_, release, ok := k.TryLock(context.Background(), key)
		if !ok || !k.Held(key) {
			t.Fatal("key not held")
		}
		if _, _, ok := k.TryLock(context.Background(), key); ok {
			t.Fatal("same key acquired twice")
		}
		release()
		release()
		if k.Held(key) {
			t.Fatal("released key held")
		}
	}
	k.mu.Lock()
	count := len(k.entries)
	k.mu.Unlock()
	if count != 0 {
		t.Fatalf("404-style keys retained: %d", count)
	}
	_, release, ok := k.TryLock(context.Background(), 1)
	if !ok {
		t.Fatal("first key unavailable")
	}
	defer release()
	_, other, err := k.Lock(context.Background(), 2, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	other()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, _, err := k.Lock(ctx, 1, 0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("waiter cancellation: %v", err)
	}
	release()
	k.mu.Lock()
	count = len(k.entries)
	k.mu.Unlock()
	if count != 0 {
		t.Fatalf("cancelled waiter leaked entry: %d", count)
	}
}

func TestKeyedConcurrentStress(t *testing.T) {
	k := NewKeyed[int](LevelDevice, "key-stress")
	counts := [8]int{}
	var wg sync.WaitGroup
	for worker := range 50 {
		wg.Go(func() {
			for i := range 200 {
				key := (worker + i) % len(counts)
				_, release, err := k.Lock(context.Background(), key, 5*time.Second)
				if err != nil {
					t.Error(err)
					return
				}
				counts[key]++
				release()
			}
		})
	}
	wg.Wait()
	total := 0
	for _, count := range counts {
		total += count
	}
	if total != 10000 {
		t.Fatalf("lost keyed writes: %d", total)
	}
	k.mu.Lock()
	count := len(k.entries)
	k.mu.Unlock()
	if count != 0 {
		t.Fatalf("stress retained entries: %d", count)
	}
}

func TestRWMutexSharedAndExclusive(t *testing.T) {
	m := NewRWMutex(LevelDevice, "leases")
	_, r1, ok := m.TryRLock(context.Background())
	if !ok {
		t.Fatal("first reader unavailable")
	}
	defer r1()
	_, r2, err := m.RLock(context.Background(), 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r2()
	if _, _, ok := m.TryLock(context.Background()); ok {
		t.Fatal("writer acquired with readers")
	}
	if _, _, err := m.Lock(context.Background(), 10*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("writer timeout: %v", err)
	}
	r1()
	r1()
	r2()
	_, w, err := m.Lock(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer w()
	if _, _, ok := m.TryRLock(context.Background()); ok {
		t.Fatal("reader acquired with writer")
	}
	if _, _, err := m.RLock(context.Background(), 10*time.Millisecond); !errors.Is(err, ErrTimeout) {
		t.Fatalf("reader timeout: %v", err)
	}
	w()
	w()
	_, r3, ok := m.TryRLock(context.Background())
	if !ok {
		t.Fatal("released writer blocked reader")
	}
	r3()
}

func TestRWMutexWriterPriorityAndCancellation(t *testing.T) {
	m := NewRWMutex(LevelDevice, "priority")
	_, reader, ok := m.TryRLock(context.Background())
	if !ok {
		t.Fatal("reader unavailable")
	}
	defer reader()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, release, err := m.Lock(ctx, 0)
		if release != nil {
			release()
		}
		done <- err
	}()
	eventually(t, func() bool { m.mu.Lock(); defer m.mu.Unlock(); return m.waitingWriters == 1 })
	if _, _, ok := m.TryRLock(context.Background()); ok {
		t.Fatal("new reader bypassed waiting writer")
	}
	cancel()
	if err := await(t, done); !errors.Is(err, context.Canceled) {
		t.Fatalf("writer cancellation: %v", err)
	}
	_, r2, ok := m.TryRLock(context.Background())
	if !ok {
		t.Fatal("cancelled writer still blocked new readers")
	}
	r2()
	reader()
	_, writer, err := m.Lock(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	writer()
}

func TestRWMutexJobTryOnly(t *testing.T) {
	m := NewRWMutex(LevelJob, "job-leases")
	_, release, ok := m.TryLock(context.Background())
	if !ok {
		t.Fatal("writer unavailable")
	}
	defer release()
	for _, lock := range []func(context.Context, time.Duration) (context.Context, func(), error){m.Lock, m.RLock} {
		done := make(chan error, 1)
		go func() {
			_, release, err := lock(context.Background(), time.Hour)
			if release != nil {
				release()
			}
			done <- err
		}()
		if err := await(t, done); !errors.Is(err, ErrBusy) {
			t.Fatalf("job lease waited or wrong error: %v", err)
		}
	}
}

func TestRWMutexConcurrentStress(t *testing.T) {
	m := NewRWMutex(LevelDevice, "rw-stress")
	var insideReaders, insideWriter atomic.Int64
	value := 0
	var wg sync.WaitGroup
	for worker := range 40 {
		wg.Go(func() {
			for range 100 {
				if worker%4 == 0 {
					_, release, err := m.Lock(context.Background(), 5*time.Second)
					if err != nil {
						t.Error(err)
						return
					}
					if insideWriter.Add(1) != 1 || insideReaders.Load() != 0 {
						t.Error("writer overlap")
					}
					value++
					insideWriter.Add(-1)
					release()
				} else {
					_, release, err := m.RLock(context.Background(), 5*time.Second)
					if err != nil {
						t.Error(err)
						return
					}
					insideReaders.Add(1)
					if insideWriter.Load() != 0 {
						t.Error("reader overlap")
					}
					_ = value
					insideReaders.Add(-1)
					release()
				}
			}
		})
	}
	wg.Wait()
	if value != 1000 {
		t.Fatalf("lost writes: %d", value)
	}
}

func TestLocksLeaveFilesUntouched(t *testing.T) {
	dir := t.TempDir()
	leftover := filepath.Join(dir, "resource.lock")
	old := []byte("python-leftover\n")
	if err := os.WriteFile(leftover, old, 0o600); err != nil {
		t.Fatal(err)
	}
	m := NewMutex(LevelDevice, filepath.Join(dir, "new.lock"))
	_, release := lockMutex(t, m, context.Background())
	release()
	k := NewKeyed[string](LevelJob, leftover)
	_, unlock, ok := k.TryLock(context.Background(), "missing-resource")
	if !ok {
		t.Fatal("key unavailable")
	}
	unlock()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(leftover)
	if err != nil || string(data) != string(old) || len(entries) != 1 {
		t.Fatalf("lock files modified: %d %q %v", len(entries), data, err)
	}
}

func TestLockWaitsDoNotSpawnGoroutines(t *testing.T) {
	baseline := runtime.NumGoroutine()
	m := NewMutex(LevelDevice, "leak-check")
	_, release := lockMutex(t, m, context.Background())
	defer release()
	var wg sync.WaitGroup
	for range 100 {
		wg.Go(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
			defer cancel()
			_, unlock, err := m.Lock(ctx, 0)
			if unlock != nil {
				unlock()
			}
			if !errors.Is(err, context.DeadlineExceeded) {
				t.Errorf("cancelled wait: %v", err)
			}
		})
	}
	wg.Wait()
	release()
	eventually(t, func() bool { return runtime.NumGoroutine() <= baseline+2 })
}
