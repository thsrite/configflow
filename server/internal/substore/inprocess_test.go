package substore

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/lockx"
	"configflow/internal/model"
)

func substoreEventually(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !fn() {
		if time.Now().After(deadline) {
			t.Fatal("Sub-Store condition not reached")
		}
		runtime.Gosched()
	}
}

func TestRuntimeProductionLeaseAndStartupLimits(t *testing.T) {
	if workWait != 45*time.Second || maintenanceWait != 90*time.Second || controlWait != 45*time.Second || startupWait != 20*time.Second || startupPoll != 200*time.Millisecond {
		t.Fatal("production lease/startup timing changed", workWait, maintenanceWait, controlWait, startupWait, startupPoll)
	}
}

func TestNestedUseDoesNotRLockBehindWaitingWriter(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true})
	r := h.service.Runtime
	r.ready = func(context.Context) (bool, error) { return true, nil }
	_, leases, unpin, err := r.pinLeases()
	if err != nil {
		t.Fatal(err)
	}
	defer unpin()
	writer := make(chan error, 1)
	err = r.Use(context.Background(), func(held context.Context) error {
		go func() {
			_, release, err := leases.work.Lock(context.Background(), time.Second)
			if err == nil {
				release()
			}
			writer <- err
		}()
		substoreEventually(t, func() bool {
			_, release, ok := leases.work.TryRLock(context.Background())
			if ok {
				release()
			}
			return !ok
		})
		// Reuse the existing lease; a recursive RLock here would wait for a writer
		// which cannot enter until this outer conversion finishes.
		return r.Use(held, func(context.Context) error { return nil })
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := <-writer; err != nil {
		t.Fatal("nested Use blocked the waiting writer", err)
	}
	if lockx.Violations() != 0 {
		t.Fatal("normal order violation", lockx.Violations())
	}
}

func TestRuntimeLeavesLegacyLocksAndCancelsWaiters(t *testing.T) {
	h := newHarness(t, harnessOptions{builtin: true})
	r := h.service.Runtime
	r.ready = func(context.Context) (bool, error) { return true, nil }
	if err := os.MkdirAll(r.Root(), 0700); err != nil {
		t.Fatal(err)
	}
	type sentinel struct {
		path   string
		before os.FileInfo
	}
	var files []sentinel
	for _, name := range []string{"work.lock", "control.lock"} {
		path := filepath.Join(r.Root(), name)
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
		files = append(files, sentinel{path, before})
	}
	disconnected, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.Use(disconnected, func(context.Context) error { return nil }); err != nil {
		t.Fatal(err)
	}
	_, leases, unpin, err := r.pinLeases()
	if err != nil {
		t.Fatal(err)
	}
	defer unpin()
	_, release, err := leases.work.Lock(context.Background(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	job, shutdown := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- r.Use(ctxx.WithParent(context.Background(), job), func(context.Context) error { t.Error("canceled waiter ran"); return nil })
	}()
	shutdown()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("job cancellation did not stop lease wait")
	}
	err = r.withWork(context.Background(), false, 20*time.Millisecond, func(context.Context) error { t.Error("work entered busy lease"); return nil })
	if !IsRuntimeUnavailable(err) || err.Error() != msgRuntimeBusy {
		t.Fatal("leaf lease limit ignored", err)
	}
	release()
	if err := r.tryWork(context.Background(), func(ctx context.Context) error { return r.withControl(ctx, func(context.Context) error { return nil }) }); err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		after, err := os.Stat(file.path)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(file.path)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(file.before, after) || !file.before.ModTime().Equal(after.ModTime()) || string(data) != "python sentinel" {
			t.Fatal("legacy lease changed")
		}
	}
}

func TestRuntimeReverseOrderProcess(t *testing.T) {
	if os.Getenv("CONFIGFLOW_SUBSTORE_ORDER_HELPER") != "1" {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRuntimeReverseOrderProcess$", "-test.count=1")
		cmd.Env = append(os.Environ(), "CONFIGFLOW_SUBSTORE_ORDER_HELPER=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("order child: %v\n%s", err, out)
		}
		return
	}
	h := newHarness(t, harnessOptions{builtin: true})
	r := h.service.Runtime
	if err := r.withControl(context.Background(), func(held context.Context) error {
		return r.withWork(held, false, time.Second, func(context.Context) error { return nil })
	}); err != nil {
		t.Fatal(err)
	}
	if lockx.Violations() != 1 {
		t.Fatal("reverse work/control order not reported", lockx.Violations())
	}
}

func TestCacheRemovalPreservesPythonLock(t *testing.T) {
	for _, prune := range []bool{false, true} {
		t.Run(map[bool]string{false: "delete", true: "prune"}[prune], func(t *testing.T) {
			ctx, r := sharedRepo(t)
			path, err := CachePath(ctx, "drop")
			if err != nil {
				t.Fatal(err)
			}
			path += ".lock"
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
			if prune {
				err = r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
					shared.Subscriptions = []model.Subscription{}
					return nil
				})
				if err == nil {
					_, err = PruneOrphanCaches(ctx)
				}
			} else {
				_, err = DeleteSubscriptionCache(ctx, "drop")
			}
			if err != nil {
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
				t.Fatal("cache removal changed old lock")
			}
		})
	}
}
