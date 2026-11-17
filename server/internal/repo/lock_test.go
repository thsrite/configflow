package repo

// Lock and transaction semantics (no Python test file covers them directly).

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"configflow/internal/jsonx"
)

func TestLockTimeoutParsing(t *testing.T) {
	os.Unsetenv(LockTimeoutEnv)
	if got := LockTimeout(); got != 300*time.Second {
		t.Fatalf("unset: %v", got)
	}
	for value, want := range map[string]time.Duration{
		"abc": 300 * time.Second, "nan": 300 * time.Second, "inf": 300 * time.Second, "": 300 * time.Second,
		"0": 100 * time.Millisecond, "0.05": 100 * time.Millisecond, "-3": 100 * time.Millisecond,
		"5": 5 * time.Second, " 2.5 ": 2500 * time.Millisecond, "99999": 3600 * time.Second,
	} {
		t.Setenv(LockTimeoutEnv, value)
		if got := LockTimeout(); got != want {
			t.Errorf("%q: %v, want %v", value, got, want)
		}
	}
}

func TestWriteWaitsForForeignFileLockUntilTimeout(t *testing.T) {
	r := newRepo(t)
	t.Setenv(LockTimeoutEnv, "0.3")
	handle := must(os.OpenFile(filepath.Join(r.DataDir(), ".config.lock"), os.O_RDWR, 0))(t)
	defer handle.Close()
	noError(t, syscall.Flock(int(handle.Fd()), syscall.LOCK_EX))
	started := time.Now()
	_, err := r.UpdateProfile(bg, "default", jsonx.Obj("name", "blocked"))
	wantError(t, err, ErrRepository, "Timed out waiting for profile repository lock")
	if waited := time.Since(started); waited < 250*time.Millisecond || waited > 3*time.Second {
		t.Fatalf("waited %v", waited)
	}
	// Reads never need the lock.
	wantEqual(t, must(r.GetProfile(bg, "default"))(t).Value("_revision"), 0)
	noError(t, syscall.Flock(int(handle.Fd()), syscall.LOCK_UN))
	must(r.UpdateProfile(bg, "default", jsonx.Obj("name", "free")))(t)
}

func TestWriteWaitsForReleasedFileLock(t *testing.T) {
	r := newRepo(t)
	t.Setenv(LockTimeoutEnv, "5")
	handle := must(os.OpenFile(filepath.Join(r.DataDir(), ".config.lock"), os.O_RDWR, 0))(t)
	defer handle.Close()
	noError(t, syscall.Flock(int(handle.Fd()), syscall.LOCK_EX))
	fd := int(handle.Fd())
	released := make(chan struct{})
	go func() {
		defer close(released)
		time.Sleep(200 * time.Millisecond)
		syscall.Flock(fd, syscall.LOCK_UN)
	}()
	started := time.Now()
	must(r.UpdateProfile(bg, "default", jsonx.Obj("name", "waited")))(t)
	<-released // the deferred Close must not race with the unlocking goroutine
	if waited := time.Since(started); waited < 150*time.Millisecond {
		t.Fatalf("did not wait: %v", waited)
	}
}

func TestNestedWriteIsRefusedAndReadsSeeWorkingCopy(t *testing.T) {
	r := newRepo(t)
	_, err := r.UpdateSystemTransaction(bg, func(ctx context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		if !r.InTransaction(ctx) || r.InTransaction(bg) {
			t.Error("InTransaction")
		}
		_, err := r.UpdateProfile(ctx, "default", jsonx.Obj("name", "nested"))
		wantError(t, err, ErrInUse, "不能在配置事务中再次开启写事务")
		_, err = r.UpdateSharedTransaction(ctx, func(context.Context, *jsonx.Object) (*jsonx.Object, error) { return nil, nil })
		wantError(t, err, ErrInUse, "不能在配置事务中再次开启写事务")
		system.GetObject("system_config").Set("marker", "working")
		return nil, nil
	})
	noError(t, err)

	_, _, err = r.UpdateProfileTransaction(bg, "default", func(ctx context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		// The working copy is the transaction's document; edits made by the
		// updater to its snapshot are applied after it returns.
		wantEqual(t, must(r.GetSystem(ctx))(t).GetObject("system_config").Value("marker"), "working")
		done := make(chan any, 1)
		go func() { // another call chain: the committed state, without waiting
			done <- must(r.ConfigRevision(context.Background(), "default"))(t)
		}()
		wantEqual(t, <-done, 0)
		current.GetObject("mihomo").Set("custom_config", "x")
		return nil, nil
	})
	noError(t, err)
	wantEqual(t, must(r.ConfigRevision(bg, "default"))(t), 1)
}

func TestReenteringAHeldLockTimesOut(t *testing.T) {
	if repositoryChild(t, "TestReenteringAHeldLockTimesOut") {
		return
	}
	t.Setenv(LockTimeoutEnv, "0.2")
	path := filepath.Join(t.TempDir(), "x.lock")
	err := withLock(bg, path, func(ctx context.Context) error {
		if !holdsLock(ctx, path) || holdsLock(bg, path) {
			t.Error("holdsLock")
		}
		return withLock(ctx, path, func(context.Context) error { return nil })
	})
	wantError(t, err, ErrRepository, "Timed out waiting for profile repository lock")
	noError(t, withLock(bg, path, func(context.Context) error { return nil }))
}

func TestLockWaitStopsWhenContextIsCancelled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.lock")
	ctx, cancel := context.WithCancel(bg)
	err := withLock(bg, path, func(context.Context) error {
		go func() { time.Sleep(50 * time.Millisecond); cancel() }()
		return withLock(ctx, path, func(context.Context) error { return nil })
	})
	if err != context.Canceled {
		t.Fatalf("got %v", err)
	}
}

func TestConcurrentWritersAreSerialized(t *testing.T) {
	r := newRepo(t)
	const writers, rounds = 8, 5
	var group sync.WaitGroup
	failures := make(chan error, writers*rounds)
	for range writers {
		group.Add(1)
		go func() {
			defer group.Done()
			for range rounds {
				_, _, err := r.UpdateProfileTransaction(bg, "default", func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
					mihomo := current.GetObject("mihomo")
					mihomo.Set("counter", jsonx.Int(mihomo.GetInt("counter")+1))
					return nil, nil
				})
				if err != nil {
					failures <- err
				}
			}
		}()
	}
	group.Wait()
	close(failures)
	for err := range failures {
		t.Fatal(err)
	}
	profile := must(r.GetProfile(bg, "default"))(t)
	wantEqual(t, profile.GetObject("mihomo").Value("counter"), writers*rounds)
	wantEqual(t, profile.Value("_revision"), writers*rounds)
	if r.CommitCount() != writers*rounds {
		t.Fatalf("commit count %d", r.CommitCount())
	}
}
