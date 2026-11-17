package weblistener

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/lockx"
)

func listenerResult[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("listener work blocked")
		var zero T
		return zero
	}
}

func TestListenerAdmissionPreservesLegacyFilesAndValidationOrder(t *testing.T) {
	m := manager(t, map[string]string{"CONFIGFLOW_WEB_PORT": strconv.Itoa(freePort(t))})
	path := m.lockPath()
	if err := os.WriteFile(path, []byte("python sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	disconnected, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Start(disconnected); err != nil {
		t.Fatal("client disconnect aborted owner", err)
	}
	if _, err := m.Status(disconnected); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || string(data) != "python sentinel" {
		t.Fatal("legacy lock changed")
	}
	_, release, err := m.lock(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	other := &Manager{DataDir: m.DataDir}
	result := make(chan error, 1)
	go func() { _, err := other.Status(context.Background()); result <- err }()
	wantError(t, listenerResult(t, result), 409, msgBusy)
	_, err = other.Update(context.Background(), false)
	wantError(t, err, 400, "")
	if lockx.Violations() != 0 {
		t.Fatal("unexpected order violation", lockx.Violations())
	}
	fresh := manager(t, map[string]string{"CONFIGFLOW_WEB_PORT": strconv.Itoa(freePort(t))})
	if _, err = fresh.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(fresh.DataDir, "web-listener.lock")); !os.IsNotExist(err) {
		t.Fatal("created resource lock", err)
	}
}

func TestHealthWaitingUsesClockAndDetachedShutdownLifetime(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "timeout", true: "shutdown"}[shutdown], func(t *testing.T) {
			clock := lifecycletest.NewClock(time.Unix(0, 0))
			runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Clock: clock})
			runner.Start(context.Background())
			t.Cleanup(func() { runner.Shutdown(context.Background()) })
			request, cancel := context.WithCancel(context.Background())
			bound := runner.BindRequest(request)
			cancel()
			ctx := lifecycle.Detach(bound)
			done := make(chan error, 1)
			m := &Manager{}
			port := freePort(t) // A closed loopback listener makes probes fail immediately.
			go func() { done <- m.waitHealthy(ctx, port, false, "token") }()
			deadline := time.Now().Add(5 * time.Second)
			for clock.Pending() != 1 {
				if time.Now().After(deadline) {
					t.Fatal("health wait did not reach the injected clock")
				}
				runtime.Gosched()
			}
			select {
			case err := <-done:
				t.Fatal("client disconnect aborted health wait", err)
			default:
			}
			if shutdown {
				runner.Shutdown(context.Background())
			} else {
				clock.Advance(5 * time.Second)
			}
			err := listenerResult(t, done)
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
			} else {
				wantError(t, err, 503, msgHealthFailed)
			}
			if clock.Pending() != 0 {
				t.Fatal("health timer leaked")
			}
		})
	}
}
