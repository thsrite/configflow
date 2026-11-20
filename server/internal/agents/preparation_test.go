package agents

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"configflow/internal/ctxx"
	"configflow/internal/lifecycle"
	"configflow/internal/lockx"
	"configflow/internal/model"
)

func preparationIdle(t *testing.T, m *Manager, agent, id string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		running, err := m.defaultPreparationRunning(agent, id)
		if err != nil {
			t.Fatal(err)
		}
		if !running {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("preparation lease leaked")
		}
		runtime.Gosched()
	}
}

func TestPreparationHeldIsSharedAndPreservesLegacyLock(t *testing.T) {
	e := newEnv(t)
	other := newManager(t, e.repo)
	id := "shared-lease"
	sum := sha256.Sum256([]byte(e.id() + "/" + id))
	root := filepath.Join(e.dir, ".agent-deployment-locks")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, hex.EncodeToString(sum[:]))
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
	request, cancel := context.WithCancel(e.ctx)
	cancel()
	result, err := e.m.ReserveDeployment(request, e.id(), id)
	if err != nil || !deploymentFlagTrue(result, "success") {
		t.Fatal(result, err)
	}
	t.Cleanup(func() { e.finish(id) })
	if held, err := other.defaultPreparationRunning(e.id(), id); err != nil || !held {
		t.Fatal("manager copies disagree", held, err)
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
	e.finish(id)
	preparationIdle(t, other, e.id(), id)
	if got := must[*model.Deployment](t)(other.GetDeploymentModel(e.ctx, e.id(), id)).Status; got != "failed" {
		t.Fatal("idle preparation was not recovered", got)
	}
	id = "new-lease"
	e.begin(id)
	e.finish(id)
	sum = sha256.Sum256([]byte(e.id() + "/" + id))
	if _, err = os.Stat(filepath.Join(root, hex.EncodeToString(sum[:]))); !os.IsNotExist(err) {
		t.Fatal("created preparation lock", err)
	}
	if lockx.Violations() != 0 {
		t.Fatal(lockx.Violations())
	}
}

func TestPreparationLeaseReleasedOnCommitFailurePanicAndShutdown(t *testing.T) {
	for _, mode := range []string{"failure", "panic", "shutdown"} {
		t.Run(mode, func(t *testing.T) {
			e := newEnv(t)
			beforeState := e.system()
			beforeDB, readErr := e.repo.Store().Fingerprint()
			if readErr != nil {
				t.Fatal(readErr)
			}
			job, cancel := context.WithCancel(context.Background())
			defer cancel()
			ctx := ctxx.WithParent(e.ctx, job)
			e.repo.Store().BeforeCommit = func() error {
				if held, err := e.m.defaultPreparationRunning(e.id(), mode); err != nil || !held {
					t.Error("lease missing during commit", err)
				}
				switch mode {
				case "failure":
					return errors.New("synthetic commit failure")
				case "panic":
					panic("synthetic commit panic")
				default:
					cancel()
					return nil
				}
			}
			var err error
			var recovered any
			func() { defer func() { recovered = recover() }(); _, err = e.m.ReserveDeployment(ctx, e.id(), mode) }()
			e.repo.Store().BeforeCommit = nil
			if mode == "failure" && err == nil {
				t.Fatal("missing failure")
			}
			if mode == "panic" && recovered == nil {
				t.Fatal("missing synthetic panic")
			}
			if mode == "shutdown" {
				if !errors.Is(err, context.Canceled) || recovered != nil {
					t.Fatal("shutdown must reject the final commit", err, recovered)
				}
				afterState := e.system()
				afterDB, readErr := e.repo.Store().Fingerprint()
				if readErr != nil || afterDB != beforeDB || afterState.Revision != beforeState.Revision || diffJSONValues(t, beforeState, afterState) != "" {
					t.Fatal("shutdown changed database, revision or deployment state", readErr)
				}
				agent := e.stored(e.id())
				if _, ok := agent.Deployments.Get(mode); ok {
					t.Fatal("shutdown persisted the canceled preparation")
				}
			}
			preparationIdle(t, e.m, e.id(), mode)
		})
	}
}

func TestPreparationBusyPreservesValidationOrderAndForeignLease(t *testing.T) {
	e := newEnv(t)
	key := [3]string{e.dir, e.id(), "busy"}
	_, release, ok := preparationLocks.TryLock(e.ctx, key)
	if !ok {
		t.Fatal("cannot hold preparation key")
	}
	defer release()
	got, err := e.m.ReserveDeployment(e.ctx, e.id(), "busy")
	if err != nil || got.HTTPStatus == nil || *got.HTTPStatus != 409 || derefStr(got.Message) != "Deployment preparation already running" {
		t.Fatal(got, err)
	}
	if !preparationLocks.Held(key) {
		t.Fatal("busy attempt released foreign lease")
	}
	got, err = e.m.ReserveDeployment(e.ctx, "missing", "busy")
	if err != nil || got.HTTPStatus == nil || *got.HTTPStatus != 404 {
		t.Fatal("validation must precede admission", got, err)
	}
	if lockx.Violations() != 0 {
		t.Fatal(lockx.Violations())
	}
}

func TestPreparationRequestDisconnectDuringCommitStillSucceeds(t *testing.T) {
	e := newEnv(t)
	runtime := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	runtime.Start(e.ctx)
	defer runtime.Shutdown(context.Background())
	request, disconnect := context.WithCancel(runtime.BindRequest(e.ctx))
	defer disconnect()
	const id = "disconnect-at-commit"
	beforeDB, err := e.repo.Store().Fingerprint()
	if err != nil {
		t.Fatal(err)
	}
	beforeRevision := e.revision()
	committing := false
	e.repo.Store().BeforeCommit = func() error {
		committing = true
		if held, err := e.m.defaultPreparationRunning(e.id(), id); err != nil || !held {
			t.Error("lease missing during commit", err)
		}
		disconnect()
		return nil
	}
	defer func() { e.repo.Store().BeforeCommit = nil }()
	result, err := e.m.ReserveDeployment(request, e.id(), id)
	e.repo.Store().BeforeCommit = nil
	if !committing || !errors.Is(request.Err(), context.Canceled) || lifecycle.Detach(request).Err() != nil || err != nil || !deploymentFlagTrue(result, "success") {
		t.Fatal("request disconnect prevented preparation commit", result, err)
	}
	afterDB, err := e.repo.Store().Fingerprint()
	agent := e.stored(e.id())
	dep, _ := agent.Deployments.Get(id)
	if err != nil || beforeDB == afterDB || e.revision() <= beforeRevision || dep.Status != "preparing" || agent.LatestDeployment == nil || agent.LatestDeployment.DeploymentID != id {
		t.Fatal("disconnected preparation state or revision was not committed", err)
	}
	if held, err := e.m.defaultPreparationRunning(e.id(), id); err != nil || !held {
		t.Fatal("request disconnect released the live preparation", held, err)
	}
	e.finish(id)
	preparationIdle(t, e.m, e.id(), id)
}
