package dnsfollow

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/lifecycle"
)

func startRuntime(t *testing.T, ctx context.Context) *lifecycle.Runtime {
	t.Helper()
	rt := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	rt.Start(ctx)
	t.Cleanup(func() { rt.Stop(time.Second) })
	return rt
}
func durable(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	entry, err := f.r.Store().Load()
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{"@DB": string(b)}
	err = filepath.WalkDir(f.r.DataDir(), func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(f.r.DataDir(), p)
		if d.IsDir() || strings.HasPrefix(rel, "configflow.sqlite3") {
			return nil
		}
		b, e := os.ReadFile(p)
		out[rel] = string(b)
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestRuntimeStoppedRejectsEveryFollowWrite(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	rt := startRuntime(t, f.ctx)
	ctx := rt.BindRequest(f.ctx)
	rt.Stop(time.Second)
	before := durable(t, f)
	calls := len(f.source.fake.Calls()) + len(f.target.fake.Calls())
	dnsCalls := len(f.source.paths) + len(f.target.paths)
	checks := []struct {
		name string
		run  func() error
	}{
		{"configure", func() error { _, e := f.s.Configure(ctx, "target", json.RawMessage(`{}`)); return e }},
		{"disable", func() error { _, e := f.s.Disable(ctx, "target"); return e }},
		{"retry", func() error { _, e := f.s.Retry(ctx, "target"); return e }},
		{"tick", func() error { return f.s.Tick(ctx) }},
	}
	for _, tc := range checks {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.run(); !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(before, durable(t, f)) || len(f.source.fake.Calls())+len(f.target.fake.Calls()) != calls || len(f.source.paths)+len(f.target.paths) != dnsCalls {
				t.Fatal("post-stop write/network")
			}
		})
	}
}
func TestDisconnectedBoundRequestFinishesBindingAndFollow(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	rt := startRuntime(t, f.ctx)
	req, cancel := context.WithCancel(f.ctx)
	ctx := rt.BindRequest(req)
	cancel()
	if _, e := f.s.Configure(ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`)); e != nil {
		t.Fatal(e)
	}
	f.publishSource(t, "source-next")
	if e := f.s.Tick(ctx); e != nil {
		t.Fatal(e)
	}
	if state(f.agent(t)).Status != "succeeded" || f.target.uploads() != 2 {
		t.Fatal(state(f.agent(t)))
	}
}

type stopAfterReply struct {
	next             agents.Client
	arrived, release chan struct{}
	path             string
}

func (c *stopAfterReply) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	reply, err := c.next.Do(ctx, r)
	if strings.Contains(r.URL, c.path) {
		close(c.arrived)
		<-c.release
	}
	return reply, err
}
func TestShutdownDuringFollowIOLeavesNoLateCommit(t *testing.T) {
	for _, path := range []string{"/api/dns/listener", "/api/deployments"} {
		t.Run(path, func(t *testing.T) {
			f := newFixture(t, Options{}, agentfake.Options{})
			f.bind(t)
			f.publishSource(t, "source-next")
			rt := startRuntime(t, f.ctx)
			c := &stopAfterReply{next: agents.HTTPClient{}, arrived: make(chan struct{}), release: make(chan struct{}), path: path}
			if path == "/api/deployments" {
				c.path = "/api/deployments"
			} // Only POST is intercepted below for accepted uploads.
			f.m.SetClient(&followStopClient{c: c})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(c.release) }) }
			finished := make(chan struct{})
			t.Cleanup(func() {
				release()
				select {
				case <-finished:
				case <-time.After(30 * time.Second):
					t.Error("follow owner did not finish during cleanup")
				}
			})
			done := make(chan error, 1)
			go func() { defer close(finished); done <- f.s.Tick(rt.BindRequest(f.ctx)) }()
			select {
			case <-c.arrived:
			case err := <-done:
				t.Fatal("owner exited before real I/O", err)
			case <-time.After(60 * time.Second):
				t.Fatal("owner did not reach real I/O")
			}
			rt.Stop(20 * time.Millisecond)
			before := durable(t, f)
			release()
			if e := <-done; !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(before, durable(t, f)) {
				t.Fatal("late state/job/profile/taskstatus commit")
			}
			if path == "/api/deployments" {
				a := f.agent(t)
				id := value(state(a).TargetDeploymentID)
				child, ok := a.Deployments.Get(id)
				if !ok || !value(child.Submitted) || f.target.uploads() != 2 {
					t.Fatal("uncertain submission was not durable")
				}
			}
		})
	}
}

type followStopClient struct{ c *stopAfterReply }

func (c *followStopClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	if c.c.path == "/api/deployments" && r.Method != http.MethodPost {
		return c.c.next.Do(ctx, r)
	}
	return c.c.Do(ctx, r)
}
