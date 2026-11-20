package dnsfollow

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
)

// outcomeCheckPause models preemption after the owner read Err but before it
// entered the task-status leaf store. It returns that actual observed error;
// Value and the Runtime's recorded work lifetime are unchanged.
type outcomeCheckPause struct {
	context.Context
	once            sync.Once
	checked, resume chan struct{}
}

func (p *outcomeCheckPause) Err() error {
	err := p.Context.Err()
	p.once.Do(func() { close(p.checked); <-p.resume })
	return err
}

func TestTaskStatusRetainsOwnerLifetimeAfterPrecheck(t *testing.T) {
	for _, shutdown := range []bool{true, false} {
		name := "disconnect"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			r, err := repo.OpenDefault(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			ctx := repo.WithScope(repo.WithRepository(t.Context(), r))
			m, err := agents.New(ctx, r)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(m.Close)
			f := &fixture{ctx: ctx, r: r, m: m, s: New(m, Options{})}
			store := taskstatus.New(r.DataDir())
			if _, err = store.SaveResult(context.Background(), "dns_follow:target", taskstatus.Result{OK: true}); err != nil {
				t.Fatal(err)
			}
			startRuntime(t, ctx)
			request, disconnect := context.WithCancel(ctx)
			defer disconnect()
			bound := lifecycle.BindRequest(request)
			checked, resume := make(chan struct{}), make(chan struct{})
			var release sync.Once
			releaseWriter := func() { release.Do(func() { close(resume) }) }
			defer releaseWriter()
			done := make(chan error, 1)
			go func() {
				done <- deploy.RunDetached(bound, "dns-follow-outcome", func(work context.Context) error {
					paused := &outcomeCheckPause{Context: work, checked: checked, resume: resume}
					return f.s.recordOutcome(paused, "target", "succeeded", msgSucceeded)
				})
			}()
			select {
			case <-checked:
			case <-time.After(10 * time.Second):
				t.Fatal("owner never reached its outcome check")
			}
			if shutdown {
				lifecycle.Stop(20 * time.Millisecond)
			} else {
				disconnect()
			}
			before := durable(t, f)
			releaseWriter()
			select {
			case err = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("outcome owner did not finish")
			}
			if shutdown {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("post-check shutdown returned %v", err)
				}
				if !reflect.DeepEqual(before, durable(t, f)) {
					t.Fatal("task status or notification committed after Runtime shutdown")
				}
			} else {
				if err != nil {
					t.Fatal("browser disconnect canceled accepted outcome", err)
				}
				data, err := os.ReadFile(filepath.Join(r.DataDir(), taskstatus.Filename))
				if err != nil {
					t.Fatal(err)
				}
				var stored map[string]struct {
					RunCount    int    `json:"run_count"`
					LastTrigger string `json:"last_trigger"`
					LastStatus  string `json:"last_status"`
				}
				if err := json.Unmarshal(data, &stored); err != nil {
					t.Fatal(err)
				}
				row := stored["dns_follow:target"]
				if row.RunCount != 2 || row.LastTrigger != "event" || row.LastStatus != "success" {
					t.Fatal("disconnected outcome did not commit once", row)
				}
			}
		})
	}
}

func TestTaskRowPropagatesInvalidHistory(t *testing.T) {
	var a model.Agent
	if err := json.Unmarshal([]byte(`{"id":"target","service_type":"oxidns","dns_follow":{"id":"binding"}}`), &a); err != nil {
		t.Fatal(err)
	}
	var statuses taskstatus.Snapshot
	if err := json.Unmarshal([]byte(`{"dns_follow:target":{"run_count":{"invalid":true}}}`), &statuses); err != nil {
		t.Fatal(err)
	}
	if _, err := agentTaskRow(t.Context(), &a, statuses); !errors.Is(err, tasks.ErrInvalidHistory) {
		t.Fatal("invalid history was ignored", err)
	}
}
