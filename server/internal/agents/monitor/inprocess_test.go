package monitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/lockx"
	"configflow/internal/repo"
)

func stateResult[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case result := <-ch:
		return result
	case <-time.After(5 * time.Second):
		t.Fatal("dashboard work blocked")
		var zero T
		return zero
	}
}

func holdState(t *testing.T, store *StateStore) func() {
	t.Helper()
	key, err := repo.ResolveDataDir(store.Path("one"))
	if err != nil {
		t.Fatal(err)
	}
	_, release, err := stateLocks.Lock(context.Background(), key, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	var once sync.Once
	done := func() { once.Do(release) }
	t.Cleanup(done)
	return done
}

func TestStateProductionLimitCancellationAndLegacySentinel(t *testing.T) {
	if LockTimeout != 5*time.Second {
		t.Fatal("production dashboard limit changed", LockTimeout)
	}
	store := NewStateStore(t.TempDir())
	if err := os.MkdirAll(store.Directory, 0700); err != nil {
		t.Fatal(err)
	}
	legacy := store.Path("one")
	legacy = legacy[:len(legacy)-len(filepath.Ext(legacy))] + ".lock"
	if err := os.WriteFile(legacy, []byte("python sentinel"), 0600); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1700000000, 0)
	if err := os.Chtimes(legacy, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(legacy)
	if err != nil {
		t.Fatal(err)
	}
	release := holdState(t, store)
	original := LockTimeout
	LockTimeout = 20 * time.Millisecond
	t.Cleanup(func() { LockTimeout = original })
	_, err = store.Read(context.Background(), "one")
	var busy *StateError
	if !errors.As(err, &busy) || busy.Message != "DNS statistics state is busy" {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = store.Read(canceled, "one"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	if _, err = NewStateStore(filepath.Dir(store.Directory)).Read(context.Background(), "one"); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(legacy)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) || !before.ModTime().Equal(after.ModTime()) || string(data) != "python sentinel" {
		t.Fatal("legacy lock changed")
	}
	if _, err = store.Read(context.Background(), "two"); err != nil {
		t.Fatal(err)
	}
	path := store.Path("two")
	if _, err = os.Stat(path[:len(path)-len(filepath.Ext(path))] + ".lock"); !os.IsNotExist(err) {
		t.Fatal("created resource lock", err)
	}
}

func TestConfigurationVerificationRemainsLockedThroughLeafCAS(t *testing.T) {
	for _, kind := range []string{"target", "publication"} {
		t.Run(kind, func(t *testing.T) {
			e := newPubEnv(t)
			repository, err := repo.Open(e.m.DataDir(), repo.Options{})
			if err != nil {
				t.Fatal(err)
			}
			stored, err := e.m.GetAgentByID(ctx, "one", true)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = repository.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
				system.Set("agents", []any{stored})
				return nil, nil
			}); err != nil {
				t.Fatal(err)
			}
			manager, err := agents.New(ctx, repository)
			if err != nil {
				t.Fatal(err)
			}
			owner := FromAgents(manager)
			digest, err := target(stored)
			if err != nil {
				t.Fatal(err)
			}
			pending := jsonx.Obj("deployment_id", "publish-one", "expected_revision", revB)
			verify := func(held context.Context) error {
				if kind == "publication" {
					return verifyPublication(held, owner, "one", digest, pending)
				}
				return verifyTarget(held, owner, "one", digest)
			}
			state, err := e.store.Read(ctx, "one")
			if err != nil {
				t.Fatal(err)
			}
			replacement := state.DeepCopy()
			replacement.Set("generation", "11111111111111111111111111111111")
			release := holdState(t, e.store)
			verified := make(chan struct{})
			finished := make(chan error, 1)
			go func() {
				finished <- compareState(ctx, owner, e.store, "one", state.GetString("generation"), replacement, func(held context.Context) error {
					err := verify(held)
					close(verified)
					return err
				})
			}()
			stateResult(t, verified)
			changed, started := make(chan error, 1), make(chan struct{})
			go func() {
				close(started)
				_, err := manager.UpdateAgents(ctx, "", func(_ context.Context, values *[]any) (any, error) {
					agent := (*values)[0].(*jsonx.Object)
					if kind == "target" {
						agent.Set("host", "192.0.2.24")
					} else {
						agent.GetObject("deployments").GetObject("publish-one").Set("status", "failed")
					}
					return nil, nil
				})
				changed <- err
			}()
			stateResult(t, started)
			select {
			case err := <-changed:
				t.Fatal("configuration changed between verification and CAS", err)
			case <-time.After(20 * time.Millisecond):
			}
			release()
			if err := stateResult(t, finished); err != nil {
				t.Fatal(err)
			}
			if err := stateResult(t, changed); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(e.store.Path("one"))
			if err != nil {
				t.Fatal(err)
			}
			err = compareState(ctx, owner, e.store, "one", replacement.GetString("generation"), state, verify)
			if kind == "target" {
				wantMonitorError(t, err, "DASHBOARD_CHANGED", 409)
			} else if !errors.Is(err, ErrStateConflict) {
				t.Fatal(err)
			}
			after, err := os.ReadFile(e.store.Path("one"))
			if err != nil {
				t.Fatal(err)
			}
			if string(before) != string(after) {
				t.Fatal("failed verification changed state")
			}
			if lockx.Violations() != 0 {
				t.Fatal("invalid lock order", lockx.Violations())
			}
		})
	}
}
