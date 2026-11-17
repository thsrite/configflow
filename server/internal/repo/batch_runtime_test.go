package repo_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/store"
)

func batchFixture(t *testing.T, r *repo.Repository, base context.Context, kind string) (func(context.Context) error, func()) {
	t.Helper()
	switch kind {
	case "delete":
		if _, err := r.CreateProfile(base, jsonx.Obj("id", "delete-me", "name", "Delete"), ""); err != nil {
			t.Fatal(err)
		}
		if _, err := r.WriteProfileText(base, "delete-me", "private.txt", []byte("retained artifact")); err != nil {
			t.Fatal(err)
		}
		return func(ctx context.Context) error { return r.DeleteProfile(ctx, "delete-me", "") }, func() {
			if _, err := r.ProfileMetadata(base, "delete-me"); err == nil {
				t.Fatal("deleted profile is still in the database")
			}
			paths, err := filepath.Glob(filepath.Join(r.DataDir(), "migrations", "deleted-delete-me-*", "private.txt"))
			if err != nil || len(paths) != 1 {
				t.Fatal("deleted artifacts were not retained", paths, err)
			}
			data, err := os.ReadFile(paths[0])
			if err != nil || string(data) != "retained artifact" {
				t.Fatal("deleted artifact changed", err)
			}
			data, err = os.ReadFile(filepath.Join(filepath.Dir(paths[0]), "deleted-profile.json"))
			if err != nil {
				t.Fatal(err)
			}
			profile, err := jsonx.Parse(data)
			if err != nil || profile.(*jsonx.Object).GetString("id") != "delete-me" {
				t.Fatal("deleted profile export is incomplete", err)
			}
		}
	case "import", "reset":
		if err := commitMarker(base, r, "before-import"); err != nil {
			t.Fatal(err)
		}
		original, err := r.Store().Fingerprint()
		if err != nil {
			t.Fatal(err)
		}
		document, err := r.ExportAll(base, false)
		if err != nil {
			t.Fatal(err)
		}
		document.GetObject("system").GetObject("system_config").Set("o3_commit_marker", "after-import")
		return func(ctx context.Context) error {
				if kind == "reset" {
					_, err := r.ResetAll(ctx)
					return err
				}
				_, err := r.ImportAll(ctx, document)
				return err
			}, func() {
				system, err := r.GetSystem(base)
				if err != nil {
					t.Fatal(err)
				}
				marker := system.GetObject("system_config").GetString("o3_commit_marker")
				if (kind == "reset" && marker != "") || (kind == "import" && marker != "after-import") {
					t.Fatal("import/reset document did not commit", marker)
				}
				paths, err := filepath.Glob(filepath.Join(r.DataDir(), "migrations", "*", "import", repo.DatabaseFilename))
				if err != nil || len(paths) != 1 {
					t.Fatal("import backup incomplete", paths, err)
				}
				backup := store.New(paths[0], repo.LockTimeout)
				fingerprint, err := backup.Fingerprint()
				if err != nil || fingerprint != original {
					t.Fatal("import backup does not contain the original complete database", err)
				}
			}
	case "create-rule":
		rule := jsonx.Obj("id", "batch-rule", "name", "Batch", "source_type", "content", "content", "example.com\n",
			"behavior", "domain", "format", "text", "enabled", true)
		return func(ctx context.Context) error {
				_, err := r.CreateRuleLibraryItem(ctx, rule, []byte("example.com\n"))
				return err
			}, func() {
				shared, err := r.GetShared(base)
				if err != nil || !jsonx.Equal(shared.GetList("rule_library"), []any{rule}) {
					t.Fatal("created rule document incomplete", err)
				}
				path, err := r.SharedPath(filepath.Join("rules", rules.RuleCacheFilename(rule)))
				if err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(path)
				if err != nil || string(data) != "example.com\n" {
					t.Fatal("created rule cache incomplete", err)
				}
			}
	}
	t.Fatalf("unknown batch %s", kind)
	return nil, nil
}

func holdBatchRepository(t *testing.T, r *repo.Repository, base context.Context) (func(), <-chan error, error) {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	holder := make(chan error, 1)
	failure := errors.New("synthetic holder rollback")
	go func() {
		_, err := r.UpdateSystemTransaction(base, func(context.Context, *jsonx.Object) (*jsonx.Object, error) {
			close(entered)
			<-release
			return nil, failure
		})
		holder <- err
	}()
	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		unblock()
		t.Fatal("batch holder did not acquire repository lock")
	}
	return unblock, holder, failure
}

func TestO36cExplicitBatchRuntimeBoundaries(t *testing.T) {
	for _, kind := range []string{"delete", "import", "reset", "create-rule"} {
		for _, boundary := range []string{"before-marker", "after-marker", "disconnect"} {
			t.Run(kind+"/"+boundary, func(t *testing.T) {
				r, runtime, base := commitRuntime(t)
				run, verify := batchFixture(t, r, base, kind)
				before, files := commitState(t, r)
				ctx, disconnect := context.WithCancel(runtime.BindRequest(base))
				defer disconnect()
				var err error
				if boundary == "before-marker" {
					unblock, holder, failure := holdBatchRepository(t, r, base)
					defer unblock()
					done := make(chan error, 1)
					go func() { done <- run(ctx) }()
					select {
					case err := <-done:
						t.Fatalf("batch escaped repository lock: %v", err)
					case <-time.After(50 * time.Millisecond):
					}
					runtime.Shutdown(context.Background())
					unblock()
					if err := commitResult(t, holder); !errors.Is(err, failure) {
						t.Fatal("holder rollback", err)
					}
					err = commitResult(t, done)
				} else {
					calls := 0
					r.Store().BeforeCommit = func() error {
						calls++
						if boundary == "after-marker" {
							runtime.Shutdown(context.Background())
						}
						return nil
					}
					defer func() { r.Store().BeforeCommit = nil }()
					if boundary == "disconnect" {
						disconnect()
					}
					err = run(ctx)
					if calls != 1 {
						t.Fatal("batch must have one final database commit", calls)
					}
				}
				after, afterFiles := commitState(t, r)
				if boundary == "before-marker" {
					if !errors.Is(err, context.Canceled) || before != after || !reflect.DeepEqual(files, afterFiles) {
						t.Fatalf("batch had effects before admission: %v", err)
					}
				} else {
					if err != nil || after.Generation != before.Generation+1 {
						t.Fatalf("admitted batch did not fully commit: %v", err)
					}
					verify()
				}
			})
		}
	}
}
