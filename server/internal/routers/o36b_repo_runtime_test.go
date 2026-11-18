package routers

import (
	"context"
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
	"configflow/internal/lockx"
	"configflow/internal/repo"
	"configflow/internal/store"
)

func o36bRepositoryState(t *testing.T, r *repo.Repository) (store.Fingerprint, map[string]string) {
	t.Helper()
	head, err := r.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	err = filepath.WalkDir(r.DataDir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(r.DataDir(), path)
		if err != nil {
			return err
		}
		switch relative {
		case repo.DatabaseFilename, repo.DatabaseFilename + "-wal", repo.DatabaseFilename + "-shm", repo.DatabaseFilename + "-journal":
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if entry.IsDir() {
			files[relative] = info.Mode().String()
			return nil
		}
		content, err := os.ReadFile(path)
		if err == nil {
			files[relative] = info.Mode().String() + string(content)
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return head, files
}

func o36bRouterCommit(ctx context.Context, r *repo.Repository, connection *jsonx.Object, operation string) error {
	if strings.HasPrefix(operation, "generic-") {
		return jobUpdate(r, ctx, strings.TrimPrefix(operation, "generic-"), connection)
	}
	device := jobConnection(connection.GetString("endpoint"))
	held, release, err := RouterJobLock(ctx, "live", "router", device)
	if err != nil {
		return err
	}
	defer release()
	held, unlock, err := RouterOperationLock(held, *device, true)
	if err != nil {
		return err
	}
	defer unlock()
	switch operation {
	case "settings":
		replacement := connection.DeepCopy()
		replacement.Set("password", "synthetic-runtime-repair")
		return r.SaveRouterSettings(held, "live", connection, replacement)
	case "settings-delete":
		return r.SaveRouterSettings(held, "live", connection, nil)
	case "publication":
		return r.SaveRouterPublication(held, "live", connection, repo.RouterPublication{Manifest: repo.Some(jsonx.Obj("owner", "configflow:synthetic", "resources", []any{}))})
	default:
		return r.SaveRouterPublication(held, "live", connection, repo.RouterPublication{Manifest: repo.Some(nil)})
	}
}

// TestO36bRepositoryRuntimeCommitBoundary exercises the real final SQLite
// barrier after O3-6c, including the caller-held JobLock exemption.
func TestO36bRepositoryRuntimeCommitBoundary(t *testing.T) {
	for _, operation := range []string{"generic-password", "generic-manifest", "generic-delete", "settings", "settings-delete", "publication", "withdrawal"} {
		for _, point := range []string{"lock-wait", "before-commit"} {
			for _, shutdown := range []bool{true, false} {
				name := operation + "/" + point + "/disconnect"
				if shutdown {
					name = operation + "/" + point + "/shutdown"
				}
				t.Run(name, func(t *testing.T) {
					r, base, connection := jobRepository(t)
					Wire()
					if operation == "withdrawal" {
						if err := o36bRouterCommit(base, r, connection, "publication"); err != nil {
							t.Fatal(err)
						}
					}
					runtime, request, disconnect := o36bRuntime(t)
					ctx := repo.WithRepository(request, r)
					before, files := o36bRepositoryState(t, r)
					violations := lockx.Violations()
					stop := func() {
						if shutdown {
							runtime.Shutdown(context.Background())
						} else {
							disconnect()
						}
					}
					var holder <-chan error
					var release func()
					failure := errors.New("synthetic holder rollback")
					if point == "lock-wait" {
						entered, leave := make(chan struct{}), make(chan struct{})
						var once sync.Once
						release = func() { once.Do(func() { close(leave) }) }
						defer release()
						done := make(chan error, 1)
						holder = done
						go func() {
							_, err := r.UpdateSystemTransaction(base, func(context.Context, *jsonx.Object) (*jsonx.Object, error) {
								close(entered)
								<-leave
								return nil, failure
							})
							done <- err
						}()
						select {
						case <-entered:
						case <-time.After(10 * time.Second):
							t.Fatal("holder did not acquire repository lock")
						}
					} else {
						r.Store().BeforeCommit = func() error { stop(); return nil }
						defer func() { r.Store().BeforeCommit = nil }()
					}
					done := make(chan error, 1)
					go func() { done <- o36bRouterCommit(ctx, r, connection, operation) }()
					if point == "lock-wait" {
						select {
						case err := <-done:
							t.Fatal("candidate escaped held L3", err)
						case <-time.After(50 * time.Millisecond):
						}
						stop()
						release()
						select {
						case err := <-holder:
							if !errors.Is(err, failure) {
								t.Fatal("holder unexpectedly committed", err)
							}
						case <-time.After(10 * time.Second):
							t.Fatal("holder did not roll back")
						}
					}
					var err error
					select {
					case err = <-done:
					case <-time.After(10 * time.Second):
						t.Fatal("router commit did not finish")
					}
					after, afterFiles := o36bRepositoryState(t, r)
					if shutdown {
						if !errors.Is(err, context.Canceled) || before != after || !reflect.DeepEqual(files, afterFiles) {
							t.Fatalf("shutdown committed router DB/files/revision: %v", err)
						}
					} else {
						if err != nil || after.Generation != before.Generation+1 || !reflect.DeepEqual(files, afterFiles) {
							t.Fatalf("disconnected router commit failed or was not one commit: %v", err)
						}
						o36bCheckRouterCommit(t, r, base, operation)
					}
					_, unlock, err := RouterJobLock(base, "live", "router", jobConnection(connection.GetString("endpoint")))
					if err != nil {
						t.Fatal("finished commit retained its job lease", err)
					}
					unlock()
					if lockx.Violations() != violations || violations != 0 {
						t.Fatal("router/repository lock order violated")
					}
				})
			}
		}
	}
}

func o36bCheckRouterCommit(t *testing.T, r *repo.Repository, ctx context.Context, operation string) {
	t.Helper()
	profile, err := r.GetProfile(ctx, "live")
	if err != nil {
		t.Fatal(err)
	}
	items := profile.GetObject("routers").GetList("items")
	if operation == "generic-delete" || operation == "settings-delete" {
		if len(items) != 0 {
			t.Fatal("disconnected delete retained router")
		}
		return
	}
	if len(items) != 1 {
		t.Fatal("disconnected commit lost router")
	}
	current, ok := items[0].(*jsonx.Object)
	if !ok {
		t.Fatal("router is not a document")
	}
	switch operation {
	case "generic-password":
		if current.GetString("password") != "synthetic-replacement" {
			t.Fatal("connection edit was not committed")
		}
	case "settings":
		if current.GetString("password") != "synthetic-runtime-repair" {
			t.Fatal("caller-held connection edit was not committed")
		}
	case "withdrawal":
		if current.Has("ros_policy_state") {
			t.Fatal("publication withdrawal retained manifest")
		}
	default:
		manifest := current.GetObject("ros_policy_state")
		if manifest == nil || manifest.GetString("owner") != "configflow:synthetic" {
			t.Fatal("verified manifest was not committed")
		}
	}
}
