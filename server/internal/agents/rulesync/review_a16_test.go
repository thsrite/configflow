package rulesync

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// Python's worker calls prepare_candidate without force_refresh: the flag a
// failure leaves in the state only resumes an interrupted refresh. Passing it
// to candidate preparation disables live-byte reuse and, with the library scope
// off, unpins and re-downloads rule files the user did not opt in to.
func TestStructurePublicationIgnoresStoredForceRefresh(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	if _, e := f.s.Configure(f.ctx, "device", []byte(`{"library":false,"rules":true}`)); e != nil {
		t.Fatal(e)
	}
	var forced []bool
	prepare := f.s.prepare
	f.s.prepare = func(ctx context.Context, target deploy.Target, source *deploy.Source, baseline Baseline, settings Settings, id string, force bool) (*deploy.Bundle, error) {
		forced = append(forced, force)
		return prepare(ctx, target, source, baseline, settings, id, force)
	}
	// A previous round failed: Run's failure path always stores force_refresh.
	past := nowSeconds(f.now.Add(-time.Second))
	if e := persistSync(f.ctx, f.s.m, "device", nil, &model.RuleSyncState{Status: "failed", Message: msgFailed, RetryAt: &past, ForceRefresh: ptr(true)}); e != nil {
		t.Fatal(e)
	}
	_, e := f.s.m.Repository().UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		for i := range p.RuleConfigs {
			if p.RuleConfigs[i].ID == "single" {
				p.RuleConfigs[i].Value = "new-inline.example.com"
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if state := f.state(t); state.Status != "pending" || !value(state.ForceRefresh) {
		t.Fatalf("debounce state %+v", state)
	}
	f.now = f.now.Add(PollInterval)
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	if len(forced) != 1 {
		t.Fatalf("prepare calls %v, state %+v", forced, f.state(t))
	}
	if forced[0] {
		t.Fatal("stored force_refresh reached candidate preparation (Python passes none)")
	}
	for i := 0; i < 8 && f.state(t).Status == "running"; i++ {
		if e = f.s.Run(f.ctx, "device"); e != nil {
			t.Fatal(e)
		}
	}
	if state := f.state(t); state.Status != "succeeded" || len(forced) != 1 {
		t.Fatalf("publication state %s %s, prepare calls %d", state.Status, state.Message, len(forced))
	}
}

// Python skips a rule whose enabled/library_enabled is falsy (None, 0, "");
// the generators agree. Editing such a rule is not an effective change.
func TestFalsyEnabledRuleIsNotAnEffectiveChange(t *testing.T) {
	source := func(value string, enabled any) *deploy.Source {
		t.Helper()
		raw := map[string]any{
			"profile_id":   "default",
			"rule_library": []any{},
			"rule_configs": []any{
				map[string]any{"id": "single", "itemType": "rule", "rule_type": "DOMAIN", "value": value, "policy": "DIRECT", "enabled": enabled},
			},
			"oxidns": map[string]any{"direct_rules": []any{"single"}},
		}
		data, e := json.Marshal(raw)
		if e != nil {
			t.Fatal(e)
		}
		s, e := deploy.SourceFromJSON(data, nil)
		if e != nil {
			t.Fatal(e)
		}
		return s
	}
	for _, enabled := range []any{nil, 0, ""} {
		kind, _, _, e := RuleChanges(source("old.example.com", enabled), source("new.example.com", enabled), "mihomo", Settings{Rules: true})
		if e != nil {
			t.Fatal(e)
		}
		if kind != None {
			t.Errorf("enabled %#v: kind %s, Python none", enabled, kind)
		}
	}
	kind, _, _, e := RuleChanges(source("old.example.com", true), source("new.example.com", true), "mihomo", Settings{Rules: true})
	if e != nil || kind != Structure {
		t.Fatalf("negative control: kind %s %v", kind, e)
	}
}

type snapshotBlocker struct {
	inner   agents.Client
	block   atomic.Bool
	entered chan struct{}
}

func (c *snapshotBlocker) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	if strings.HasSuffix(r.URL, "/api/rule-sync/snapshot") && c.block.Load() {
		c.entered <- struct{}{}
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return c.inner.Do(ctx, r)
}

// A shutdown while a structural publication reads the live snapshot lands no
// write after it (no failure record, no state), and the next process converges
// through A01's interrupted-preparation classification without a second upload.
func TestShutdownDuringStructurePublicationConverges(t *testing.T) {
	f := newWorkerFixture(t, "mihomo")
	if _, e := f.s.Configure(f.ctx, "device", []byte(`{"library":false,"rules":true}`)); e != nil {
		t.Fatal(e)
	}
	_, e := f.s.m.Repository().UpdateProfile(f.ctx, "default", func(_ context.Context, p *model.Profile) error {
		for i := range p.RuleConfigs {
			if p.RuleConfigs[i].ID == "single" {
				p.RuleConfigs[i].Value = "new-inline.example.com"
			}
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(PollInterval)
	blocker := &snapshotBlocker{inner: f.fake, entered: make(chan struct{}, 1)}
	blocker.block.Store(true)
	f.s.m.SetClient(blocker)
	lifecycle.Stop(4 * time.Second)
	if e = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); e != nil {
		t.Fatal(e)
	}
	lifecycle.Start(f.ctx)
	t.Cleanup(func() {
		lifecycle.Stop(4 * time.Second)
		_ = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	})
	done := make(chan error, 1)
	go func() { done <- f.s.Run(lifecycle.BindRequest(f.ctx), "device") }()
	select {
	case <-blocker.entered:
	case <-time.After(30 * time.Second):
		t.Fatalf("publication did not read the snapshot, state %+v", f.state(t))
	}
	before := systemBytes(t, f.s)
	state := f.state(t)
	if state.Status != "running" || value(state.DeploymentID) == "" {
		t.Fatalf("running state %s %s", state.Status, state.Message)
	}
	id := value(state.DeploymentID)
	lifecycle.Stop(4 * time.Second)
	if e = <-done; e == nil {
		t.Fatal("shutdown publication succeeded")
	}
	if !bytes.Equal(before, systemBytes(t, f.s)) {
		t.Fatal("a write landed after shutdown")
	}
	// Next process: fresh Runtime and owner, the device answers again.
	_ = lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()})
	lifecycle.Start(f.ctx)
	blocker.block.Store(false)
	resumed := New(f.s.m, nil, Options{Now: func() time.Time { return f.now }})
	if e = resumed.Run(f.ctx, "device"); e != nil {
		t.Fatal(e)
	}
	r, e := f.s.m.GetDeploymentModel(f.ctx, "device", id)
	if e != nil {
		t.Fatal(e)
	}
	if r.Status != "failed" {
		t.Fatalf("interrupted preparation %s", r.Status)
	}
	if state = f.state(t); state.Status != "failed" || state.Message != msgReloadFailed || value(state.RetryAt) == 0 {
		t.Fatalf("resumed state %s %s", state.Status, state.Message)
	}
	for _, call := range f.agent.Calls() {
		if call.Manifest != nil && call.Manifest.DeploymentID == id {
			t.Fatal("interrupted candidate was uploaded")
		}
	}
}

// R5: stored rule_sync members of the wrong type read as absent; reads and
// a worker round neither fail nor rewrite unrelated members.
func TestWrongTypeRuleSyncMembersReadAsAbsent(t *testing.T) {
	shapes := []struct {
		name  string
		value any
		on    bool
	}{
		{"null", nil, false},
		{"list", []any{}, false},
		{"string", "x", false},
		{"settings list", map[string]any{"settings": []any{true}}, false},
		{"state null", map[string]any{"settings": map[string]any{"library": true, "rules": false}, "state": nil}, true},
		{"state list", map[string]any{"settings": map[string]any{"library": true, "rules": false}, "state": []any{"x"}}, true},
		{"state wrong members", map[string]any{"settings": map[string]any{"library": true, "rules": false}, "state": map[string]any{"status": 5, "retry_at": "soon", "noticed_at": nil, "native_task": 7, "force_refresh": "yes"}}, true},
	}
	for _, tt := range shapes {
		t.Run(tt.name, func(t *testing.T) {
			ctx, s := fixtureService(t)
			raw, err := json.Marshal(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			e := s.m.Repository().UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
				return sys.Agents[0].SetJSON("rule_sync", raw)
			})
			if e != nil {
				t.Fatal(e)
			}
			view, e := s.Status(ctx, "device")
			if e != nil {
				t.Fatal(e)
			}
			if view.Settings.Any() != tt.on {
				t.Fatalf("settings %+v", view.Settings)
			}
			if e = s.Run(ctx, "device"); e != nil {
				t.Fatalf("worker: %v", e)
			}
			if _, e = s.Configure(ctx, "device", []byte(`{"library":false,"rules":true}`)); e != nil {
				t.Fatalf("configure: %v", e)
			}
		})
	}
}
