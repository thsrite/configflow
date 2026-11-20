package dnsha

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
	"configflow/internal/jsonx"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

const config = `plugins:
  - tag: sequence_main
    type: sequence
    args:
      - exec: reject 3
  - tag: udp
    type: udp_server
    args:
      entry: sequence_main
      listen: 0.0.0.0:53
`
const primaryHost = "192.0.2.20"
const secondaryHost = "192.0.2.21"
const vip = "192.0.2.53"

type transport struct {
	mu       sync.Mutex
	urls     map[string]string
	before   func(context.Context, agents.Request) error
	after    func(context.Context, agents.Request, *agents.Response) error
	requests []agents.Request
}

func (c *transport) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	c.mu.Lock()
	c.requests = append(c.requests, r)
	before, after := c.before, c.after
	c.mu.Unlock()
	if before != nil {
		if e := before(ctx, r); e != nil {
			return nil, e
		}
	}
	u, e := url.Parse(r.URL)
	if e != nil {
		return nil, e
	}
	base := c.urls[u.Hostname()]
	if base == "" {
		return nil, errors.New("non-loopback test destination refused")
	}
	local := r
	local.URL = base + u.RequestURI()
	local.NoProxy = true
	response, e := (agents.HTTPClient{}).Do(ctx, local)
	if e == nil && after != nil {
		e = after(ctx, r, response)
	}
	return response, e
}
func (c *transport) setBefore(f func(context.Context, agents.Request) error) {
	c.mu.Lock()
	c.before = f
	c.mu.Unlock()
}
func (c *transport) setAfter(f func(context.Context, agents.Request, *agents.Response) error) {
	c.mu.Lock()
	c.after = f
	c.mu.Unlock()
}
func (c *transport) calls() []agents.Request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]agents.Request(nil), c.requests...)
}

type fixture struct {
	ctx     context.Context
	r       *repo.Repository
	m       *agents.Manager
	s       *Service
	segment *agentfake.Segment
	devices map[string]*agentfake.Agent
	client  *transport
}

func fixtureNew(t *testing.T, options Options, paired bool, deviceOptions map[string]agentfake.Options) *fixture {
	t.Helper()
	r, e := repo.Open(t.TempDir(), repo.Options{DefaultConfig: repo.GetDefaultConfig})
	if e != nil {
		t.Fatal(e)
	}
	ctx := repo.WithRepository(t.Context(), r)
	segment := agentfake.NewSegment("primary")
	client := &transport{urls: map[string]string{}}
	devices := map[string]*agentfake.Agent{}
	records := []any{}
	for i, id := range []string{"primary", "secondary"} {
		host := primaryHost
		if i == 1 {
			host = secondaryHost
		}
		token := "synthetic-" + id + "-token"
		o := deviceOptions[id]
		o.AgentID = id
		o.Token = token
		o.ServiceType = "oxidns"
		if o.Capabilities == nil {
			o.Capabilities = json.RawMessage(`{"success":true,"deployment_protocols":[1],"reload_only_protocols":[1],"final_config_protocols":[1],"oxidns_ha_protocols":[1],"oxidns_ha_handoff_protocols":[1],"service_type":"oxidns","config_path":"config.yaml"}`)
		}
		fake := agentfake.New(o)
		devices[id] = fake
		srv := httptest.NewServer(segment.Handler(id, token, fake))
		t.Cleanup(srv.Close)
		client.urls[host] = srv.URL
		a := jsonx.Obj("id", id, "name", "DNS "+id, "host", host, "port", 8082, "profile_id", "default", "service_type", "oxidns", "deployment_method", "shell", "enabled", true, "token", token, "version", "1.8.60-go")
		if paired {
			other, role := "secondary", "primary"
			if i == 1 {
				other, role = "primary", "secondary"
			}
			a.Set("oxidns_ha", jsonx.Obj("enabled", true, "group_id", "synthetic-group", "peer_id", other, "vip", vip, "vrid", 53, "role", role))
			a.Set("oxidns_ha_state", jsonx.Obj("status", "succeeded", "error", ""))
		}
		records = append(records, a)
	}
	_, e = r.UpdateSystemTransaction(ctx, func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
		doc.Set("agents", records)
		return nil, nil
	})
	if e != nil {
		t.Fatal(e)
	}
	_, _, e = r.UpdateProfileTransaction(ctx, "default", func(_ context.Context, doc *jsonx.Object) (*jsonx.Object, error) {
		doc.Set("oxidns", jsonx.Obj("config_mode", "native", "native_config", config))
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
	m.SetClient(client)
	for _, id := range []string{"primary", "secondary"} {
		if _, e = m.ObserveHeartbeat(ctx, id, agentModelForTest(t, jsonx.Obj("service_status", "running"))); e != nil {
			t.Fatal(e)
		}
	}
	deploy.Wire()
	installHooks()
	f := &fixture{ctx, r, m, New(m, options), segment, devices, client}
	services.Lock()
	services.byManager[m] = f.s
	services.Unlock()
	if paired {
		segment.Configure("primary", "secondary")
	}
	return f
}
func (f *fixture) bundle(t *testing.T, id, did string, reload bool) *deploy.Bundle {
	t.Helper()
	a, e := f.s.get(f.ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	src, _, e := f.s.source(f.ctx, a, true)
	if e != nil {
		t.Fatal(e)
	}
	b, e := deploy.Build(f.ctx, deploy.BuildInput{Target: a.target, Source: src, ConfigContent: config, DeploymentID: did})
	if e != nil {
		t.Fatal(e)
	}
	if reload {
		m := b.Manifest()
		m.ReloadOnly = true
		fs, e := b.Contents()
		if e != nil {
			t.Fatal(e)
		}
		b, e = deploy.Pack(m, fs, b.ConfigVersion(), b.Comparison())
		if e != nil {
			t.Fatal(e)
		}
	}
	return b
}
func (f *fixture) publish(t *testing.T, id, did string, activate bool) *agents.Result {
	t.Helper()
	a, e := f.s.get(f.ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	src, rev, e := f.s.source(f.ctx, a, true)
	if e != nil {
		t.Fatal(e)
	}
	b := f.bundle(t, id, did, false)
	result, e := deploy.Publish(f.ctx, f.m, id, b, deploy.PublishOptions{Activate: activate, SourceSnapshot: src, ResourceConfig: src, ConfigRevision: rev, BaseURL: "https://panel.example"})
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(result)
	var legacy agents.Result
	_ = json.Unmarshal(raw, &legacy)
	return &legacy
}
func (f *fixture) drain(t *testing.T, rounds int) *model.HAPublicationSummary {
	t.Helper()
	var last *model.HAPublicationSummary
	for range rounds {
		var e error
		last, e = f.s.Advance(f.ctx, "primary")
		if e != nil {
			t.Fatal(e)
		}
		if last != nil && terminal(last.Status) {
			return last
		}
	}
	return last
}
func (f *fixture) uploads() []agentfake.HACall {
	out := []agentfake.HACall{}
	for _, c := range f.segment.Calls() {
		if c.Method == "POST" && c.Path == "/api/deployments" {
			out = append(out, c)
		}
	}
	return out
}
func TestMain(m *testing.M) {
	code := m.Run()
	if lockx.Violations() != 0 {
		fmt.Fprintln(os.Stderr, "HA lock order violation")
		code = 1
	}
	os.Exit(code)
}
func TestPairPublicationFromEitherMember(t *testing.T) {
	for _, owner := range []string{"primary", "secondary"} {
		t.Run(owner, func(t *testing.T) {
			f := fixtureNew(t, Options{}, true, nil)
			f.publish(t, owner, "shared-release", true)
			last := f.drain(t, 12)
			if last.Status != "succeeded" {
				t.Fatalf("%+v", last)
			}
			uploads := f.uploads()
			if len(uploads) != 2 || uploads[0].AgentID != "secondary" || uploads[1].AgentID != "primary" || uploads[1].Header.Get("X-Activate") != "false" {
				t.Fatalf("uploads %+v", uploads)
			}
			for _, id := range []string{"primary", "secondary"} {
				a, e := f.s.get(f.ctx, id)
				if e != nil {
					t.Fatal(e)
				}
				j := a.OxiDNSHAPublication
				if j.Handoff == nil || !yes(j.Handoff.Released) || j.Handoff.Confirmations < 2 {
					t.Fatalf("handoff %+v", j.Handoff)
				}
			}
			f.publish(t, owner, "shared-release", true)
			if len(f.uploads()) != 2 {
				t.Fatal("replay uploaded twice")
			}
			views := []*agent{}
			for _, id := range []string{"primary", "secondary"} {
				a, e := f.s.get(f.ctx, id)
				if e != nil {
					t.Fatal(e)
				}
				views = append(views, a)
			}
			if !sameBaseline(views) {
				t.Fatal("pair baselines differ")
			}
			v, e := f.s.Status(f.ctx, "primary")
			if e != nil || v.Health != "ok" {
				t.Fatal(v, e)
			}
			for _, r := range f.client.calls() {
				u, _ := url.Parse(r.URL)
				if strings.HasPrefix(u.Path, "/api/oxidns-ha") {
					if r.Timeout.Connect.Seconds() != 5 {
						t.Fatal("connect timeout", r.Timeout)
					}
					read := 15.0
					if r.Method == "PUT" || r.Method == "DELETE" && u.Path == "/api/oxidns-ha" {
						read = 90
					}
					if u.Path == "/api/oxidns-ha/preview" {
						read = 30
					}
					if r.Timeout.Read.Seconds() != read {
						t.Fatal("read timeout", r.URL, r.Timeout)
					}
					if r.Method == "GET" || r.Method == "DELETE" && u.Path == "/api/oxidns-ha" {
						if len(r.Body) != 0 || r.Header.Get("Content-Type") != "" {
							t.Fatal("read/remove sent body")
						}
					}
					if r.Header.Get("Authorization") == "" || !r.Latin1Headers {
						t.Fatal("auth contract")
					}
				}
			}
		})
	}
}
func TestInitialEnableCapturesOneSource(t *testing.T) {
	count := 0
	f := fixtureNew(t, Options{Prepare: func(ctx context.Context, m *agents.Manager, target deploy.Target, src *deploy.Source, o deploy.PrepareOptions) (*deploy.Prepared, error) {
		count++
		return deploy.PrepareContent(ctx, m, target, src, o)
	}}, false, nil)
	out, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "https://panel.example")
	if e != nil {
		t.Fatal(e)
	}
	if out.Publication.Status != "preparing" || count != 0 || len(f.uploads()) != 0 {
		t.Fatal("enable changed a device before returning")
	}
	again, e := f.s.Configure(f.ctx, "primary", []byte(`{"peer_id":"secondary","vip":"192.0.2.53"}`), "https://panel.example")
	if e != nil || again.Publication.ID != out.Publication.ID {
		t.Fatal("duplicate enable", again, e)
	}
	last := f.drain(t, 12)
	if last.Status != "succeeded" || count != 1 {
		t.Fatal(last, count)
	}
	applies := []string{}
	for _, c := range f.segment.Calls() {
		if c.Method == "PUT" && c.Path == "/api/oxidns-ha" {
			applies = append(applies, c.AgentID)
		}
	}
	if !reflect.DeepEqual(applies, []string{"secondary", "primary"}) {
		t.Fatal(applies)
	}
	a, _ := f.s.get(f.ctx, "primary")
	b, _ := f.s.get(f.ctx, "secondary")
	if !paired(a, b) || a.OxiDNSHAState.Status != "succeeded" || b.OxiDNSHAState.Status != "succeeded" {
		t.Fatal("partial stored pair")
	}
	_, e = f.s.Disable(f.ctx, "primary", false)
	if e != nil {
		t.Fatal(e)
	}
	a, _ = f.s.get(f.ctx, "primary")
	b, _ = f.s.get(f.ctx, "secondary")
	if a.OxiDNSHA != nil || b.OxiDNSHA != nil || a.OxiDNSHAPublication != nil || a.LatestDeployment.HAPublication == nil {
		t.Fatal("disable shape/history")
	}
}

func decodeAgent(raw *agents.Record) (*agent, error) {
	if raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var a model.Agent
	if err = json.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return modelAgent(&a)
}

// ServiceOperation fences restart/update/uninstall against a pair reservation.
func (s *Service) ServiceOperation(ctx context.Context, id string, op func() (*agents.Result, error), uninstall bool) (*agents.Result, error) {
	raw, err := s.ServiceOperationJSON(ctx, id, func() (json.RawMessage, error) {
		value, err := op()
		if err != nil {
			return nil, err
		}
		return json.Marshal(value)
	}, uninstall)
	if err != nil {
		return nil, err
	}
	var result agents.Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// Compatibility test codecs preserve the old assertions against the same native owner.
func resultObject(value any, err error) (*agents.Result, error) {
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result agents.Result
	err = json.Unmarshal(raw, &result)
	return &result, err
}
func (s *Service) Publish(ctx context.Context, id string, b *deploy.Bundle, o deploy.PublishOptions) (*agents.Result, bool, error) {
	result, handled, err := s.PublishPair(ctx, id, b, o)
	value, err := resultObject(result, err)
	return value, handled, err
}
func (s *Service) Deployment(ctx context.Context, id, did string) (*agents.Result, error) {
	return resultObject(s.PairDeployment(ctx, id, did))
}
func (s *Service) Activate(ctx context.Context, id, did string) (*agents.Result, error) {
	return resultObject(s.ActivatePair(ctx, id, did))
}
func (s *Service) Rules(ctx context.Context, id, method string) (*agents.Result, int, error) {
	result, code, err := s.PairRules(ctx, id, method)
	value, err := resultObject(result, err)
	return value, code, err
}

func agentModelForTest(t testing.TB, value any) *model.Agent {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var a model.Agent
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return &a
}
