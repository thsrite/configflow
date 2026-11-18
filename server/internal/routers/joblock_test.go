package routers

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

func jobConnection(endpoint string) *Router {
	return &Router{RouterSettings: RouterSettings{Type: "routeros", Endpoint: endpoint}}
}

func TestO36bJobLockConnectionIdentity(t *testing.T) {
	tests := []struct {
		name, profile, id string
		connection        *Router
		busy              bool
	}{
		{"same-device-other-record", "other", "alias", jobConnection("http://192.0.2.1///"), true},
		{"different-device", "profile", "router", jobConnection("http://192.0.2.2"), false},
		{"different-type", "profile", "router", &Router{RouterSettings: RouterSettings{Type: "ikuai", Endpoint: "http://192.0.2.1"}}, false},
		{"missing-record", "profile", "router", nil, false},
	}
	_, release, err := RouterJobLock(t.Context(), "profile", "router", jobConnection("http://192.0.2.1/"))
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, unlock, err := RouterJobLock(t.Context(), tt.profile, tt.id, tt.connection)
			if unlock != nil {
				defer unlock()
			}
			if tt.busy {
				failure, ok := errors.AsType[*RouterError](err)
				if !ok || failure.Kind != KindConflict || failure.Message != messageEditBusy || unlock != nil {
					t.Fatalf("busy identity: %v", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
	release()
	release()
	_, unlock, err := RouterJobLock(t.Context(), "other", "alias", jobConnection("http://192.0.2.1"))
	if err != nil {
		t.Fatal("released key stayed busy", err)
	}
	unlock()
}

func TestO36bMissingRouterJobLockAndTryOnly(t *testing.T) {
	_, release, err := RouterJobLock(t.Context(), "missing-profile", "missing-router", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	done := make(chan error, 1)
	go func() {
		_, unlock, err := RouterJobLock(t.Context(), "missing-profile", "missing-router", nil)
		if unlock != nil {
			unlock()
		}
		done <- err
	}()
	select {
	case err := <-done:
		failure, ok := errors.AsType[*RouterError](err)
		if !ok || failure.Kind != KindConflict {
			t.Fatal("missing-record key did not conflict", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("L1 waited instead of trying")
	}
	for _, pair := range [][2]string{{"missing-profile", "other"}, {"other", "missing-router"}} {
		_, unlock, err := RouterJobLock(t.Context(), pair[0], pair[1], nil)
		if err != nil {
			t.Fatal("independent missing record conflicted", err)
		}
		unlock()
	}
}

func TestO36bJobLockRuntimeBoundary(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
			runtime.Start(t.Context())
			defer runtime.Shutdown(context.Background())
			request, cancel := context.WithCancel(runtime.BindRequest(t.Context()))
			defer cancel()
			if shutdown {
				runtime.Shutdown(context.Background())
			} else {
				cancel()
			}
			_, release, err := RouterJobLock(request, "runtime", name, nil)
			if shutdown {
				if !errors.Is(err, context.Canceled) || release != nil {
					t.Fatal("stopped Runtime admitted a job", err)
				}
			} else {
				if err != nil {
					t.Fatal("disconnect canceled the job", err)
				}
				release()
			}
		})
	}
}

func TestO36bJobHookInsideRepositoryAndLegacySentinel(t *testing.T) {
	violations := lockx.Violations()
	held, release, err := lockx.NewMutex(lockx.LevelRepository, "o36b-test-repository").Lock(t.Context(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	connection := jsonx.Obj("type", "routeros", "endpoint", "http://192.0.2.3/")
	unlock, ok := jobLockHook(held, "p", "r", connection)
	if !ok {
		t.Fatal("L3 commit guard did not acquire L1 try lock")
	}
	defer unlock()
	if _, duplicate := jobLockHook(held, "alias", "alias", connection); duplicate {
		t.Fatal("commit guard bypassed the publisher's key")
	}
	if lockx.Violations() != violations {
		t.Fatal("nonblocking commit guard reported an order violation")
	}

	repository, err := repo.Open(t.TempDir(), repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(repository.DataDir(), "router-rule-sync")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "retired.lock")
	if err := os.WriteFile(path, []byte("Python legacy sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1234567890, 0)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	_, free, err := RouterJobLock(repo.WithRepository(t.Context(), repository), "sentinel", "r", nil)
	if err != nil {
		t.Fatal(err)
	}
	free()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	entries, listErr := os.ReadDir(dir)
	if err != nil || listErr != nil || len(entries) != 1 || string(data) != "Python legacy sentinel" || !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("job lock touched legacy files", err, listErr)
	}
}
