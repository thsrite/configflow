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

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/store"
)

func commitRuntime(t *testing.T) (*repo.Repository, *lifecycle.Runtime, context.Context) {
	t.Helper()
	r, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
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
				_, _, err := r.Store().Commit(t.Context(), entry, entry.Document, map[string]json.RawMessage{"o36c_holder": json.RawMessage(`true`)})
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
				_, _, err := other.Commit(work, entry, entry.Document, map[string]json.RawMessage{"o36c_target": json.RawMessage(`true`)})
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
	raw, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	return r.UpdateSystem(ctx, func(_ context.Context, current *model.System) error {
		return markerSet(&current.SystemConfig.GitHubProxyDomain, raw)
	})
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
				err := r.UpdateSystem(base, func(_ context.Context, _ *model.System) error {
					close(locked)
					<-release
					return holdError
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

// TestO36cRepoOwnerRuntimeMatrix mirrors the six N8-5a review scenarios through
// the current public transaction entry. The typed Update variant must also run
// after N8-5a moves this shared commit core into transact/transactLocked.
func TestO36cRepoOwnerRuntimeMatrix(t *testing.T) {
	for _, point := range []string{"before-owner", "updater", "before-commit"} {
		for _, shutdown := range []bool{true, false} {
			name := point + "/disconnect"
			if shutdown {
				name = point + "/shutdown"
			}
			t.Run(name, func(t *testing.T) {
				r, runtime, base := commitRuntime(t)
				request, disconnect := context.WithCancel(runtime.BindRequest(base))
				defer disconnect()
				before, files := commitState(t, r)
				stop := func() {
					if shutdown {
						runtime.Shutdown(context.Background())
					} else {
						disconnect()
					}
				}
				if point == "before-owner" {
					stop()
				}
				if point == "before-commit" {
					r.Store().BeforeCommit = func() error { stop(); return nil }
					defer func() { r.Store().BeforeCommit = nil }()
				}
				err := r.UpdateSystem(request, func(_ context.Context, current *model.System) error {
					if point == "updater" {
						stop()
					}
					return markerSet(&current.SystemConfig.GitHubProxyDomain, json.RawMessage(`"runtime-matrix"`))
				})
				after, afterFiles := commitState(t, r)
				if shutdown {
					if !errors.Is(err, context.Canceled) || before != after || !reflect.DeepEqual(files, afterFiles) {
						t.Fatalf("shutdown result=%v DB/files/revisions changed", err)
					}
				} else if err != nil || before == after || !reflect.DeepEqual(files, afterFiles) {
					t.Fatalf("disconnected transaction did not complete: %v", err)
				}
			})
		}
	}
}
