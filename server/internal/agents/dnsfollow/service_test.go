package dnsfollow

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
)

const nativeDraft = `runtime:
  worker_threads: 2
plugins:
  - tag: forward_remote
    type: forward
    args:
      concurrent: 1
      upstreams:
        - addr: udp://192.0.2.10:5353
          timeout: 3s
        - addr: udp://127.0.0.1:1053
          timeout: 5s
  - tag: forward_local
    type: forward
    args:
      upstreams:
        - addr: udp://192.0.2.11:53
`

type peer struct {
	mu                                          sync.Mutex
	server                                      *httptest.Server
	fake                                        *agentfake.Agent
	id, token, service                          string
	listenerHost                                string
	listenerPort                                int
	activeID                                    string
	listenerEnabled, peerMatches, activeMatches bool
	paths                                       []string
	hook                                        func(*http.Request)
}

func newPeer(t *testing.T, id, service string, o agentfake.Options) *peer {
	t.Helper()
	o.AgentID = id
	o.ServiceType = service
	o.Token = "synthetic-" + id + "-token"
	p := &peer{id: id, token: o.Token, service: service, fake: agentfake.New(o), listenerHost: "0.0.0.0", listenerPort: 2053, listenerEnabled: true, peerMatches: true, activeMatches: true}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.paths = append(p.paths, r.Method+" "+r.URL.RequestURI())
		hook := p.hook
		host, port, active, enabled, same, matches := p.listenerHost, p.listenerPort, p.activeID, p.listenerEnabled, p.peerMatches, p.activeMatches
		p.mu.Unlock()
		if hook != nil {
			hook(r)
		}
		if strings.HasPrefix(r.URL.Path, "/api/dns/") {
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+p.token || r.Header.Get("Content-Type") != "" || r.ContentLength > 0 {
				t.Error("invalid raw DNS request", r.Method, r.URL, r.Header, r.ContentLength)
			}
			var reply any
			switch r.URL.Path {
			case "/api/dns/listener":
				reply = map[string]any{"success": true, "agent_id": p.id, "deployment_id": active, "enabled": enabled, "host": host, "port": port}
			case "/api/dns/peer":
				id := "source"
				if !same {
					id = "other"
				}
				reply = map[string]any{"success": true, "peer_agent_id": id, "service_type": "mihomo"}
			case "/api/dns/active":
				if !matches {
					active = "other"
				}
				reply = map[string]any{"success": true, "agent_id": p.id, "deployment_id": active}
			default:
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(reply)
			return
		}
		p.fake.ServeHTTP(w, r)
		if r.Method == "POST" && r.URL.Path == "/api/deployments" {
			p.mu.Lock()
			p.activeID = r.Header.Get("X-Deployment-ID")
			p.mu.Unlock()
		}
	}))
	t.Cleanup(p.server.Close)
	return p
}
func (p *peer) record() *jsonx.Object {
	u, _ := url.Parse(p.server.URL)
	host, port, _ := net.SplitHostPort(u.Host)
	n, _ := strconv.Atoi(port)
	return jsonx.Obj("id", p.id, "name", p.service, "service_type", p.service, "host", host, "port", n, "token", p.token, "profile_id", "default", "enabled", true, "version", "1.8.60-go")
}
func (p *peer) uploads() int {
	n := 0
	for _, c := range p.fake.Calls() {
		if c.Method == "POST" && c.Path == "/api/deployments" {
			n++
		}
	}
	return n
}

type fixture struct {
	ctx            context.Context
	r              *repo.Repository
	m              *agents.Manager
	s              *Service
	source, target *peer
}

func newFixture(t *testing.T, o Options, targetOptions agentfake.Options) *fixture {
	t.Helper()
	r, e := repo.OpenDefault(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	ctx := repo.WithScope(repo.WithRepository(t.Context(), r))
	source := newPeer(t, "source", "mihomo", agentfake.Options{})
	target := newPeer(t, "target", "oxidns", targetOptions)
	_, e = r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.Set("agents", []any{source.record(), target.record()})
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, profile *jsonx.Object) (*jsonx.Object, error) {
		profile.Set("oxidns", jsonx.Obj("config_mode", "native", "native_config", nativeDraft))
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	m, e := agents.New(ctx, r)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	deploy.Wire()
	f := &fixture{ctx: ctx, r: r, m: m, s: New(m, o), source: source, target: target}
	a, e := f.s.get(ctx, "target")
	if e != nil {
		t.Fatal(e)
	}
	src, revision, e := f.s.snapshot(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	p, e := deploy.PrepareContent(ctx, m, a.target, src, deploy.PrepareOptions{BaseURL: "https://panel.example", DeploymentID: "target-baseline"})
	if e != nil {
		t.Fatal(e)
	}
	result, e := deploy.Publish(ctx, m, "target", p.Bundle, deploy.PublishOptions{Activate: true, ConfigRevision: revision, BaseURL: "https://panel.example", ResourceConfig: src, SourceSnapshot: src})
	if e == nil && result.GetString("status") == "verifying" {
		result, e = m.GetDeployment(ctx, "target", "target-baseline")
	}
	if e != nil || result.GetString("status") != "succeeded" {
		t.Fatal("baseline publication", result, e)
	}
	f.publishSource(t, "source-before-binding")
	return f
}
func (f *fixture) publishSource(t *testing.T, id string) {
	t.Helper()
	a, e := f.s.get(f.ctx, "source")
	if e != nil {
		t.Fatal(e)
	}
	src, revision, e := f.s.snapshot(f.ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	b, e := deploy.Build(f.ctx, deploy.BuildInput{Target: a.target, Source: src, ConfigContent: "dns:\n  enable: true\n  listen: 0.0.0.0:2053\n", DeploymentID: id})
	if e != nil {
		t.Fatal(e)
	}
	r, e := deploy.Publish(f.ctx, f.m, "source", b, deploy.PublishOptions{Activate: true, ConfigRevision: revision, BaseURL: "https://panel.example", ResourceConfig: src, SourceSnapshot: src})
	if e == nil && r.GetString("status") == "verifying" {
		r, e = f.m.GetDeployment(f.ctx, "source", id)
	}
	if e != nil || r.GetString("status") != "succeeded" {
		t.Fatal("source publication", r, e)
	}
}
func (f *fixture) bind(t *testing.T) {
	t.Helper()
	_, e := f.s.Configure(f.ctx, "target", json.RawMessage(`{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`))
	if e != nil {
		t.Fatal(e)
	}
}
func (f *fixture) agent(t *testing.T) *agent {
	t.Helper()
	a, e := f.s.get(f.ctx, "target")
	if e != nil {
		t.Fatal(e)
	}
	return a
}
func (f *fixture) profile(t *testing.T) string {
	t.Helper()
	p, e := f.r.GetProfile(f.ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	return p.GetObject("oxidns").GetString("native_config")
}

func TestConfirmedFollowChangesOnlySelectedUpstream(t *testing.T) {
	f := newFixture(t, Options{}, agentfake.Options{})
	f.bind(t)
	before := f.profile(t)
	f.publishSource(t, "source-next")
	if e := f.s.Tick(f.ctx); e != nil {
		t.Fatal(e)
	}
	st := state(f.agent(t))
	if st.Status != "succeeded" || value(st.SourceDeploymentID) != "source-next" || value(st.Address) != "udp://127.0.0.1:2053" {
		t.Fatal(st)
	}
	want, e := yamlx.Parse([]byte(before))
	if e != nil {
		t.Fatal(e)
	}
	binding := f.agent(t).DNSFollow
	entry, e := boundEntry(want, binding)
	if e != nil {
		t.Fatal(e)
	}
	yamlx.Set(entry, "addr", yamlx.Str("udp://127.0.0.1:2053"))
	got, e := yamlx.Parse([]byte(f.profile(t)))
	if e != nil || !yamlx.Equal(want, got) {
		t.Fatal("unrelated draft changed", f.profile(t), e)
	}
	if f.target.uploads() != 2 {
		t.Fatal("follow did not upload exactly once", f.target.uploads())
	}
	for range 3 {
		if e = f.s.Tick(f.ctx); e != nil {
			t.Fatal(e)
		}
	}
	if f.target.uploads() != 2 {
		t.Fatal("repeated follow uploaded again")
	}
}

func TestUnchangedVerifiedAddressSkipsPreparation(t *testing.T) {
	calls := 0
	f := newFixture(t, Options{Prepare: func(ctx context.Context, m *agents.Manager, a deploy.Target, s *deploy.Source, o deploy.PrepareOptions) (*deploy.Prepared, error) {
		calls++
		return deploy.PrepareContent(ctx, m, a, s, o)
	}}, agentfake.Options{})
	f.bind(t)
	f.source.mu.Lock()
	f.source.listenerPort = 1053
	f.source.mu.Unlock()
	f.publishSource(t, "source-next")
	if e := f.s.Tick(f.ctx); e != nil {
		t.Fatal(e)
	}
	st := state(f.agent(t))
	if st.Status != "succeeded" || st.TargetDeploymentID != nil || calls != 0 || f.target.uploads() != 1 {
		t.Fatal(st, calls, f.target.uploads())
	}
}
