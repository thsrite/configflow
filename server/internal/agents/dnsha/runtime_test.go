package dnsha

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

type runtimeControl struct{}

func (*runtimeControl) BindRequest(ctx context.Context) context.Context {
	return lifecycle.BindRequest(ctx)
}
func (*runtimeControl) Shutdown(context.Context) { lifecycle.Stop(100 * time.Millisecond) }
func newRuntime(t *testing.T, ctx context.Context) *runtimeControl {
	t.Helper()
	lifecycle.Stop(time.Second)
	if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 25 * time.Millisecond}); e != nil {
		t.Fatal(e)
	}
	lifecycle.Start(ctx)
	t.Cleanup(func() {
		lifecycle.Stop(time.Second)
		if e := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
			t.Error(e)
		}
	})
	return &runtimeControl{}
}

func durable(t *testing.T, f *fixture) map[string]string {
	t.Helper()
	row, e := f.r.Store().Load()
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(row)
	if e != nil {
		t.Fatal(e)
	}
	out := map[string]string{"@DB": string(b)}
	e = filepath.WalkDir(f.r.DataDir(), func(path string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(f.r.DataDir(), path)
		if d.IsDir() || strings.HasPrefix(rel, "configflow.sqlite3") {
			return nil
		}
		b, e := os.ReadFile(path)
		if e == nil {
			out[rel] = string(b)
		}
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	return out
}
func TestStoppedRuntimeRefusesEveryHAWrite(t *testing.T) {
	f := fixtureNew(t, Options{}, true, nil)
	r := newRuntime(t, f.ctx)
	ctx := r.BindRequest(f.ctx)
	r.Shutdown(context.Background())
	before := durable(t, f)
	calls := len(f.client.calls())
	b := f.bundle(t, "primary", "stopped", false)
	entries := []struct {
		name string
		call func() error
	}{
		{"configure", func() error { _, e := f.s.Configure(ctx, "primary", []byte(`{}`), ""); return e }},
		{"disable", func() error { _, e := f.s.Disable(ctx, "primary", true); return e }},
		{"advance", func() error { _, e := f.s.Advance(ctx, "primary"); return e }},
		{"status", func() error { _, e := f.s.Status(ctx, "primary"); return e }},
		{"publish", func() error { _, _, e := f.s.PublishPair(ctx, "primary", b, zeroPublish()); return e }},
		{"activate", func() error { _, e := f.s.ActivatePair(ctx, "primary", "stopped"); return e }},
		{"deployment", func() error { _, e := f.s.PairDeployment(ctx, "primary", "stopped"); return e }},
		{"rules", func() error { _, _, e := f.s.PairRules(ctx, "primary", "POST"); return e }},
		{"service", func() error {
			_, e := f.s.ServiceOperationJSON(ctx, "primary", func() (json.RawMessage, error) { t.Fatal("stopped service operation ran"); return nil, nil }, false)
			return e
		}},
	}
	for _, entry := range entries {
		t.Run(entry.name, func(t *testing.T) {
			if e := entry.call(); !errors.Is(e, context.Canceled) {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(before, durable(t, f)) || len(f.client.calls()) != calls {
				t.Fatal("late HA write or I/O")
			}
		})
	}
}
func TestDisconnectedRequestCompletesEnableAndPair(t *testing.T) {
	f := fixtureNew(t, Options{}, false, nil)
	r := newRuntime(t, f.ctx)
	request, cancel := context.WithCancel(f.ctx)
	ctx := r.BindRequest(request)
	cancel()
	if _, e := f.s.Configure(ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "https://panel.example"); e != nil {
		t.Fatal(e)
	}
	for range 12 {
		last, e := f.s.Advance(ctx, "primary")
		if e != nil {
			t.Fatal(e)
		}
		if last != nil && last.Status == "succeeded" {
			return
		}
	}
	t.Fatal("disconnected accepted enable did not finish")
}
func TestShutdownAtAcceptedDeviceWriteResumesJournal(t *testing.T) {
	for _, point := range []string{"upload", "yield", "keepalived-secondary", "keepalived-primary"} {
		t.Run(point, func(t *testing.T) {
			enable := strings.HasPrefix(point, "keepalived")
			f := fixtureNew(t, Options{}, !enable, nil)
			if enable {
				if _, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "https://panel.example"); e != nil {
					t.Fatal(e)
				}
			}
			r := newRuntime(t, f.ctx)
			ctx := r.BindRequest(f.ctx)
			arrived, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var gateOnce sync.Once
			unblock := func() { gateOnce.Do(func() { close(release) }) }
			f.client.setAfter(func(ctx context.Context, req agents.Request, _ *agents.Response) error {
				hit := point == "upload" && req.Method == "POST" && strings.HasSuffix(req.URL, "/api/deployments") || point == "yield" && req.Method == "POST" && strings.HasSuffix(req.URL, "/handoff") || point == "keepalived-secondary" && req.Method == "PUT" && strings.Contains(req.URL, secondaryHost) || point == "keepalived-primary" && req.Method == "PUT" && strings.Contains(req.URL, primaryHost)
				if hit {
					once.Do(func() { close(arrived); <-release })
				}
				return nil
			})
			done := make(chan error, 1)
			finished := make(chan struct{})
			t.Cleanup(func() {
				unblock()
				select {
				case <-finished:
				case <-time.After(time.Minute):
					t.Error("owner failed to drain")
				}
			})
			go func() {
				defer close(finished)
				if !enable {
					b := f.bundle(t, "primary", "shutdown-release", false)
					_, _, e := f.s.PublishPair(ctx, "primary", b, zeroPublish())
					if e != nil {
						done <- e
						return
					}
				}
				for range 16 {
					if _, e := f.s.Advance(ctx, "primary"); e != nil {
						done <- e
						return
					}
				}
				done <- nil
			}()
			select {
			case <-arrived:
			case e := <-done:
				t.Fatal("fault did not reach device", e)
			case <-time.After(time.Minute):
				t.Fatal("owner stalled before device")
			}
			r.Shutdown(context.Background())
			before := durable(t, f)
			unblock()
			select {
			case e := <-done:
				if !errors.Is(e, context.Canceled) {
					t.Fatal("cancel result", e)
				}
			case <-time.After(time.Minute):
				t.Fatal("owner ignored shutdown")
			}
			if !reflect.DeepEqual(before, durable(t, f)) {
				t.Fatal("late failure, undo or half-pair commit")
			}

			f.client.setAfter(nil)
			// New Runtime and owner resume persisted intent, including accepted yield/apply.
			restart := newRuntime(t, f.ctx)
			f.s = New(f.m, Options{})
			var status string
			for range 16 {
				j, e := f.s.Advance(restart.BindRequest(f.ctx), "primary")
				if e != nil {
					t.Fatal(e)
				}
				if j != nil {
					status = j.Status
					if terminal(status) {
						break
					}
				}
			}
			if status != "succeeded" || len(f.uploads()) != 2 {
				t.Fatal(status, len(f.uploads()))
			}
			a, _ := f.s.get(f.ctx, "primary")
			b, _ := f.s.get(f.ctx, "secondary")
			if !paired(a, b) {
				t.Fatal("restart left partial pair")
			}
			if point == "yield" {
				n := 0
				for _, c := range f.segment.Calls() {
					if c.Method == "POST" && c.Path == "/api/oxidns-ha/handoff" {
						n++
					}
				}
				if n != 1 {
					t.Fatal("yield was reposted", n)
				}
			}
		})
	}
}
func TestKeepalivedSaveFailureUndoesBothAndShutdownDoesNotCommit(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(map[bool]string{false: "storage-error", true: "shutdown-final-commit"}[shutdown], func(t *testing.T) {
			f := fixtureNew(t, Options{}, false, nil)
			r := newRuntime(t, f.ctx)
			ctx := r.BindRequest(f.ctx)
			armed := false
			f.client.setAfter(func(callCtx context.Context, req agents.Request, _ *agents.Response) error {
				if req.Method == "PUT" && strings.Contains(req.URL, primaryHost) {
					armed = true
					f.r.Store().BeforeCommit = func() error {
						f.r.Store().BeforeCommit = nil
						if shutdown {
							r.Shutdown(context.Background())
							return nil
						}
						return errors.New("synthetic storage failure")
					}
				}
				return nil
			})
			e := deploy.RunDetached(ctx, "keepalived-test", func(ctx context.Context) error { return f.s.configureKeepalived(ctx, "primary", &enableIntent, nil) })
			f.r.Store().BeforeCommit = nil
			if !armed {
				t.Fatal("save fault did not fire")
			}
			if shutdown {
				if !errors.Is(e, context.Canceled) {
					t.Fatal(e)
				}
			} else if e == nil || e.Error() != msgSaveFailed {
				t.Fatal(e)
			}
			a, _ := f.s.get(f.ctx, "primary")
			b, _ := f.s.get(f.ctx, "secondary")
			if haEnabled(a) || haEnabled(b) {
				t.Fatal("failed commit enabled one side")
			}
			if !shutdown {
				deletes := []string{}
				for _, c := range f.segment.Calls() {
					if c.Method == "DELETE" {
						deletes = append(deletes, c.AgentID)
					}
				}
				if !reflect.DeepEqual(deletes, []string{"primary", "secondary"}) {
					t.Fatal("undo order", deletes)
				}
			}
		})
	}
}

func zeroPublish() deploy.PublishOptions { return deploy.PublishOptions{Activate: true} }

var enableIntent = model.HAEnable{PeerID: "secondary", VIP: vip, VRID: 53}
