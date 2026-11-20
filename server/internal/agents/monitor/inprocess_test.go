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
	"configflow/internal/lockx"
	"configflow/internal/model"
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
	_, err = store.ReadState(context.Background(), "one")
	var busy *StateError
	if !errors.As(err, &busy) || busy.Message != "DNS statistics state is busy" {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = store.ReadState(canceled, "one"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	release()
	if _, err = NewStateStore(filepath.Dir(store.Directory)).ReadState(context.Background(), "one"); err != nil {
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
	if _, err = store.ReadState(context.Background(), "two"); err != nil {
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
			stored, err := e.m.ReadAgent(ctx, "one", true)
			if err != nil {
				t.Fatal(err)
			}
			if err = repository.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
				system.Agents = []model.Agent{*stored}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			manager, err := agents.New(ctx, repository)
			if err != nil {
				t.Fatal(err)
			}
			owner := TypedFromAgents(manager)
			digest, err := TargetIdentity(stored)
			if err != nil {
				t.Fatal(err)
			}
			pending := &PendingPublication{DeploymentID: "publish-one", ExpectedRevision: revB}
			verify := func(held context.Context) error {
				if kind == "publication" {
					return verifyNativePublication(held, owner, "one", digest, pending)
				}
				return verifyNativeTarget(held, owner, "one", digest)
			}
			state, err := e.store.ReadState(ctx, "one")
			if err != nil {
				t.Fatal(err)
			}
			replacement, err := DecodeState(mustMarshal(t, state))
			if err != nil {
				t.Fatal(err)
			}
			replacement.Generation = "11111111111111111111111111111111"
			release := holdState(t, e.store)
			verified := make(chan struct{})
			finished := make(chan error, 1)
			go func() {
				finished <- compareDashboardState(ctx, owner, e.store, "one", state.Generation, replacement, func(held context.Context) error {
					err := verify(held)
					close(verified)
					return err
				})
			}()
			stateResult(t, verified)
			changed, started := make(chan error, 1), make(chan struct{})
			go func() {
				close(started)
				err := manager.EditAgents(ctx, "", func(_ context.Context, values *[]model.Agent) error {
					agent := &(*values)[0]
					if kind == "target" {
						return agent.SetJSON("host", []byte(`"192.0.2.24"`))
					}
					d, _ := agent.Deployments.Get("publish-one")
					d.Status = "failed"
					agent.Deployments.Set("publish-one", d)
					return nil
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
			err = compareDashboardState(ctx, owner, e.store, "one", replacement.Generation, state, verify)
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
