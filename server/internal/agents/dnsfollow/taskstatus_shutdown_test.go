package dnsfollow

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
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
			if _, err = store.Record("dns_follow:target", taskstatus.Result{OK: true}); err != nil {
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
				value, err := jsonx.Parse(data)
				if err != nil {
					t.Fatal(err)
				}
				row := value.(*jsonx.Object).GetObject("dns_follow:target")
				if row.GetInt("run_count") != 2 || row.GetString("last_trigger") != "event" || row.GetString("last_status") != "success" {
					t.Fatal("disconnected outcome did not commit once", row)
				}
			}
		})
	}
}

func TestTaskRowPropagatesInvalidHistory(t *testing.T) {
	raw := jsonx.Obj("id", "target", "service_type", "oxidns", "dns_follow", jsonx.Obj("id", "binding"))
	statuses := jsonx.Obj("dns_follow:target", jsonx.Obj("run_count", jsonx.Obj("invalid", true)))
	if _, err := taskRow(t.Context(), raw, statuses); !errors.Is(err, tasks.ErrInvalidHistory) {
		t.Fatal("invalid history was ignored", err)
	}
}
