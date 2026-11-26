package dnsha_test

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
	"regexp"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
	"configflow/internal/yamlx"
)

const haGoldenDraft = `plugins:
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

const haGoldenBody = `{"peer_id":"secondary","vip":"192.0.2.53"}`

type haLocalClient struct{ urls map[string]string }

func (c haLocalClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, err
	}
	base := c.urls[u.Hostname()]
	if base == "" {
		return nil, fmt.Errorf("non-loopback destination refused")
	}
	r.URL = base + u.RequestURI()
	r.NoProxy = true
	return (agents.HTTPClient{}).Do(ctx, r)
}

type haDevices struct {
	segment *agentfake.Segment
	urls    map[string]string
	records []map[string]any
	fakes   map[string]*agentfake.Agent
}

func newHADevices(t *testing.T, scenario string) *haDevices {
	t.Helper()
	d := &haDevices{
		segment: agentfake.NewSegment("primary"),
		urls:    map[string]string{},
		fakes:   map[string]*agentfake.Agent{},
	}
	for i, id := range []string{"primary", "secondary"} {
		host := "192.0.2.20"
		if i == 1 {
			host = "192.0.2.21"
		}
		token := "synthetic-" + id + "-token"
		caps := map[string]any{
			"success":                     true,
			"service_type":                "oxidns",
			"deployment_protocols":        []int{1},
			"final_config_protocols":      []int{1},
			"reload_only_protocols":       []int{1},
			"config_path":                 "config.yaml",
			"oxidns_ha_protocols":         []int{1},
			"oxidns_ha_handoff_protocols": []int{1},
		}
		if scenario == "old-protocol" && i == 1 {
			caps["oxidns_ha_protocols"] = []int{}
		}
		if scenario == "docker-environment" && i == 1 {
			caps["oxidns_ha_environment"] = map[string]any{
				"kind":     "container",
				"ready":    false,
				"blockers": []string{"synthetic mount required"},
			}
		}
		outcome := ""
		if scenario == "standby-failure" && id == "secondary" {
			outcome = "failed"
		}
		fake := agentfake.New(agentfake.Options{
			ActivationOutcome: outcome,
			AgentID:           id,
			Token:             token,
			ServiceType:       "oxidns",
			Capabilities:      haMustJSON(t, caps),
		})
		d.fakes[id] = fake
		handler := d.segment.Handler(id, token, fake)
		var lost sync.Once
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if scenario == "preview-blocker" && r.URL.Path == "/api/oxidns-ha/preview" {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"ok":       false,
					"blockers": []string{"synthetic blocker"},
					"cidr":     "192.0.2.0/24",
				})
				return
			}
			if scenario == "connection" && r.URL.Path == "/api/capabilities" {
				conn, _, _ := w.(http.Hijacker).Hijack()
				_ = conn.Close()
				return
			}
			if scenario == "lost-yield" && id == "primary" && r.Method == "POST" && r.URL.Path == "/api/oxidns-ha/handoff" {
				dropped := false
				lost.Do(func() {
					rec := httptest.NewRecorder()
					handler.ServeHTTP(rec, r)
					conn, _, _ := w.(http.Hijacker).Hijack()
					_ = conn.Close()
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
		d.records = append(d.records, map[string]any{
			"id":                id,
			"name":              "DNS " + id,
			"host":              host,
			"port":              8082,
			"token":             token,
			"service_type":      "oxidns",
			"profile_id":        "default",
			"enabled":           true,
			"deployment_method": "shell",
			"version":           "1.8.60-go",
		})
	}
	if scenario == "reconfigure" || scenario == "lost-yield" {
		for _, a := range d.records {
			other, role := "secondary", "primary"
			if a["id"] == "secondary" {
				other, role = "primary", "secondary"
			}
			a["oxidns_ha"] = map[string]any{
				"enabled":  true,
				"group_id": "synthetic-existing-group",
				"peer_id":  other,
				"vip":      "192.0.2.53",
				"vrid":     53,
				"role":     role,
			}
			a["oxidns_ha_state"] = map[string]any{"status": "succeeded", "error": ""}
		}
		d.segment.Configure("primary", "secondary")
	}
	return d
}

func haMustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func haJSONValue(t *testing.T, b []byte) any {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("%v: %s", err, string(b))
	}
	return v
}

func haAgentModel(t *testing.T, v any) *model.Agent {
	t.Helper()
	var a model.Agent
	if err := json.Unmarshal(haMustJSON(t, v), &a); err != nil {
		t.Fatal(err)
	}
	return &a
}

func haRunScenario(t *testing.T, d *haDevices, requestBody string) any {
	t.Helper()
	srv, app := servertest.New(t, httpxtest.Options{
		Auth:             true,
		DefaultFactories: false,
	})
	ctx := srv.Context()
	if err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, doc *model.System) error {
		return json.Unmarshal(haMustJSON(t, d.records), &doc.Agents)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, doc *model.Profile) error {
		return doc.SetJSON("oxidns", haMustJSON(t, map[string]any{"config_mode": "native", "native_config": haGoldenDraft}))
	}); err != nil {
		t.Fatal(err)
	}
	m := app.Agents
	m.SetClient(haLocalClient{d.urls})
	for _, id := range []string{"primary", "secondary"} {
		if _, err := m.ObserveHeartbeat(ctx, id, haAgentModel(t, map[string]any{"service_status": "running"})); err != nil {
			t.Fatal(err)
		}
	}
	admin := srv.Login()
	replies := []any{}
	path := "/api/agents/primary/oxidns-ha"
	request := func(method, reqPath, body string, auth bool) {
		opts := []httpxtest.Option{}
		if auth {
			opts = append(opts, admin)
		}
		if body != "" {
			opts = append(opts, httpxtest.Body([]byte(body), "application/json"))
		}
		r := srv.Do(method, reqPath, opts...)
		var data any = ""
		if method != "HEAD" {
			data = haJSONValue(t, r.Body)
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
			if _, err := app.DNSHA.Advance(ctx, "primary"); err != nil {
				t.Fatal(err)
			}
		}
		request("GET", path, "", true)
		request("GET", "/api/agents/secondary/oxidns-ha", "", true)
		request("GET", "/api/agents/primary/oxidns/rules/update", "", true)
		request("POST", "/api/agents/primary/generate-config",
			`{"expected_profile_id":"default","expected_service_type":"oxidns","base_url":"https://panel.example"}`, true)
		request("DELETE", path, "", true)
	}
	stored := []any{}
	for _, id := range []string{"primary", "secondary"} {
		a, err := m.ReadAgent(ctx, id, true)
		if err != nil {
			t.Fatal(err)
		}
		rawMap := haJSONValue(t, haMustJSON(t, a)).(map[string]any)
		v := map[string]any{}
		for _, key := range []string{"oxidns_ha", "oxidns_ha_state", "oxidns_ha_publication"} {
			if value, ok := rawMap[key]; ok {
				v[key] = value
			}
		}
		stored = append(stored, v)
	}
	return haJSONValue(t, haMustJSON(t, map[string]any{
		"replies":   replies,
		"stored":    stored,
		"artifacts": haArtifacts(t, d),
	}))
}

var (
	haRandomHex  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	haChildHex   = regexp.MustCompile(`^ha-[0-9a-f]{48}$`)
	haOwnerHex   = regexp.MustCompile(`^ha-[0-9a-f]{32}$`)
	haLocalPort  = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)
	haISOTimeStr = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
)

func normalizeHAGolden(v any) any {
	switch x := v.(type) {
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeHAGolden(item)
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
						if haOwnerHex.MatchString(text) || haChildHex.MatchString(text) {
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
					if haRandomHex.MatchString(text) || haOwnerHex.MatchString(text) || haChildHex.MatchString(text) {
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
					if node, err := yamlx.Parse([]byte(text)); err == nil {
						if raw, err := yamlx.ToJSON(node); err == nil {
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
			out[k] = normalizeHAGolden(item)
		}
		return out
	case string:
		if haISOTimeStr.MatchString(x) {
			return "@time"
		}
		return haLocalPort.ReplaceAllString(x, "http://127.0.0.1:<port>")
	}
	return v
}

func haArtifacts(t *testing.T, d *haDevices) []any {
	t.Helper()
	out := []any{}
	for _, c := range d.segment.Calls() {
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
			if !strings.HasPrefix(h.Name, "files/") {
				continue
			}
			b, err := io.ReadAll(tr)
			if err != nil {
				t.Fatal(err)
			}
			if strings.HasSuffix(h.Name, ".yaml") {
				node, err := yamlx.Parse(b)
				if err != nil {
					t.Fatal(err)
				}
				raw, err := yamlx.ToJSON(node)
				if err != nil {
					t.Fatal(err)
				}
				files[h.Name] = haJSONValue(t, raw)
			} else {
				files[h.Name] = string(b)
			}
		}
		_ = gz.Close()
		out = append(out, map[string]any{
			"member":   c.AgentID,
			"activate": c.Header.Get("X-Activate"),
			"files":    files,
		})
	}
	return out
}

func TestDNSHAGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})
	scenarios := []string{
		"enable", "malformed", "non-object", "wrong-type", "missing",
		"old-protocol", "docker-environment", "preview-blocker", "connection",
		"reconfigure", "lost-yield", "standby-failure",
	}
	for _, scenario := range scenarios {
		t.Run(scenario, func(t *testing.T) {
			reqBody := haGoldenBody
			switch scenario {
			case "malformed":
				reqBody = "{"
			case "non-object":
				reqBody = "[]"
			case "wrong-type":
				reqBody = `{"peer_id":1,"vip":"192.0.2.53"}`
			case "missing":
				reqBody = `{"peer_id":"secondary"}`
			}
			dev := newHADevices(t, scenario)
			got := haRunScenario(t, dev, reqBody)
			key := haMustJSON(t, map[string]string{"scenario": scenario})
			actual := haMustJSON(t, normalizeHAGolden(got))
			ref.Assert(t, key, actual)
		})
	}
}
