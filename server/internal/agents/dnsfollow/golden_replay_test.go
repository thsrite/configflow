package dnsfollow_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents/agentfake"
	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
)

const dfGoldenDraft = `runtime:
  worker_threads: 2
plugins:
  - tag: forward_remote
    type: forward
    args:
      upstreams:
        - addr: udp://192.0.2.10:5353
        - addr: udp://127.0.0.1:1053
`

const dfGoldenBinding = `{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`

type dfDevice struct {
	mu                  sync.Mutex
	fake                *agentfake.Agent
	server              *httptest.Server
	id, service, active string
	enabled, same       bool
	port                int
}

func newDFDevice(t *testing.T, id, service, scenario string) *dfDevice {
	t.Helper()
	d := &dfDevice{
		fake:    agentfake.New(agentfake.Options{AgentID: id, ServiceType: service, Token: "synthetic-" + id + "-token"}),
		id:      id,
		service: service,
		enabled: scenario != "disabled-listener",
		same:    scenario != "wrong-peer",
		port:    2053,
	}
	if scenario == "unchanged" {
		d.port = 1053
	}
	d.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		active := d.active
		d.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/api/dns/") {
			var out any
			switch r.URL.Path {
			case "/api/dns/peer":
				peer := "source"
				if !d.same {
					peer = "other"
				}
				out = map[string]any{"success": true, "peer_agent_id": peer, "service_type": "mihomo"}
			case "/api/dns/listener":
				out = map[string]any{"success": true, "agent_id": id, "deployment_id": active, "host": "0.0.0.0", "port": d.port, "enabled": d.enabled}
			case "/api/dns/active":
				out = map[string]any{"success": true, "agent_id": id, "deployment_id": active}
			}
			_ = json.NewEncoder(w).Encode(out)
			return
		}
		d.fake.ServeHTTP(w, r)
		if r.Method == "POST" && r.URL.Path == "/api/deployments" {
			d.mu.Lock()
			d.active = r.Header.Get("X-Deployment-ID")
			d.mu.Unlock()
		}
	}))
	t.Cleanup(d.server.Close)
	return d
}

func (d *dfDevice) record() map[string]any {
	u, _ := url.Parse(d.server.URL)
	h, p, _ := net.SplitHostPort(u.Host)
	n, _ := strconv.Atoi(p)
	return map[string]any{
		"id":           d.id,
		"name":         d.service,
		"service_type": d.service,
		"host":         h,
		"port":         n,
		"profile_id":   "default",
		"enabled":      true,
		"version":      "1.8.60-go",
		"token":        "synthetic-" + d.id + "-token",
	}
}

func dfMustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func dfValueJSON(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, string(b))
	}
	return v
}

func dfYAMLJSON(t *testing.T, text string) any {
	t.Helper()
	n, err := yamlx.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	b, err := yamlx.ToJSON(n)
	if err != nil {
		t.Fatal(err)
	}
	return dfValueJSON(t, b)
}

func dfArtifacts(t *testing.T, d *dfDevice) []any {
	t.Helper()
	var out []any
	for _, c := range d.fake.Calls() {
		if c.Method != "POST" || c.Path != "/api/deployments" {
			continue
		}
		gz, err := gzip.NewReader(bytes.NewReader(c.Body))
		if err != nil {
			t.Fatal(err)
		}
		tr := tar.NewReader(gz)
		files := map[string]any{}
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasPrefix(h.Name, "files/") {
				b, err := io.ReadAll(tr)
				if err != nil {
					t.Fatal(err)
				}
				if strings.HasSuffix(h.Name, ".yaml") {
					files[h.Name] = dfYAMLJSON(t, string(b))
				} else {
					files[h.Name] = string(b)
				}
			}
		}
		_ = gz.Close()
		out = append(out, files)
	}
	return out
}

func dfRunScenario(t *testing.T, scenario, body string, source, target *dfDevice) any {
	t.Helper()
	srv, app := servertest.New(t, httpxtest.Options{
		Auth:             true,
		DefaultFactories: true,
	})
	ctx := srv.Context()
	if err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		return json.Unmarshal(dfMustJSON(t, []any{source.record(), target.record()}), &s.Agents)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("oxidns", dfMustJSON(t, map[string]any{"config_mode": "native", "native_config": dfGoldenDraft}))
	}); err != nil {
		t.Fatal(err)
	}
	m := app.Agents
	publish := func(id, ident string, baseline bool) {
		a, err := m.ReadAgent(ctx, id, true)
		if err != nil {
			t.Fatal(err)
		}
		tgt, err := deploy.NewTypedTarget(a)
		if err != nil {
			t.Fatal(err)
		}
		raw, rev, err := srv.Repo.PublishConfig(ctx, "default", false)
		if err != nil {
			t.Fatal(err)
		}
		src, err := deploy.SourceFromJSON(dfMustJSON(t, raw), &rev)
		if err != nil {
			t.Fatal(err)
		}
		var bundle *deploy.Bundle
		if baseline {
			p, err := app.Deploy.PrepareContent(ctx, m, tgt, src, deploy.PrepareOptions{BaseURL: "https://panel.example", DeploymentID: ident})
			if err != nil {
				t.Fatal(err)
			}
			bundle = p.Bundle
		} else {
			bundle, err = deploy.Build(ctx, deploy.BuildInput{
				Target:        tgt,
				Source:        src,
				ConfigContent: "dns:\n  enable: true\n  listen: 0.0.0.0:2053\n",
				DeploymentID:  ident,
			})
			if err != nil {
				t.Fatal(err)
			}
		}
		r, err := app.Deploy.Publish(ctx, m, id, bundle, deploy.PublishOptions{
			Activate:       true,
			ConfigRevision: &deploy.Revision{Value: rev},
			BaseURL:        "https://panel.example",
			ResourceConfig: src,
			SourceSnapshot: src,
		})
		if err == nil && r.Status == "verifying" {
			r, err = m.GetDeploymentModel(ctx, id, ident)
		}
		if err != nil || r.Status != "succeeded" {
			t.Fatal(r, err)
		}
	}
	publish("target", "target-baseline", true)
	publish("source", "source-before-binding", false)
	owner := app.DNSFollow
	admin := srv.Login()
	path := "/api/agents/target/dns-follow"
	var replies []any
	request := func(method, raw string, auth bool, suffix string) {
		opts := []httpxtest.Option{}
		if auth {
			opts = append(opts, admin)
		}
		if raw != "" {
			opts = append(opts, httpxtest.Body([]byte(raw), "application/json"))
		}
		r := srv.Do(method, path+suffix, opts...)
		var result any = ""
		if method != "HEAD" {
			result = dfValueJSON(t, r.Body)
		}
		replies = append(replies, map[string]any{"status": r.Status, "body": result})
	}
	request("GET", "", false, "")
	request("GET", "", true, "")
	before := srv.Repo.CommitCount()
	request("HEAD", "", true, "")
	if srv.Repo.CommitCount() != before {
		t.Fatal("HEAD wrote")
	}
	request("PUT", body, true, "")
	if replies[len(replies)-1].(map[string]any)["status"] == 200 {
		if scenario == "drift" {
			if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
				o := dfValueJSON(t, dfMustJSON(t, p.OxiDNS)).(map[string]any)
				o["native_config"] = strings.Replace(o["native_config"].(string), "worker_threads: 2", "worker_threads: 3", 1)
				return p.SetJSON("oxidns", dfMustJSON(t, o))
			}); err != nil {
				t.Fatal(err)
			}
		}
		publish("source", "source-next", false)
		if err := owner.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		request("GET", "", true, "")
		if scenario == "disabled-listener" || scenario == "drift" {
			request("POST", "", true, "/retry")
		} else if scenario == "success" {
			request("DELETE", "", true, "")
		}
	}
	a, err := m.ReadAgent(ctx, "target", true)
	if err != nil {
		t.Fatal(err)
	}
	rawAgent := dfValueJSON(t, dfMustJSON(t, a)).(map[string]any)
	return dfValueJSON(t, dfMustJSON(t, map[string]any{
		"replies":   replies,
		"binding":   rawAgent["dns_follow"],
		"state":     rawAgent["dns_follow_state"],
		"artifacts": dfArtifacts(t, target),
	}))
}

var (
	dfHex32   = regexp.MustCompile(`^[0-9a-f]{32}$`)
	dfHex64   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	dfChildID = regexp.MustCompile(`^dns-[0-9a-f]{16,}$`)
)

func normalizeDFGolden(t *testing.T, v any) any {
	t.Helper()
	out := dfValueJSON(t, dfMustJSON(t, v)).(map[string]any)
	normState := func(raw any) {
		s, ok := raw.(map[string]any)
		if !ok {
			return
		}
		if x, ok := s["updated_at"].(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, x); err != nil {
				t.Fatal(x)
			}
			s["updated_at"] = "@time"
		}
		if x, ok := s["target_deployment_id"].(string); ok {
			if !dfChildID.MatchString(x) {
				t.Fatal(x)
			}
			s["target_deployment_id"] = "@child"
		}
		if events, ok := s["events"].([]any); ok {
			for _, rawEv := range events {
				e := rawEv.(map[string]any)
				for _, key := range []string{"time", "timestamp", "at"} {
					if x, ok := e[key].(string); ok {
						if _, err := time.Parse(time.RFC3339Nano, x); err != nil {
							t.Fatal(x)
						}
						e[key] = "@time"
					}
				}
			}
		}
	}
	normBinding := func(raw any) {
		b, ok := raw.(map[string]any)
		if !ok {
			return
		}
		if id, ok := b["id"].(string); ok && dfHex32.MatchString(id) {
			b["id"] = "@binding"
		}
		if sig, ok := b["upstream_signature"].(string); ok && dfHex64.MatchString(sig) {
			b["upstream_signature"] = "@entry-digest"
		}
	}
	for _, raw := range out["replies"].([]any) {
		body, ok := raw.(map[string]any)["body"].(map[string]any)
		if !ok {
			continue
		}
		normBinding(body["binding"])
		normState(body["state"])
	}
	normBinding(out["binding"])
	normState(out["state"])
	return out
}

func TestDNSFollowGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	cases := []struct{ name, body string }{
		{"success", dfGoldenBinding},
		{"unchanged", dfGoldenBinding},
		{"drift", dfGoldenBinding},
		{"disabled-listener", dfGoldenBinding},
		{"wrong-peer", dfGoldenBinding},
		{"null-index", strings.Replace(dfGoldenBinding, `"upstream_index":1`, `"upstream_index":null`, 1)},
		{"shape", `[]`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gs := newDFDevice(t, "source", "mihomo", tc.name)
			gt := newDFDevice(t, "target", "oxidns", tc.name)
			got := dfRunScenario(t, tc.name, tc.body, gs, gt)
			key := dfMustJSON(t, map[string]string{"scenario": tc.name})
			actual := dfMustJSON(t, normalizeDFGolden(t, got))
			ref.Assert(t, key, actual)
		})
	}
}
