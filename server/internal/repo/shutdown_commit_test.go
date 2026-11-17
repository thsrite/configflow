package repo_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/store"
)

func commitRuntime(t *testing.T) (*repo.Repository, *lifecycle.Runtime, context.Context) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(ctx)
	t.Cleanup(func() { runtime.Shutdown(context.Background()) })
	return r, runtime, ctx
}

func TestO36cSQLiteLockRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			r, runtime, base := commitRuntime(t)
			before, files := commitState(t, r)
			entry, err := r.Store().Load()
			if err != nil {
				t.Fatal(err)
			}
			locked, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			holdError := errors.New("synthetic SQLite holder rollback")
			r.Store().BeforeCommit = func() error {
				close(locked)
				<-release
				return holdError
			}
			defer func() { r.Store().BeforeCommit = nil }()
			holder := make(chan error, 1)
			go func() {
				_, _, err := r.Store().Commit(entry, entry.Document, map[string]json.RawMessage{"o36c_holder": json.RawMessage(`true`)})
				holder <- err
			}()
			select {
			case <-locked:
			case <-time.After(10 * time.Second):
				t.Fatal("holder did not acquire SQLite write lock")
			}
			request, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			work := lifecycle.Detach(request)
			done := make(chan error, 1)
			go func() {
				other := store.New(r.Store().Path(), repo.LockTimeout)
				_, _, err := other.CommitContext(work, entry, entry.Document, map[string]json.RawMessage{"o36c_target": json.RawMessage(`true`)})
				done <- err
			}()
			select {
			case err := <-done:
				t.Fatalf("writer escaped SQLite lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				disconnect()
			}
			unblock()
			if err := commitResult(t, holder); !errors.Is(err, holdError) {
				t.Fatal("SQLite holder rollback", err)
			}
			err = commitResult(t, done)
			after, afterFiles := commitState(t, r)
			if shutdown {
				if !errors.Is(err, context.Canceled) || before != after || !reflect.DeepEqual(files, afterFiles) {
					t.Fatalf("SQLite waiter committed after shutdown: %v", err)
				}
			} else if err != nil || before == after || !reflect.DeepEqual(files, afterFiles) {
				t.Fatalf("disconnected SQLite waiter did not commit: %v", err)
			}
		})
	}
}

func commitMarker(ctx context.Context, r *repo.Repository, marker string) error {
	_, err := r.UpdateSystemTransaction(ctx, func(_ context.Context, current *jsonx.Object) (*jsonx.Object, error) {
		current.GetObject("system_config").Set("o3_commit_marker", marker)
		return current, nil
	})
	return err
}

func commitState(t *testing.T, r *repo.Repository) (store.Fingerprint, map[string]string) {
	t.Helper()
	fingerprint, err := r.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	err = filepath.WalkDir(r.DataDir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(r.DataDir(), path)
		if err != nil || strings.HasPrefix(rel, repo.DatabaseFilename) {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[rel] = info.Mode().String()
			return nil
		}
		data, err := os.ReadFile(path)
		if err == nil {
			files[rel] = info.Mode().String() + string(data)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return fingerprint, files
}

func commitResult(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("repository writer did not finish")
		return nil
	}
}

func TestO36cRepoBeforeCommitRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			r, runtime, base := commitRuntime(t)
			ctx, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			before, files := commitState(t, r)
			r.Store().BeforeCommit = func() error {
				if shutdown {
					runtime.Shutdown(context.Background())
				} else {
					disconnect()
				}
				return nil
			}
			defer func() { r.Store().BeforeCommit = nil }()
			err := commitMarker(ctx, r, name)
			after, afterFiles := commitState(t, r)
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Errorf("BeforeCommit shutdown returned %v", err)
				}
				if after != before || !reflect.DeepEqual(files, afterFiles) {
					t.Fatal("cancelled SQLite transaction committed or changed files/revisions")
				}
			} else if err != nil || after == before || !reflect.DeepEqual(files, afterFiles) {
				t.Fatalf("disconnected request did not complete its atomic commit: %v", err)
			}
		})
	}
}

func TestO36cRepoLockWaitRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			r, runtime, base := commitRuntime(t)
			before, files := commitState(t, r)
			locked, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer unblock()
			holder := make(chan error, 1)
			holdError := errors.New("synthetic holder rollback")
			go func() {
				_, err := r.UpdateSystemTransaction(base, func(_ context.Context, _ *jsonx.Object) (*jsonx.Object, error) {
					close(locked)
					<-release
					return nil, holdError
				})
				holder <- err
			}()
			select {
			case <-locked:
			case <-time.After(10 * time.Second):
				t.Fatal("holder did not acquire the repository lock")
			}
			ctx, disconnect := context.WithCancel(runtime.BindRequest(base))
			defer disconnect()
			done := make(chan error, 1)
			go func() { done <- commitMarker(ctx, r, name) }()
			select {
			case err := <-done:
				t.Fatalf("writer escaped the held lock: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				disconnect()
			}
			unblock()
			if err := commitResult(t, holder); !errors.Is(err, holdError) {
				t.Fatal("holder rollback", err)
			}
			err := commitResult(t, done)
			after, afterFiles := commitState(t, r)
			if shutdown {
				if !errors.Is(err, context.Canceled) || before != after || !reflect.DeepEqual(files, afterFiles) {
					t.Fatalf("shutdown lock waiter committed: %v", err)
				}
			} else if err != nil || before == after || !reflect.DeepEqual(files, afterFiles) {
				t.Fatalf("disconnected lock waiter did not commit: %v", err)
			}
		})
	}
}
