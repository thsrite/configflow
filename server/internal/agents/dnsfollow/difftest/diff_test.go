// Package difftest compares A17's actual Flask/Go routes and generated artifacts.
package difftest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"configflow/internal/repo/repotest"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/agents/dnsfollow"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

const draft = `runtime:
  worker_threads: 2
plugins:
  - tag: forward_remote
    type: forward
    args:
      upstreams:
        - addr: udp://192.0.2.10:5353
        - addr: udp://127.0.0.1:1053
`
const binding = `{"source_agent_id":"source","upstream_tag":"forward_remote","upstream_index":1,"connection":"auto"}`

type device struct {
	mu                  sync.Mutex
	fake                *agentfake.Agent
	server              *httptest.Server
	id, service, active string
	enabled, same       bool
	port                int
}

func newDevice(t *testing.T, id, service, scenario string) *device {
	t.Helper()
	d := &device{fake: agentfake.New(agentfake.Options{AgentID: id, ServiceType: service, Token: "synthetic-" + id + "-token"}), id: id, service: service, enabled: scenario != "disabled-listener", same: scenario != "wrong-peer", port: 2053}
	if scenario == "unchanged" {
		d.port = 1053
	}
	d.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		active := d.active
		d.mu.Unlock()
		if strings.HasPrefix(r.URL.Path, "/api/dns/") {
			if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer synthetic-"+id+"-token" || r.ContentLength > 0 || r.Header.Get("Content-Type") != "" {
				t.Error("DNS wire mismatch", r.Method, r.URL, r.Header)
			}
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
func (d *device) record() map[string]any {
	u, _ := url.Parse(d.server.URL)
	h, p, _ := net.SplitHostPort(u.Host)
	n, _ := strconv.Atoi(p)
	return map[string]any{"id": d.id, "name": d.service, "service_type": d.service, "host": h, "port": n, "profile_id": "default", "enabled": true, "version": "1.8.60-go", "token": "synthetic-" + d.id + "-token"}
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func valueJSON(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if e := json.Unmarshal(b, &v); e != nil {
		t.Fatal(e, string(b))
	}
	return v
}
func yamlJSON(t *testing.T, text string) any {
	t.Helper()
	n, e := yamlx.Parse([]byte(text))
	if e != nil {
		t.Fatal(e)
	}
	b, e := yamlx.ToJSON(n)
	if e != nil {
		t.Fatal(e)
	}
	return valueJSON(t, b)
}
func artifacts(t *testing.T, d *device) []any {
	t.Helper()
	var out []any
	for _, c := range d.fake.Calls() {
		if c.Method != "POST" || c.Path != "/api/deployments" {
			continue
		}
		gz, e := gzip.NewReader(bytes.NewReader(c.Body))
		if e != nil {
			t.Fatal(e)
		}
		tr := tar.NewReader(gz)
		files := map[string]any{}
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				t.Fatal(e)
			}
			if strings.HasPrefix(h.Name, "files/") {
				b, e := io.ReadAll(tr)
				if e != nil {
					t.Fatal(e)
				}
				if strings.HasSuffix(h.Name, ".yaml") {
					files[h.Name] = yamlJSON(t, string(b))
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
func goRun(t *testing.T, scenario, body string, source, target *device) any {
	t.Helper()
	agents.ResetHooks()
	agents.ResetRuntime()
	t.Cleanup(func() { agents.ResetHooks(); agents.ResetRuntime() })
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, DefaultFactories: true})
	ctx := srv.Context()
	err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		return json.Unmarshal(mustJSON(t, []any{source.record(), target.record()}), &s.Agents)
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		return p.SetJSON("oxidns", mustJSON(t, map[string]any{"config_mode": "native", "native_config": draft}))
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := agents.New(ctx, srv.Repo, agents.Options{ReceiptMatches: deploy.ReceiptMatches})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Close)
	owner := dnsfollow.New(m, dnsfollow.Options{})
	apiagents.NewHandler(apiagents.Deps{Repo: srv.Repo, Agents: m, DNSFollow: owner}).Register(srv.App.Router)
	publish := func(id, ident string, baseline bool) {
		a, e := m.ReadAgent(ctx, id, true)
		if e != nil {
			t.Fatal(e)
		}
		tgt, e := deploy.NewTypedTarget(a)
		if e != nil {
			t.Fatal(e)
		}
		raw, rev, e := srv.Repo.PublishConfig(ctx, "default", false)
		if e != nil {
			t.Fatal(e)
		}
		src, e := deploy.SourceFromJSON(mustJSON(t, raw), &rev)
		if e != nil {
			t.Fatal(e)
		}
		var bundle *deploy.Bundle
		if baseline {
			p, e := deploy.New(deploy.Options{}).PrepareContent(ctx, m, tgt, src, deploy.PrepareOptions{BaseURL: "https://panel.example", DeploymentID: ident})
			if e != nil {
				t.Fatal(e)
			}
			bundle = p.Bundle
		} else {
			bundle, e = deploy.Build(ctx, deploy.BuildInput{Target: tgt, Source: src, ConfigContent: "dns:\n  enable: true\n  listen: 0.0.0.0:2053\n", DeploymentID: ident})
			if e != nil {
				t.Fatal(e)
			}
		}
		r, e := deploy.New(deploy.Options{}).Publish(ctx, m, id, bundle, deploy.PublishOptions{Activate: true, ConfigRevision: &deploy.Revision{Value: rev}, BaseURL: "https://panel.example", ResourceConfig: src, SourceSnapshot: src})
		if e == nil && r.Status == "verifying" {
			r, e = m.GetDeploymentModel(ctx, id, ident)
		}
		if e != nil || r.Status != "succeeded" {
			t.Fatal(r, e)
		}
	}
	publish("target", "target-baseline", true)
	publish("source", "source-before-binding", false)
	// The clock does not affect comparisons: only event/updated_at timestamps are normalized below.
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
			result = valueJSON(t, r.Body)
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
			_, e := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
				o := valueJSON(t, mustJSON(t, p.OxiDNS)).(map[string]any)
				o["native_config"] = strings.Replace(o["native_config"].(string), "worker_threads: 2", "worker_threads: 3", 1)
				return p.SetJSON("oxidns", mustJSON(t, o))
			})
			if e != nil {
				t.Fatal(e)
			}
		}
		publish("source", "source-next", false)
		if e := owner.Tick(ctx); e != nil {
			t.Fatal(e)
		}
		request("GET", "", true, "")
		if scenario == "disabled-listener" || scenario == "drift" {
			request("POST", "", true, "/retry")
		} else if scenario == "success" {
			request("DELETE", "", true, "")
		}
	}
	a, e := m.ReadAgent(ctx, "target", true)
	if e != nil {
		t.Fatal(e)
	}
	s, e := m.ReadAgent(ctx, "source", true)
	if e != nil {
		t.Fatal(e)
	}
	p, e := srv.Repo.Profile(ctx, "default")
	if e != nil {
		t.Fatal(e)
	}
	proofs := true
	for _, reply := range replies {
		body, ok := reply.(map[string]any)["body"].(map[string]any)
		if !ok {
			continue
		}
		b, bok := body["binding"].(map[string]any)
		st, sok := body["state"].(map[string]any)
		if bok && sok && st["target_deployment_id"] != nil {
			attempt := st["attempt"]
			if attempt == nil {
				attempt = 0
			}
			expected := "dns-" + deploy.CanonicalDigest(mustJSON(t, []any{b["id"], st["source_deployment_id"], attempt}))[:48]
			proofs = proofs && st["target_deployment_id"] == expected
		}
	}
	aMap := valueJSON(t, mustJSON(t, a)).(map[string]any)
	pOxi := valueJSON(t, mustJSON(t, p.OxiDNS)).(map[string]any)
	jobRaw, hasJob := aMap["dns_follow_job"]
	if hasJob {
		job := jobRaw.(map[string]any)
		settings := job["candidate_settings"].(map[string]any)
		doc, e := yamlx.Parse([]byte(settings["native_config"].(string)))
		if e != nil {
			t.Fatal(e)
		}
		entry := yamlx.Get(yamlx.Get(yamlx.Get(doc, "plugins").Content[0], "args"), "upstreams").Content[1]
		raw, e := yamlx.ToJSON(entry)
		if e != nil {
			t.Fatal(e)
		}
		proofs = proofs && job["candidate_signature"] == deploy.CanonicalDigest(raw)
	}
	statusOf := func(rec *model.Agent) string {
		if rec == nil || rec.LatestDeployment == nil {
			return ""
		}
		return rec.LatestDeployment.Status
	}
	return valueJSON(t, mustJSON(t, map[string]any{"replies": replies, "draft": yamlJSON(t, pOxi["native_config"].(string)), "state": aMap["dns_follow_state"], "has_job": hasJob, "proofs": proofs, "source_status": statusOf(s), "target_status": statusOf(a)}))
}

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)
var childID = regexp.MustCompile(`^dns-[0-9a-f]{48}$`)

// normalize touches only A17's named nondeterministic/proof fields. Addresses,
// error text, status, events, YAML, and arbitrary nested hex strings stay exact.
func normalize(t *testing.T, v any) any {
	t.Helper()
	out := valueJSON(t, mustJSON(t, v)).(map[string]any)
	state := func(raw any) {
		if raw == nil {
			return
		}
		s := raw.(map[string]any)
		// A null deployment ID is a missing member since O18 T1b.
		for _, key := range []string{"source_deployment_id", "target_deployment_id"} {
			if value, present := s[key]; present && value == nil {
				delete(s, key)
			}
		}
		if x, ok := s["updated_at"].(string); ok {
			if _, err := time.Parse(time.RFC3339Nano, x); err != nil {
				t.Fatal(x)
			}
			s["updated_at"] = "@time"
		}
		if x, ok := s["target_deployment_id"].(string); ok {
			if !childID.MatchString(x) {
				t.Fatal(x)
			}
			s["target_deployment_id"] = "@child"
		}
		if events, ok := s["events"].([]any); ok {
			for _, raw := range events {
				e := raw.(map[string]any)
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
	for _, raw := range out["replies"].([]any) {
		body, ok := raw.(map[string]any)["body"].(map[string]any)
		if !ok {
			continue
		}
		if b, ok := body["binding"].(map[string]any); ok {
			if !hex32.MatchString(b["id"].(string)) || !hex64.MatchString(b["upstream_signature"].(string)) {
				t.Fatal(b)
			}
			b["id"] = "@binding"
			b["upstream_signature"] = "@entry-digest"
		}
		state(body["state"])
	}
	state(out["state"])
	return out
}
func TestPythonRoutesAndArtifacts(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 for live Python")
	}
	root, err := filepath.Abs("../../../../..")
	if err != nil {
		t.Fatal(err)
	}
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		python = filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python")
	}
	// Refresh the fixture contract through Python's real pin_rule helper.
	fixtureCommand := exec.CommandContext(t.Context(), python, filepath.Join(root, "server/internal/agents/dnsfollow/testdata/pin_reference.py"))
	fixtureCommand.Dir = root
	fixtureCommand.Env = append(os.Environ(), "PYTHONPATH="+root, "PYTHONDONTWRITEBYTECODE=1", "LOG_FILE="+filepath.Join(t.TempDir(), "pin.log"))
	generated, e := fixtureCommand.Output()
	if e != nil {
		t.Fatal("Python pin helper", e)
	}
	pinned, e := os.ReadFile("../testdata/pinned.json")
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(valueJSON(t, generated), valueJSON(t, pinned)) {
		t.Fatal("pin fixture is stale; regenerate using testdata/pin_reference.py")
	}
	for _, tc := range []struct{ name, body string }{
		{"success", binding}, {"unchanged", binding}, {"drift", binding}, {"disabled-listener", binding}, {"wrong-peer", binding},
		{"null-index", strings.Replace(binding, `"upstream_index":1`, `"upstream_index":null`, 1)}, {"shape", `[]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ps, pt := newDevice(t, "source", "mihomo", tc.name), newDevice(t, "target", "oxidns", tc.name)
			cmd := exec.CommandContext(t.Context(), python, filepath.Join(root, "server/internal/agents/dnsfollow/difftest/testdata/reference.py"))
			dir := t.TempDir()
			cmd.Dir = root
			repotest.PythonLayout(t, dir) // Python reads the version-1 layout (O17)
			cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+dir, "LOG_FILE="+filepath.Join(dir, "app.log"), "ADMIN_USERNAME=a17-admin", "ADMIN_PASSWORD=synthetic-password", "JWT_SECRET_KEY=synthetic-a17-key", "PYTHONDONTWRITEBYTECODE=1")
			cmd.Stdin = bytes.NewReader(mustJSON(t, map[string]any{"agents": []any{ps.record(), pt.record()}, "draft": draft, "scenario": tc.name, "body": tc.body}))
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			output, e := cmd.Output()
			if e != nil {
				t.Fatalf("Python: %v\n%s\n%s", e, output, stderr.String())
			}
			py := valueJSON(t, output)
			gs, gt := newDevice(t, "source", "mihomo", tc.name), newDevice(t, "target", "oxidns", tc.name)
			goValue := goRun(t, tc.name, tc.body, gs, gt)
			left, right := normalize(t, py), normalize(t, goValue)
			if !right.(map[string]any)["proofs"].(bool) {
				t.Fatal("invalid Go digest/child proof")
			}
			if !reflect.DeepEqual(left, right) {
				t.Fatalf("Python %s\nGo %s", mustJSON(t, left), mustJSON(t, right))
			}
			if !reflect.DeepEqual(artifacts(t, pt), artifacts(t, gt)) {
				t.Fatalf("actual uploaded files differ: Python %s Go %s", mustJSON(t, artifacts(t, pt)), mustJSON(t, artifacts(t, gt)))
			}
			if !left.(map[string]any)["proofs"].(bool) {
				t.Fatal("invalid Python digest proof")
			}
		})
	}
}
func TestNormalizationRetainsSemanticChanges(t *testing.T) {
	v := map[string]any{"replies": []any{}, "state": map[string]any{"status": "succeeded", "address": "udp://192.0.2.1:53", "updated_at": "2026-11-18T00:00:00Z", "opaque": strings.Repeat("a", 64)}}
	original := normalize(t, v)
	v["state"].(map[string]any)["address"] = "udp://192.0.2.2:53"
	if reflect.DeepEqual(original, normalize(t, v)) {
		t.Fatal("address mutation hidden")
	}
	v["state"].(map[string]any)["address"] = "udp://192.0.2.1:53"
	v["state"].(map[string]any)["opaque"] = strings.Repeat("b", 64)
	if reflect.DeepEqual(original, normalize(t, v)) {
		t.Fatal("unrelated digest mutation hidden")
	}
}
