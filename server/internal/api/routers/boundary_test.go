package routers_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	api "configflow/internal/api/routers"
	apirules "configflow/internal/api/rules"
	"configflow/internal/httpx/httpxtest"
	core "configflow/internal/routers"
	"configflow/internal/routers/routerfake"
	"configflow/internal/yamlx"
)

type boundaryRequest struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   any    `json:"body,omitempty"`
	Use    string `json:"use,omitempty"`
	Create bool   `json:"create,omitempty"`
	Status int    `json:"status"`
	Settle bool   `json:"settle,omitempty"`
}
type boundaryResponse struct {
	Status  int             `json:"status"`
	Payload json.RawMessage `json:"payload"`
	Cache   string          `json:"cache"`
	Settled json.RawMessage `json:"settled"`
}
type boundaryResult struct {
	Systems   []json.RawMessage   `json:"systems"`
	Shared    []json.RawMessage   `json:"shared"`
	Responses []boundaryResponse  `json:"responses"`
	Stored    []json.RawMessage   `json:"stored"`
	Files     []map[string]string `json:"files"`
}

func r15Python(t *testing.T, dir string, input any) boundaryResult {
	t.Helper()
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	raw, _ := json.Marshal(input)
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r15_http_reference.py"))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "R15_DATA="+dir)
	cmd.Stdin = bytes.NewReader(raw)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	data, err := cmd.Output()
	if err != nil {
		t.Fatalf("Flask %v: %s", err, stderr.String())
	}
	var out boundaryResult
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err, string(data))
	}
	return out
}
func deviceState() routerfake.State {
	return routerfake.State{PolicyWire: true, Tables: map[string][]routerfake.Row{"/routing/table": {{".id": "*0", "name": "main", "fib": ""}}, "/ip/route": {{".id": "*1", "dst-address": "192.0.2.0/24", "gateway": "bridge", "routing-table": "main", "immediate-gw": "bridge", "active": "true", "connect": "true", "dynamic": "true"}, {".id": "*2", "dst-address": "198.18.0.0/16", "gateway": "192.0.2.4", "routing-table": "main", "active": "true", "immediate-gw": "192.0.2.4%bridge", "disabled": "false", "comment": "cf"}}, "/ip/firewall/nat": {{".id": "*3", "chain": "srcnat", "action": "masquerade", "out-interface": "wan", "disabled": "false"}}, "/ip/address": {{".id": "*A0", "address": "192.0.2.1/24", "interface": "bridge", "disabled": "false"}}, "/interface": {{".id": "*I0", "name": "bridge", "type": "bridge", "running": "true"}, {".id": "*I1", "name": "wan", "type": "ether", "running": "true"}}}}
}
func inputConfig(endpoint string) map[string]any {
	router := routerBody(endpoint)
	router["id"] = "router-one"
	router["proxy_gateway_agent_id"] = "gateway"
	router["verify_tls"] = false
	router["dns_agent_id"] = ""
	router["manage_return_routing"] = false
	router["auto_rule_sync"] = false
	router["rule_sync_interval"] = 60
	router["ip_rule_sets"] = []string{}
	router["ros_policy_rule_sets"] = []string{}
	return map[string]any{"routers": map[string]any{"items": []any{router}}, "mihomo": map[string]any{"custom_config": "dns:\n  enhanced-mode: fake-ip\n  fake-ip-range: 198.18.0.0/16\n"}, "device_notes": map[string]string{"192.0.2.21": "Synthetic"}}
}
func boundaryRequests(endpoint string) []boundaryRequest {
	root := "/api/routers/router-one"
	route := map[string]any{"action": "add", "values": map[string]any{"cidr": "203.0.113.0/24", "gateway": "192.0.2.4", "enabled": true, "comment": "manual"}}
	draft := map[string]any{"base_url": "", "router": map[string]any{"ip_rule_sets": []string{}, "ros_policy_rule_sets": []string{}, "proxy_gateway_agent_id": "gateway"}}
	return []boundaryRequest{
		{Method: "GET", Path: "/api/routers", Status: 200},
		{Method: "PUT", Path: root, Body: map[string]string{"name": "Renamed"}, Status: 200},
		{Method: "POST", Path: root + "/probe", Status: 200},
		{Method: "GET", Path: root + "/report", Status: 200},
		{Method: "POST", Path: root + "/plan", Status: 200},
		{Method: "POST", Path: root + "/apply", Use: "plan", Status: 200},
		{Method: "GET", Path: root + "/routes", Status: 200},
		{Method: "POST", Path: root + "/routes/plan", Body: route, Status: 200},
		{Method: "POST", Path: root + "/routes/apply", Body: route, Use: "route", Status: 200},
		{Method: "GET", Path: root + "/policy", Status: 200},
		{Method: "GET", Path: root + "/policy-settings", Status: 200},
		{Method: "PUT", Path: root + "/policy-settings", Body: draft, Use: "workspace", Status: 200},
		{Method: "POST", Path: root + "/policy-settings/preview", Body: map[string]any{"targets": []string{}}, Use: "workspace", Status: 200},
		{Method: "GET", Path: root + "/rule-sync", Status: 200},
		{Method: "PUT", Path: root + "/rule-sync", Body: map[string]any{"auto_rule_sync": false, "rule_sync_interval": 30}, Status: 200},
		{Method: "POST", Path: root + "/rule-sync", Status: 409},
		{Method: "GET", Path: "/api/routers/gateway-forwarding", Status: 200},
		{Method: "POST", Path: "/api/routers/gateway-forwarding", Body: map[string]string{"agent_id": "gateway"}, Status: 409},
		{Method: "GET", Path: "/api/agents/gateway/upstream-routers?profile=default", Status: 200},
		{Method: "POST", Path: "/api/routers", Body: routerBody(endpoint), Create: true, Status: 200},
		{Method: "DELETE", Path: "/api/routers/$new", Status: 200},
		{Method: "POST", Path: "/api/routers", Body: []any{}, Status: 400},
		{Method: "PUT", Path: root, Body: []any{}, Status: 400},
		{Method: "POST", Path: root + "/routes/plan", Body: []any{}, Status: 400},
		{Method: "POST", Path: root + "/routes/apply", Body: route, Status: 409},
		{Method: "POST", Path: root + "/apply", Body: map[string]string{"fingerprint": "stale"}, Status: 409},
		{Method: "PUT", Path: root + "/policy-settings", Body: []any{}, Status: 400},
		{Method: "POST", Path: root + "/policy-settings/preview", Body: map[string]any{"targets": []string{}}, Status: 409},
		{Method: "PUT", Path: root + "/rule-sync", Body: []any{}, Status: 400},
		{Method: "POST", Path: "/api/routers/gateway-forwarding", Body: []any{}, Status: 400},
		{Method: "GET", Path: "/api/routers/missing/report", Status: 404},
	}
}

var generatedRouterID = regexp.MustCompile(`^router_[0-9a-f]{12}$`)
var digestPattern = regexp.MustCompile(`^(?:[0-9a-f]{16}|[0-9a-f]{64})$`)

func normalizedBoundary(t *testing.T, value any, path string) any {
	t.Helper()
	switch v := value.(type) {
	case map[string]any:
		// R14's native Desired retains zero-valued optional fields that Python's
		// successful delta receipt omits. Non-default values remain strict.
		if strings.Contains(path, "router-rule-sync/") && strings.HasSuffix(path, ".json") {
			for key, value := range map[string]any{"added": float64(0), "removed": float64(0), "log": []any{}} {
				if _, ok := v[key]; !ok {
					v[key] = value
				}
			}
		}
		if strings.Contains(path, "router-rule-sync/") && strings.HasSuffix(path, ".baseline.desired") {
			for key, value := range map[string]any{"ipv6_cidrs": []any{}, "blocking_warnings": []any{}, "dns_agent_id": "", "dns_server": "", "dns_error": ""} {
				if _, ok := v[key]; !ok {
					v[key] = value
				}
			}
		}
		out := map[string]any{}
		for key, item := range v {
			location := path + "." + key
			if key == "_rule_order" && strings.Contains(path, "router-rule-sync/") {
				order, ok := item.([]any)
				if !ok {
					t.Fatal("bad private order index")
				}
				rules, _ := v["rules"].(map[string]any)
				if len(order) != len(rules) {
					t.Fatal("private index does not cover rules")
				}
				seen := map[string]bool{}
				for _, id := range order {
					text, ok := id.(string)
					if !ok || seen[text] || rules[text] == nil {
						t.Fatal("private index lost a rule")
					}
					seen[text] = true
				}
				continue
			}
			if key == "last_duration_ms" && strings.Contains(path, "task-status.json") {
				duration, ok := item.(float64)
				if !ok || duration < 0 {
					t.Fatal("invalid duration")
				}
				item = "<validated duration>"
			}
			if key == "dns_content" || (key == "native_config" || key == "template_base_config") && strings.HasSuffix(path, ".oxidns") {
				if text, ok := item.(string); ok && text != "" {
					node, err := yamlx.Parse([]byte(text))
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := yamlx.ToJSON(node)
					if err != nil {
						t.Fatal(err)
					}
					if err = json.Unmarshal(encoded, &item); err != nil {
						t.Fatal(err)
					}
				}
			}
			if key == "updated_at" || key == "generated_at" || key == "checked_at" || key == "synced_at" || key == "at" || strings.Contains(path, "task-status.json") && (key == "last_run_at" || key == "last_finished_at" || key == "last_success_at") {
				if text, ok := item.(string); ok && text != "" {
					if _, err := time.Parse(time.RFC3339Nano, text); err != nil {
						if _, err = time.Parse("2006-01-02T15:04:05.999999999", text); err != nil {
							t.Fatalf("bad clock at %s: %q", location, text)
						}
					}
					item = "<validated clock>"
				}
			}
			if key == "oxidns_revision" || key == "revision" && (strings.HasSuffix(path, ".workspace") || strings.HasSuffix(path, ".preview")) || key == "fingerprint" && (strings.Contains(path, ".plan") || strings.HasSuffix(path, ".router_plan")) || key == "fingerprint" && strings.Contains(path, ".last_release") || key == "revision" && strings.Contains(path, ".entry") || key == "revision" && strings.Contains(path, ".routes[") || key == "control" && strings.Contains(path, ".baseline") {
				if text, ok := item.(string); ok && digestPattern.MatchString(text) {
					item = "<native revision>"
				}
			}
			if key == "id" {
				if text, ok := item.(string); ok && generatedRouterID.MatchString(text) {
					item = "<generated router id>"
				}
			}
			out[key] = normalizedBoundary(t, item, location)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = normalizedBoundary(t, item, fmt.Sprintf("%s[%d]", path, i))
		}
		return out
	}
	return value
}
func boundaryDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if x, ok := a.(map[string]any); ok {
		y, ok := b.(map[string]any)
		if !ok {
			return path + ": type"
		}
		for key, item := range x {
			if _, exists := y[key]; !exists {
				return path + "." + key + ": Go-only key"
			}
			if difference := boundaryDifference(item, y[key], path+"."+key); difference != "" {
				return difference
			}
		}
		for key := range y {
			if _, exists := x[key]; !exists {
				return path + "." + key + ": Python-only key"
			}
		}
		return path + ": keys"
	}
	if x, ok := a.([]any); ok {
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return path + ": length/type"
		}
		for i, item := range x {
			if difference := boundaryDifference(item, y[i], fmt.Sprintf("%s[%d]", path, i)); difference != "" {
				return difference
			}
		}
	}
	return fmt.Sprintf("%s: Go=%v Python=%v", path, a, b)
}
func compareBoundary(t *testing.T, left, right any) {
	t.Helper()
	rawA, _ := json.Marshal(left)
	rawB, _ := json.Marshal(right)
	var a, b any
	if json.Unmarshal(rawA, &a) != nil || json.Unmarshal(rawB, &b) != nil {
		t.Fatal("bad comparison JSON")
	}
	a = normalizedBoundary(t, a, "$")
	b = normalizedBoundary(t, b, "$")
	if !reflect.DeepEqual(a, b) {
		t.Fatal(boundaryDifference(a, b, "$"))
	}
}
func boundaryFiles(t *testing.T, dir string) map[string]any {
	t.Helper()
	out := map[string]any{}
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		extension := filepath.Ext(path)
		if extension != ".json" && extension != ".yaml" && extension != ".yml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		key, _ := filepath.Rel(dir, path)
		var value any
		if extension == ".json" {
			if err = json.Unmarshal(data, &value); err != nil {
				return err
			}
		} else {
			node, err := yamlx.Parse(data)
			if err != nil {
				return err
			}
			encoded, err := yamlx.ToJSON(node)
			if err != nil {
				return err
			}
			if err = json.Unmarshal(encoded, &value); err != nil {
				return err
			}
		}
		out[key] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func TestR15FlaskHTTPAndStoredState(t *testing.T) {
	fake := routerfake.New(nil)
	seed := deviceState()
	fake.Reset(seed)
	server := httptest.NewServer(fake)
	defer server.Close()
	cfg := inputConfig(server.URL)
	system := map[string]any{"system_config": map[string]string{"rule_proxy_token": "synthetic-rule-token"}, "agents": []any{map[string]any{"id": "gateway", "name": "Synthetic gateway", "host": "192.0.2.4", "port": 9100, "service_type": "mihomo", "profile_id": "default", "enabled": true, "status": "offline", "token": "synthetic-agent-token"}}}
	requests := boundaryRequests(server.URL)
	runHTTPBoundary(t, fake, seed, cfg, system, requests)
}

func runHTTPBoundary(t *testing.T, fake *routerfake.Fake, seed routerfake.State, cfg, system any, requests []boundaryRequest, shared ...map[string]any) {
	t.Helper()
	dir := t.TempDir()
	input := map[string]any{"config": cfg, "system": system, "requests": requests}
	if len(shared) > 0 {
		input["shared"] = shared[0]
	}
	oracle := r15Python(t, dir, input)
	pythonTables := fake.Snapshot().Tables
	fake.Reset(seed)
	s := httpxtest.New(t, httpxtest.Options{DataDir: filepath.Join(dir, "go"), Register: httpxtest.Routes(api.Register)})
	core.Wire()
	apirules.Wire()
	var plan, route, workspace map[string]any
	created := ""
	for i, step := range requests {
		passed := t.Run(fmt.Sprintf("%02d_%s_%s", i, step.Method, step.Path), func(t *testing.T) {
			body := step.Body
			if step.Use == "plan" {
				body = map[string]any{"fingerprint": plan["fingerprint"]}
			}
			if step.Use == "route" {
				copy := map[string]any{}
				for key, value := range step.Body.(map[string]any) {
					copy[key] = value
				}
				copy["fingerprint"] = route["fingerprint"]
				body = copy
			}
			if step.Use == "workspace" {
				copy := map[string]any{}
				for key, value := range step.Body.(map[string]any) {
					copy[key] = value
				}
				copy["revision"] = workspace["revision"]
				if _, ok := copy["policies"]; ok {
					copy["names_revision"] = workspace["names_revision"]
				}
				body = copy
			}
			path := strings.ReplaceAll(step.Path, "$new", created)
			options := []httpxtest.Option{}
			if body != nil {
				options = append(options, httpxtest.JSON(body))
			}
			result := s.Do(step.Method, path, options...)
			if oracle.Responses[i].Status != step.Status {
				t.Fatalf("Flask prerequisite status=%d want=%d: %s", oracle.Responses[i].Status, step.Status, oracle.Responses[i].Payload)
			}
			if result.Status != step.Status || result.Header.Get("Cache-Control") != oracle.Responses[i].Cache {
				t.Fatalf("HTTP Go=%d %s %s Python=%d %s %s", result.Status, result.Header.Get("Cache-Control"), result.Body, oracle.Responses[i].Status, oracle.Responses[i].Cache, oracle.Responses[i].Payload)
			}
			if step.Settle {
				compareAsyncResponse(t, result.Body, oracle.Responses[i].Payload)
				settled := waitSync(t, s)
				compareBoundary(t, settled, oracle.Responses[i].Settled)
			} else {
				compareBoundary(t, json.RawMessage(result.Body), oracle.Responses[i].Payload)
			}
			var response map[string]any
			_ = json.Unmarshal(result.Body, &response)
			if value, ok := response["plan"].(map[string]any); ok {
				if strings.Contains(path, "/routes/") {
					route = value
				} else {
					plan = value
				}
			}
			if value, ok := response["workspace"].(map[string]any); ok {
				workspace = value
			}
			if step.Create {
				created = response["router"].(map[string]any)["id"].(string)
				if !generatedRouterID.MatchString(created) {
					t.Fatal("invalid new router id")
				}
			}
			stored, err := s.Repo.GetProfile(s.Context(), "default")
			if err != nil {
				t.Fatal(err)
			}
			compareBoundary(t, stored, oracle.Stored[i])
			system, err := s.Repo.GetSystem(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			compareBoundary(t, system, oracle.Systems[i])
			shared, err := s.Repo.GetShared(s.Context())
			if err != nil {
				t.Fatal(err)
			}
			compareBoundary(t, shared, oracle.Shared[i])
			expectedFiles := map[string]any{}
			for name, content := range oracle.Files[i] {
				var value any
				if strings.HasSuffix(name, ".json") {
					if err = json.Unmarshal([]byte(content), &value); err != nil {
						t.Fatal(err)
					}
				} else {
					node, err := yamlx.Parse([]byte(content))
					if err != nil {
						t.Fatal(err)
					}
					encoded, err := yamlx.ToJSON(node)
					if err != nil {
						t.Fatal(err)
					}
					_ = json.Unmarshal(encoded, &value)
				}
				expectedFiles[name] = value
			}
			compareBoundary(t, boundaryFiles(t, s.DataDir), expectedFiles)
		})
		if !passed {
			t.FailNow()
		}
	}
	compareBoundary(t, fake.Snapshot().Tables, pythonTables)
}

func TestR15FlaskWorkspaceSourcesAndDNSPreview(t *testing.T) {
	fake := routerfake.New(nil)
	seed := deviceState()
	fake.Reset(seed)
	server := httptest.NewServer(fake)
	defer server.Close()
	cfg := inputConfig(server.URL)
	system := map[string]any{"agents": []any{map[string]any{"id": "gateway", "name": "Synthetic gateway", "host": "192.0.2.4", "service_type": "mihomo", "profile_id": "default", "status": "offline", "token": "synthetic-gateway-token"}, map[string]any{"id": "dns-one", "name": "DNS one", "host": "192.0.2.53", "service_type": "oxidns", "profile_id": "default", "status": "offline", "token": "synthetic-dns-token"}, map[string]any{"id": "dns-other", "name": "Foreign", "host": "192.0.2.54", "service_type": "oxidns", "profile_id": "other", "status": "offline", "token": "synthetic-foreign-token"}}}
	root := "/api/routers/router-one/policy-settings"
	router := map[string]any{"ip_rule_sets": []string{}, "ros_policy_rule_sets": []string{}, "proxy_gateway_agent_id": "gateway"}
	draft := func(name string) map[string]any {
		return map[string]any{"base_url": "", "router": router, "policies": []any{map[string]any{"ip": "192.0.2.21", "name": name, "mode": "smart"}, map[string]any{"ip": "192.0.2.22", "name": "Direct", "mode": "direct"}}}
	}
	steps := []boundaryRequest{{Method: "GET", Path: root, Status: 200}, {Method: "PUT", Path: root, Body: draft("Renamed"), Use: "workspace", Status: 200}, {Method: "PUT", Path: root, Body: draft("中文名称"), Use: "workspace", Status: 200}, {Method: "POST", Path: root + "/preview", Body: map[string]any{"targets": []string{"dns-one"}}, Use: "workspace", Status: 200}, {Method: "POST", Path: root + "/preview", Body: map[string]any{"targets": []string{"dns-other"}}, Use: "workspace", Status: 409}, {Method: "POST", Path: root + "/preview", Body: map[string]any{"targets": []any{nil}}, Use: "workspace", Status: 400}, {Method: "PUT", Path: root, Body: map[string]any{"base_url": "", "router": router, "policies": []any{map[string]string{"ip": "bad", "name": "bad", "mode": "smart"}}}, Use: "workspace", Status: 400}, {Method: "PUT", Path: root, Body: map[string]any{"base_url": "", "revision": "stale", "router": router}, Status: 409}}
	steps[6].Body.(map[string]any)["router"] = map[string]any{"ip_rule_sets": []string{}, "ros_policy_rule_sets": []string{}, "proxy_gateway_agent_id": "different"}
	runHTTPBoundary(t, fake, seed, cfg, system, steps)
}

func waitSync(t *testing.T, s *httpxtest.Server) core.RuleSyncStatus {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		receipt, err := (core.RuleSyncStore{DataDir: s.DataDir}).Read(s.Context(), "default", "router-one")
		if err != nil {
			t.Fatal(err)
		}
		if receipt.Status != "queued" && receipt.Status != "running" {
			if receipt.Status != "synced" {
				t.Fatalf("sync did not complete: %s %s", receipt.Status, receipt.Message)
			}
			raw, err := os.ReadFile(filepath.Join(s.DataDir, "task-status.json"))
			if err == nil {
				var entries map[string]struct {
					Count int `json:"run_count"`
				}
				if json.Unmarshal(raw, &entries) == nil && entries["router_rule_sync:default:router-one"].Count == 1 {
					return receipt.Public(true)
				}
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("sync completion timed out")
	return core.RuleSyncStatus{}
}
func compareAsyncResponse(t *testing.T, left, right json.RawMessage) {
	t.Helper()
	var a, b map[string]any
	if json.Unmarshal(left, &a) != nil || json.Unmarshal(right, &b) != nil {
		t.Fatal("invalid async response")
	}
	allowed := map[string]string{"queued": "已加入同步任务", "running": "正在检查远程规则", "synced": "远程规则已核对，ROS 已同步"}
	status := []string{}
	for _, value := range []map[string]any{a, b} {
		sync, ok := value["sync"].(map[string]any)
		if !ok {
			t.Fatal("missing async sync projection")
		}
		phase, _ := sync["status"].(string)
		if allowed[phase] == "" || sync["message"] != allowed[phase] {
			t.Fatalf("invalid asynchronous phase: %v", sync)
		}
		status = append(status, phase)
	}
	if status[0] != status[1] {
		for _, value := range []map[string]any{a, b} {
			sync := value["sync"].(map[string]any)
			sync["status"] = "<validated async phase>"
			sync["message"] = "<validated phase message>"
		}
	}
	compareBoundary(t, a, b)
}
func TestR15FlaskManualRuleSyncAcceptedAndSettled(t *testing.T) {
	fake := routerfake.New(nil)
	seed := deviceState()
	fake.Reset(seed)
	server := httptest.NewServer(fake)
	defer server.Close()
	cfg := inputConfig(server.URL)
	r := cfg["routers"].(map[string]any)["items"].([]any)[0].(map[string]any)
	r["auto_rule_sync"] = true
	r["ip_rule_sets"] = []string{"selected"}
	cfg["rule_configs"] = []any{map[string]any{"id": "selected", "name": "Synthetic IPs", "itemType": "ruleset", "enabled": true, "policy": "PROXY", "library_rule_id": "lib"}}
	shared := map[string]any{"rule_library": []any{map[string]any{"id": "lib", "name": "Synthetic IPs", "source_type": "content", "content": "IP-CIDR,203.0.113.0/24\n", "format": "text", "behavior": "classical", "enabled": true}}}
	system := map[string]any{"agents": []any{map[string]any{"id": "gateway", "name": "Synthetic gateway", "host": "192.0.2.4", "port": 9100, "service_type": "mihomo", "profile_id": "default", "enabled": true, "status": "offline", "token": "synthetic-gateway-token"}}}
	steps := []boundaryRequest{{Method: "POST", Path: "/api/routers/router-one/plan", Status: 200}, {Method: "POST", Path: "/api/routers/router-one/apply", Use: "plan", Status: 200}, {Method: "GET", Path: "/api/routers/router-one/rule-sync", Status: 200}, {Method: "POST", Path: "/api/routers/router-one/rule-sync", Status: 202, Settle: true}}
	runHTTPBoundary(t, fake, seed, cfg, system, steps, shared)
}
func TestR15RealFlaskRoutePriorities(t *testing.T) {
	home, _ := os.UserHomeDir()
	wd, _ := os.Getwd()
	cases := []map[string]string{{"method": "GET", "path": "/api/agents/gateway-installations/upstream-routers"}, {"method": "GET", "path": "/api/agents/i/status"}, {"method": "POST", "path": "/api/agents/one/mihomo/rule-targets"}, {"method": "GET", "path": "/api/agents/one/mihomo/rule-targets"}, {"method": "GET", "path": "/api/agents/one/upstream-routers"}}
	raw, _ := json.Marshal(cases)
	cmd := exec.CommandContext(t.Context(), filepath.Join(home, "cf-test/.venv/bin/python"), filepath.Join(wd, "testdata/r15_route_reference.py"))
	cmd.Dir = filepath.Clean(filepath.Join(wd, "../../../.."))
	cmd.Env = append(os.Environ(), "PYTHONPATH=.", "DATA_DIR="+t.TempDir())
	cmd.Stdin = bytes.NewReader(raw)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Flask routes %v %s", err, output)
	}
	var endpoints []string
	if err = json.Unmarshal(output, &endpoints); err != nil {
		t.Fatal(err, string(output))
	}
	expected := []string{"agents.gateway_installation_status", "agents.download_install_command", "agents.mihomo_panel", "agents.rule_targets", "agents.upstream_router_pending"}
	if !reflect.DeepEqual(endpoints, expected) {
		t.Fatalf("Flask priorities changed: %v", endpoints)
	}
}
