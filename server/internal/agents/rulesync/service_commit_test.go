package rulesync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/taskstatus"
)

// Hold the real process-wide task-status leaf lock through its clock seam,
// without adding a production lock hook or replacing the recording path.
func holdTaskStatusLock(t *testing.T) func() {
	t.Helper()
	entered, release := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	store := taskstatus.New(t.TempDir())
	store.Now = func() time.Time {
		close(entered)
		<-release
		return time.Now()
	}
	go func() {
		_, err := store.SaveResult(context.Background(), "lock-holder", taskstatus.Result{OK: true})
		done <- err
	}()
	var once sync.Once
	unlock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unlock()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("release task-status lock: %v", err)
			}
		case <-time.After(30 * time.Second):
			t.Error("task-status lock holder did not finish")
		}
	})
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("task-status lock was not acquired")
	}
	return unlock
}

func waitForStateTaskStatusLock(t *testing.T) {
	t.Helper()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	stack := make([]byte, 64<<10)
	for {
		n := runtime.Stack(stack, true)
		if n == len(stack) {
			stack = make([]byte, 2*len(stack))
			continue
		}
		for _, goroutine := range strings.Split(string(stack[:n]), "\n\n") {
			if strings.Contains(goroutine, "rulesync.(*Service).saveState") &&
				strings.Contains(goroutine, "taskstatus.(*Store).locked") &&
				strings.Contains(goroutine, "sync.(*Mutex).Lock") {
				return
			}
		}
		select {
		case <-tick.C:
		case <-deadline.C:
			t.Fatal("state owner never waited on the task-status leaf lock")
		}
	}
}

func optionalReceipt(t *testing.T, dir, name string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return raw
}

func TestStateTaskStatusLockWaitRuntimeBoundary(t *testing.T) {
	for _, status := range []string{"succeeded", "failed"} {
		for _, shutdown := range []bool{true, false} {
			name := status + "/disconnect"
			if shutdown {
				name = status + "/shutdown"
			}
			t.Run(name, func(t *testing.T) {
				ctx, s := fixtureService(t)
				dir := s.m.Repository().DataDir()
				if _, err := taskstatus.New(dir).SaveResult(context.Background(), "existing", taskstatus.Result{OK: true}); err != nil {
					t.Fatal(err)
				}
				beforeStatus := optionalReceipt(t, dir, taskstatus.Filename)
				beforeNotifications := optionalReceipt(t, dir, "notifications.json")
				rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: time.Second})
				rt.Start(ctx)
				t.Cleanup(func() { rt.Shutdown(context.Background()) })
				request, disconnect := context.WithCancel(rt.BindRequest(ctx))
				defer disconnect()
				unlock := holdTaskStatusLock(t)
				done, owner := make(chan error, 1), make(chan context.Context, 1)
				if err := rt.Spawn(request, "state-task-status", func(ctx context.Context) {
					ctx = lifecycle.Detach(ctx)
					owner <- ctx
					done <- s.saveState(ctx, "device", model.RuleSyncState{Status: status, Message: msgFailed})
				}); err != nil {
					t.Fatal(err)
				}
				work := <-owner
				// This observes the actual mutex wait, rather than relying on a
				// scheduling delay that might cancel before RecordContext starts.
				waitForStateTaskStatusLock(t)
				beforeDB := systemBytes(t, s)
				stopped := make(chan struct{})
				if shutdown {
					go func() { rt.Shutdown(context.Background()); close(stopped) }()
					select {
					case <-work.Done():
					case <-time.After(30 * time.Second):
						t.Fatal("Runtime did not cancel the owner")
					}
				} else {
					disconnect()
					if err := work.Err(); err != nil {
						t.Fatalf("request disconnect canceled owner: %v", err)
					}
				}
				unlock()
				var err error
				select {
				case err = <-done:
				case <-time.After(30 * time.Second):
					t.Fatal("state owner did not finish after lock release")
				}
				if shutdown {
					<-stopped
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("shutdown task-status write returned %v", err)
					}
					if !bytes.Equal(beforeStatus, optionalReceipt(t, dir, taskstatus.Filename)) ||
						!bytes.Equal(beforeNotifications, optionalReceipt(t, dir, "notifications.json")) {
						t.Fatal("shutdown wrote task status or notifications after lock wait")
					}
				} else {
					if err != nil {
						t.Fatal(err)
					}
					var rows map[string]struct {
						Status string `json:"last_status"`
						Runs   int    `json:"run_count"`
					}
					if err := json.Unmarshal(optionalReceipt(t, dir, taskstatus.Filename), &rows); err != nil {
						t.Fatal(err)
					}
					want := "success"
					if status == "failed" {
						want = "failed"
					}
					if row := rows["agent_rule_sync:device"]; row.Runs != 1 || row.Status != want {
						t.Fatalf("disconnected owner did not record exactly once: %+v", row)
					}
				}
				if !bytes.Equal(beforeDB, systemBytes(t, s)) {
					t.Fatal("task-status completion changed the committed database")
				}
			})
		}
	}
}
