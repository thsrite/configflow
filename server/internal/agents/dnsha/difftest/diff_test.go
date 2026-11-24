// Package difftest compares A18's real HTTP entry points with Flask.
package difftest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/agents/dnsha"
	"configflow/internal/agents/monitor"
	apiagents "configflow/internal/api/agents"
	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/yamlx"
)

const draft = `plugins:
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
const body = `{"peer_id":"secondary","vip":"192.0.2.53"}`

type localClient struct{ urls map[string]string }

func (c localClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	u, e := url.Parse(r.URL)
	if e != nil {
		return nil, e
	}
	base := c.urls[u.Hostname()]
	if base == "" {
		return nil, fmt.Errorf("non-loopback destination refused")
	}
	r.URL = base + u.RequestURI()
	r.NoProxy = true
	return (agents.HTTPClient{}).Do(ctx, r)
}

type devices struct {
	segment *agentfake.Segment
	urls    map[string]string
	records []map[string]any
	fakes   map[string]*agentfake.Agent
}

func newDevices(t *testing.T, scenario string) *devices {
	t.Helper()
	d := &devices{segment: agentfake.NewSegment("primary"), urls: map[string]string{}, fakes: map[string]*agentfake.Agent{}}
	for i, id := range []string{"primary", "secondary"} {
		host := "192.0.2.20"
		if i == 1 {
			host = "192.0.2.21"
		}
		token := "synthetic-" + id + "-token"
		caps := map[string]any{"success": true, "service_type": "oxidns", "deployment_protocols": []int{1}, "final_config_protocols": []int{1}, "reload_only_protocols": []int{1}, "config_path": "config.yaml", "oxidns_ha_protocols": []int{1}, "oxidns_ha_handoff_protocols": []int{1}}
		if scenario == "old-protocol" && i == 1 {
			caps["oxidns_ha_protocols"] = []int{}
		}
		if scenario == "docker-environment" && i == 1 {
			caps["oxidns_ha_environment"] = map[string]any{"kind": "container", "ready": false, "blockers": []string{"synthetic mount required"}}
		}
		outcome := ""
		if scenario == "standby-failure" && id == "secondary" {
			outcome = "failed"
		}
		fake := agentfake.New(agentfake.Options{ActivationOutcome: outcome, AgentID: id, Token: token, ServiceType: "oxidns", Capabilities: mustJSON(t, caps)})
		d.fakes[id] = fake
		handler := d.segment.Handler(id, token, fake)
		var lost sync.Once
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if scenario == "preview-blocker" && r.URL.Path == "/api/oxidns-ha/preview" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": false, "blockers": []string{"synthetic blocker"}, "cidr": "192.0.2.0/24"})
				return
			}
			if scenario == "connection" && r.URL.Path == "/api/capabilities" {
				connection, _, _ := w.(http.Hijacker).Hijack()
				_ = connection.Close()
				return
			}
			if scenario == "lost-yield" && id == "primary" && r.Method == "POST" && r.URL.Path == "/api/oxidns-ha/handoff" {
				dropped := false
				lost.Do(func() {
					recorder := httptest.NewRecorder()
					handler.ServeHTTP(recorder, r)
					connection, _, _ := w.(http.Hijacker).Hijack()
					_ = connection.Close()
					dropped = true
				})
				if dropped {
					return
				}
			}
			handler.ServeHTTP(w, r)
		}))
		t.Cleanup(srv.Close)
		d.urls[host] = srv.URL
		d.records = append(d.records, map[string]any{"id": id, "name": "DNS " + id, "host": host, "port": 8082, "token": token, "service_type": "oxidns", "profile_id": "default", "enabled": true, "deployment_method": "shell", "version": "1.8.60-go"})
	}
	if scenario == "reconfigure" || scenario == "lost-yield" {
		for _, a := range d.records {
			other, role := "secondary", "primary"
			if a["id"] == "secondary" {
				other, role = "primary", "secondary"
			}
			a["oxidns_ha"] = map[string]any{"enabled": true, "group_id": "synthetic-existing-group", "peer_id": other, "vip": "192.0.2.53", "vrid": 53, "role": role}
			a["oxidns_ha_state"] = map[string]any{"status": "succeeded", "error": ""}
		}
		d.segment.Configure("primary", "secondary")
	}
	return d
}
func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, e := json.Marshal(v)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func jsonValue(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if e := json.Unmarshal(b, &v); e != nil {
		t.Fatal(e, string(b))
	}
	return v
}
func python(t *testing.T, d *devices, request string) any {
	t.Helper()
	root, e := filepath.Abs("../../../../..")
	if e != nil {
		t.Fatal(e)
	}
	cmd := exec.Command(filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), "server/internal/agents/dnsha/difftest/testdata/reference.py")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "DATA_DIR="+t.TempDir(), "ADMIN_USERNAME=synthetic-a18-admin", "ADMIN_PASSWORD=synthetic-password", "JWT_SECRET_KEY=synthetic-a18-key")
	cmd.Stdin = bytes.NewReader(mustJSON(t, map[string]any{"agents": d.records, "urls": d.urls, "draft": draft, "body": request}))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	result, e := cmd.Output()
	if e != nil {
		t.Fatalf("Python %v: %s", e, stderr.String())
	}
	return jsonValue(t, result)
}
func golang(t *testing.T, d *devices, requestBody string) any {
	t.Helper()
	var m *agents.Manager
	var ha *dnsha.Service
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, DefaultFactories: false})
	ctx := srv.Context()
	e := srv.Repo.UpdateSystem(ctx, func(_ context.Context, doc *model.System) error {
		return json.Unmarshal(mustJSON(t, d.records), &doc.Agents)
	})
	if e != nil {
		t.Fatal(e)
	}
	_, e = srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, doc *model.Profile) error {
		return doc.SetJSON("oxidns", mustJSON(t, map[string]any{"config_mode": "native", "native_config": draft}))
	})
	if e != nil {
		t.Fatal(e)
	}
	m, e = agents.New(ctx, srv.Repo, agents.Options{ReceiptMatches: deploy.ReceiptMatcher(srv.Repo)})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(m.Close)
	ha = dnsha.New(m, dnsha.Options{})
	if e = m.SetPairPublisher(ha); e != nil {
		t.Fatal(e)
	}
	mon := monitor.New(srv.Repo.DataDir())
	if e = mon.SetHARules(ha.PairRulesJSON); e != nil {
		t.Fatal(e)
	}
	dp := deploy.New(deploy.Options{Pair: func(ctx context.Context, _ *agents.Manager, id string, b *deploy.Bundle, o deploy.PublishOptions) (*model.Deployment, bool, error) {
		return ha.PublishPair(ctx, id, b, o)
	}})
	ha.Deploy = dp
	apiagents.NewHandler(apiagents.Deps{Repo: srv.Repo, Agents: m, DNSHA: ha, Monitor: mon, Deploy: dp}).Register(srv.App.Router)
	m.SetClient(localClient{d.urls})
	for _, id := range []string{"primary", "secondary"} {
		if _, e = m.ObserveHeartbeat(ctx, id, agentModelForTest(t, map[string]any{"service_status": "running"})); e != nil {
			t.Fatal(e)
		}
	}
	admin := srv.Login()
	replies := []any{}
	path := "/api/agents/primary/oxidns-ha"
	request := func(method, path, body string, auth bool) {
		opts := []httpxtest.Option{}
		if auth {
			opts = append(opts, admin)
		}
		if body != "" {
			opts = append(opts, httpxtest.Body([]byte(body), "application/json"))
		}
		r := srv.Do(method, path, opts...)
		var data any = ""
		if method != "HEAD" {
			data = jsonValue(t, r.Body)
		}
		replies = append(replies, map[string]any{"status": r.Status, "body": data})
	}
	request("GET", path, "", false)
	request("GET", path+"-environment", "", false)
	request("GET", path, "", true)
	request("HEAD", path, "", true)
	request("GET", path+"-environment", "", true)
	request("PUT", path, requestBody, true)
	last := replies[len(replies)-1].(map[string]any)
	if last["status"] == 202 {
		for range 14 {
			if _, e = ha.Advance(ctx, "primary"); e != nil {
				t.Fatal(e)
			}
		}
		request("GET", path, "", true)
		request("GET", "/api/agents/secondary/oxidns-ha", "", true)
		request("GET", "/api/agents/primary/oxidns/rules/update", "", true)
		request("POST", "/api/agents/primary/generate-config", `{"expected_profile_id":"default","expected_service_type":"oxidns","base_url":"https://panel.example"}`, true)
		request("DELETE", path, "", true)
	}
	stored := []any{}
	for _, id := range []string{"primary", "secondary"} {
		a, e := m.ReadAgent(ctx, id, true)
		if e != nil {
			t.Fatal(e)
		}
		rawMap := jsonValue(t, mustJSON(t, a)).(map[string]any)
		v := map[string]any{}
		for _, key := range []string{"oxidns_ha", "oxidns_ha_state", "oxidns_ha_publication"} {
			if value, ok := rawMap[key]; ok {
				v[key] = value
			}
		}
		stored = append(stored, v)
	}
	return jsonValue(t, mustJSON(t, map[string]any{"replies": replies, "stored": stored}))
}

var randomHex = regexp.MustCompile(`^[0-9a-f]{32}$`)
var childHex = regexp.MustCompile(`^ha-[0-9a-f]{48}$`)
var ownerHex = regexp.MustCompile(`^ha-[0-9a-f]{32}$`)

func normalize(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalize(item)
		}
		return out
	case map[string]any:
		out := map[string]any{}
		for k, item := range x {
			switch k {
			case "children":
				if children, ok := item.(map[string]any); ok {
					outChildren := map[string]any{}
					for id, value := range children {
						text, _ := value.(string)
						if ownerHex.MatchString(text) || childHex.MatchString(text) {
							outChildren[id] = "@child"
						} else {
							outChildren[id] = value
						}
					}
					out[k] = outChildren
					continue
				}
			case "updated_at":
				if _, ok := item.(float64); ok {
					out[k] = "@time"
					continue
				}
			case "id", "group_id", "owner_deployment_id", "deployment_id", "preview_id":
				if text, ok := item.(string); ok {
					if randomHex.MatchString(text) || ownerHex.MatchString(text) || childHex.MatchString(text) {
						out[k] = "@" + k
						continue
					}
				}
			case "expires_at":
				if _, ok := item.(float64); ok {
					out[k] = "@expires"
					continue
				}
			case "config_version", "input_digest", "content_sha256", "sha256":
				if _, ok := item.(string); ok {
					out[k] = "@" + k
					continue
				}
			case "config_content", "content":
				if text, ok := item.(string); ok {
					node, e := yamlx.Parse([]byte(text))
					if e == nil {
						raw, e := yamlx.ToJSON(node)
						if e == nil {
							var parsed any
							if json.Unmarshal(raw, &parsed) == nil {
								out[k] = parsed
								continue
							}
						}
					}
				}
			}
			if k == "size" && x["role"] == "config" {
				out[k] = "@formatted-size"
				continue
			}
			out[k] = normalize(item)
		}
		return out
	}
	return v
}
func artifacts(t *testing.T, d *devices) []any {
	t.Helper()
	out := []any{}
	for _, c := range d.segment.Calls() {
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
			if !strings.HasPrefix(h.Name, "files/") {
				continue
			}
			b, e := io.ReadAll(tr)
			if e != nil {
				t.Fatal(e)
			}
			if strings.HasSuffix(h.Name, ".yaml") {
				node, e := yamlx.Parse(b)
				if e != nil {
					t.Fatal(e)
				}
				raw, e := yamlx.ToJSON(node)
				if e != nil {
					t.Fatal(e)
				}
				files[h.Name] = jsonValue(t, raw)
			} else {
				files[h.Name] = string(b)
			}
		}
		_ = gz.Close()
		out = append(out, map[string]any{"member": c.AgentID, "activate": c.Header.Get("X-Activate"), "files": files})
	}
	return out
}
func TestHAHTTPAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1")
	}
	for _, scenario := range []string{"enable", "malformed", "non-object", "wrong-type", "missing", "old-protocol", "docker-environment", "preview-blocker", "connection", "reconfigure", "lost-yield", "standby-failure"} {
		t.Run(scenario, func(t *testing.T) {
			request := body
			switch scenario {
			case "malformed":
				request = "{"
			case "non-object":
				request = "[]"
			case "wrong-type":
				request = `{"peer_id":1,"vip":"192.0.2.53"}`
			case "missing":
				request = `{"peer_id":"secondary"}`
			}
			pyDev := newDevices(t, scenario)
			want := python(t, pyDev, request)
			goDev := newDevices(t, scenario)
			got := golang(t, goDev, request)
			if !reflect.DeepEqual(normalize(want), normalize(got)) {
				t.Fatalf("HTTP/DB mismatch\nPython %s\nGo %s", mustJSON(t, normalize(want)), mustJSON(t, normalize(got)))
			}
			pa, ga := artifacts(t, pyDev), artifacts(t, goDev)
			if scenario == "enable" || scenario == "reconfigure" || scenario == "lost-yield" {
				if len(pa) != 2 || len(ga) != 2 {
					t.Fatal("successful scenario did not publish exactly two actual bundles")
				}
				replies := got.(map[string]any)["replies"].([]any)
				generated := replies[len(replies)-2].(map[string]any)
				if generated["status"] != float64(200) || generated["body"].(map[string]any)["success"] != true {
					t.Fatal("successful scenario did not cover final generation")
				}
			}
			if !reflect.DeepEqual(pa, ga) {
				t.Fatalf("delivered config mismatch\nPython %s\nGo %s", mustJSON(t, pa), mustJSON(t, ga))
			}
		})
	}
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
