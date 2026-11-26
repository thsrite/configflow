package contentsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"configflow/internal/deploy"
	"configflow/internal/lifecycle/lifecycletest"
	"configflow/internal/model"
)

type scheduled struct {
	ctx context.Context
	run func(context.Context)
}
type deferred struct {
	mu     sync.Mutex
	jobs   []scheduled
	reject bool
}

func (d *deferred) Spawn(ctx context.Context, _ string, fn func(context.Context)) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.reject {
		d.reject = false
		return fmt.Errorf("synthetic private failure")
	}
	d.jobs = append(d.jobs, scheduled{ctx, fn})
	return nil
}
func (d *deferred) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.jobs) }
func (d *deferred) run(t *testing.T) {
	t.Helper()
	d.mu.Lock()
	if len(d.jobs) == 0 {
		d.mu.Unlock()
		t.Fatal("no job")
	}
	job := d.jobs[0]
	d.jobs = d.jobs[1:]
	d.mu.Unlock()
	job.run(job.ctx)
}

type fixture struct {
	t          *testing.T
	renders    map[deploy.InputKey][2]string
	renderMu   sync.Mutex
	c          *Checker
	clock      *lifecycletest.Clock
	spawn      *deferred
	cfg, agent map[string]any
	calls      int
	baseline   *deploy.Bundle
	prepare    PrepareFunc
	ctx        context.Context
}

func pack(t *testing.T, main, rules string) *deploy.Bundle {
	t.Helper()
	files := map[string]deploy.Content{"config.yaml": {Data: []byte(main), Role: deploy.RoleConfig}, "rules/example.list": {Data: []byte(rules), Role: deploy.RoleRule}}
	m := deploy.Manifest{ProtocolVersion: 1, DeploymentID: "synthetic", AgentID: "agent-test", ProfileID: "default", ServiceType: "mihomo", ConfigPath: "config.yaml"}
	for _, p := range []string{"config.yaml", "rules/example.list"} {
		v := files[p]
		sum := sha256.Sum256(v.Data)
		m.Files = append(m.Files, deploy.File{Path: p, Role: v.Role, Size: int64(len(v.Data)), SHA256: hex.EncodeToString(sum[:])})
	}
	b, err := deploy.Pack(m, files, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func newFixture(t *testing.T, change func(*Options)) *fixture {
	t.Helper()
	f := &fixture{t: t, renders: map[deploy.InputKey][2]string{}, clock: lifecycletest.NewClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)), spawn: &deferred{}, ctx: context.Background()}
	f.cfg = map[string]any{"profile_id": "default", "test_main": "mode: rule\nrules: []\n", "test_rule": "DOMAIN,example.test\n"}
	f.baseline = pack(t, f.cfg["test_main"].(string), f.cfg["test_rule"].(string))
	fp, err := deploy.Fingerprint(f.baseline.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	digests, err := deploy.Digests(f.baseline.Manifest())
	if err != nil {
		t.Fatal(err)
	}
	rawDigests := map[string]any{}
	for k, v := range digests {
		rawDigests[k] = v
	}
	f.agent = map[string]any{
		"id": "agent-test", "profile_id": "default", "service_type": "mihomo", "config_version": "applied-config",
		"applied_content": map[string]any{
			"fingerprint": fp, "profile_id": "default", "service_type": "mihomo", "config_version": "applied-config",
			"deployment_id": "previous", "base_url": "https://config.example.test", "config_path": "config.yaml", "files": rawDigests,
		},
	}
	f.prepare = func(_ context.Context, _ deploy.Target, s *deploy.Source, _, _ string) (*deploy.Bundle, error) {
		f.calls++
		key, err := s.InputKey()
		if err != nil {
			return nil, err
		}
		f.renderMu.Lock()
		pair := f.renders[key]
		f.renderMu.Unlock()
		return pack(t, pair[0], pair[1]), nil
	}
	opts := Options{Clock: f.clock, Spawner: f.spawn, Prepare: func(ctx context.Context, target deploy.Target, s *deploy.Source, url, id string) (*deploy.Bundle, error) {
		return f.prepare(ctx, target, s, url, id)
	}, LoadReceipt: func(context.Context, deploy.Target) (*deploy.Receipt, error) {
		return &deploy.Receipt{Manifest: f.baseline.Manifest(), Bundle: f.baseline}, nil
	}}
	if change != nil {
		change(&opts)
	}
	f.c = New(opts)
	t.Cleanup(f.c.Close)
	return f
}
func (f *fixture) target() deploy.Target {
	raw, err := json.Marshal(f.agent)
	if err != nil {
		f.t.Fatal(err)
	}
	var agent model.Agent
	if err := json.Unmarshal(raw, &agent); err != nil {
		f.t.Fatal(err)
	}
	target, err := deploy.NewTypedTarget(&agent)
	if err != nil {
		f.t.Fatal(err)
	}
	return target
}
func (f *fixture) source() *deploy.Source {
	raw, err := json.Marshal(f.cfg)
	if err != nil {
		f.t.Fatal(err)
	}
	source, err := deploy.SourceFromJSON(raw, nil)
	if err != nil {
		f.t.Fatal(err)
	}
	key, err := source.InputKey()
	if err != nil {
		f.t.Fatal(err)
	}
	mainText, _ := f.cfg["test_main"].(string)
	ruleText, _ := f.cfg["test_rule"].(string)
	f.renderMu.Lock()
	f.renders[key] = [2]string{mainText, ruleText}
	f.renderMu.Unlock()
	return source
}
func (f *fixture) status() Result {
	return f.c.Status(f.ctx, f.target(), f.source(), "https://config.example.test")
}
func (f *fixture) diff() Answer {
	return f.c.PushDiff(f.ctx, f.target(), f.source(), "https://config.example.test")
}
func requireStatus(t *testing.T, r Result, status, reason string) {
	t.Helper()
	if r.Status != status || r.Reason != reason {
		t.Fatalf("got %+v want %s/%s", r, status, reason)
	}
}
