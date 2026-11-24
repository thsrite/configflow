package installation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents"
	"configflow/internal/agents/agentfake"
	"configflow/internal/gateway/host"
	"configflow/internal/gateway/installation"
	"configflow/internal/gateway/onboarding"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/installer"
	"configflow/internal/model"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
)

const (
	instBase   = "/api/agents/gateway-installations"
	instOrigin = "https://panel.example.com"
)

func instRaw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func instObj(b []byte) map[string]json.RawMessage {
	var v map[string]json.RawMessage
	_ = json.Unmarshal(b, &v)
	return v
}

func instStr(b []byte) string { var v string; _ = json.Unmarshal(b, &v); return v }

type instClient struct{ destinations map[string]string }

func (c instClient) Do(ctx context.Context, r agents.Request) (*agents.Response, error) {
	u, err := url.Parse(r.URL)
	if err != nil {
		return nil, err
	}
	destination, ok := c.destinations[u.Host]
	if !ok {
		return nil, fmt.Errorf("unregistered origin forbidden")
	}
	v, _ := url.Parse(destination)
	u.Scheme = v.Scheme
	u.Host = v.Host
	r.URL = u.String()
	return (agents.HTTPClient{}).Do(ctx, r)
}

var (
	instHex32    = regexp.MustCompile(`^[0-9a-f]{32,}$`)
	instISO      = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	instShortURL = regexp.MustCompile(`/api/agents/i/[A-Za-z0-9_-]+`)
)

func normalizeInstGolden(v any, instID, instToken string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			switch k {
			case "updated_at", "created_at", "last_heartbeat", "started_at", "finished_at":
				if s, ok := val.(string); ok && s != "" {
					out[k] = "@time"
					continue
				}
			case "config_token", "rule_proxy_token", "jwt_secret_key", "script_token", "install_token":
				if s, ok := val.(string); ok && s != "" {
					out[k] = "@token"
					continue
				}
			}
			out[k] = normalizeInstGolden(val, instID, instToken)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeInstGolden(item, instID, instToken)
		}
		return out
	case string:
		s := x
		if instID != "" {
			s = strings.ReplaceAll(s, instID, "@inst-id")
		}
		if instToken != "" {
			s = strings.ReplaceAll(s, instToken, "@inst-token")
		}
		if instISO.MatchString(s) {
			return "@time"
		}
		if instHex32.MatchString(s) {
			return "@hex"
		}
		return instShortURL.ReplaceAllString(s, "/api/agents/i/@short-id")
	}
	return v
}

func waitInstPublished(t *testing.T, s *httpxtest.Server, path, admin string) {
	t.Helper()
	deadline := time.NewTimer(time.Minute)
	defer deadline.Stop()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	for {
		reply := s.Get(path, httpxtest.Bearer(admin)).WantStatus(200)
		phase := instStr(instObj(reply.Body)["phase"])
		if phase == "published" {
			return
		}
		if phase == "publish_failed" {
			t.Fatalf("publication failed: %s", instStr(instObj(reply.Body)["message"]))
		}
		select {
		case <-deadline.C:
			t.Fatal("publication did not settle")
		case <-poll.C:
		}
	}
}

func TestGatewayInstallationGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	s, app := servertest.New(t, httpxtest.Options{Auth: true})
	static := t.TempDir()
	t.Setenv("AGENTS_STATIC_DIR", static)
	if err := os.WriteFile(filepath.Join(static, "configflow-agent-linux-amd64"), []byte("synthetic-agent-binary"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx := s.Context()
	if err := s.Repo.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		return json.Unmarshal([]byte(`[{"id":"n","name":"Example","type":"ss","server":"192.0.2.2","port":443,"cipher":"aes-128-gcm","password":"synthetic-node-secret"}]`), &shared.Nodes)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		if err := json.Unmarshal([]byte(`[{"id":"g","name":"Proxy","type":"select","manual_nodes":["n"]}]`), &p.ProxyGroups); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(`[{"id":"match","itemType":"rule","rule_type":"MATCH","policy":"Proxy"}]`), &p.RuleConfigs); err != nil {
			return err
		}
		p.Mihomo.CustomConfig = "mixed-port: 7890\n"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	on := onboarding.Service{Repository: s.Repo}
	if _, err := on.Execute(ctx, onboarding.Operation{
		ProfileID: "default",
		BaseURL:   instOrigin,
		Write:     true,
		Body:      instRaw(map[string]any{"rule_set_ids": []string{}, "direct_rule_set_ids": []string{}, "proxy_rule_set_ids": []string{}, "local_dns": "https://192.0.2.53/dns-query"}),
	}); err != nil {
		t.Fatal(err)
	}
	m := app.Agents
	svc := installation.Service{Repository: s.Repo, Manager: m, Deploy: app.Deploy, Spawner: app.Runtime}
	revision, err := s.Repo.ConfigRevision(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	createBody := map[string]json.RawMessage{
		"profile_id":      instRaw("default"),
		"name":            instRaw("synthetic-linux"),
		"server_url":      instRaw(instOrigin),
		"network":         instRaw(host.Spec{Interface: "eth0", Address: "192.0.2.10", LANCIDR: "192.0.2.0/24", UpstreamGateway: "192.0.2.1"}),
		"config_revision": instRaw(revision),
	}
	out, err := svc.Create(ctx, instRaw(createBody))
	if err != nil {
		t.Fatal(err)
	}
	instFile := filepath.Join(s.DataDir, "gateway-installations", instStr(instObj(out)["id"])+".json")
	data, err := os.ReadFile(instFile)
	if err != nil {
		t.Fatal(err)
	}
	var r installation.Record
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"mihomo", "oxidns"} {
		r.Deployments[kind] = instRaw(map[string]string{"deployment_id": "job-" + kind, "status": "preparing"})
	}
	if err := os.WriteFile(instFile, instRaw(r), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Repo.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		var list []model.Agent
		for _, kind := range []string{"mihomo", "oxidns"} {
			var agent model.Agent
			b := instRaw(map[string]any{
				"id":                      "synthetic-" + kind,
				"name":                    r.Name + "-" + kind,
				"host":                    r.Network.Address,
				"port":                    r.Ports[kind],
				"token":                   "synthetic-token-" + kind,
				"service_type":            kind,
				"profile_id":              r.ProfileID,
				"deployment_method":       "shell",
				"version":                 "1.8.60-go",
				"enabled":                 true,
				"gateway_installation_id": r.ID,
				"created_at":              "2026-01-01T00:00:00+00:00",
			})
			if err := json.Unmarshal(b, &agent); err != nil {
				return err
			}
			list = append(list, agent)
		}
		sys.Agents = list
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	store := installer.Store{DataDir: s.DataDir}
	for range installer.MaxSnapshots - 1 {
		if _, _, err := store.SaveInstallScript(ctx, "#!/bin/sh\n# synthetic frozen installer\n", nil); err != nil {
			t.Fatal(err)
		}
	}

	dest := map[string]string{}
	for _, kind := range []string{"mihomo", "oxidns"} {
		fake := agentfake.New(agentfake.Options{AgentID: "synthetic-" + kind, Token: "synthetic-token-" + kind, ServiceType: kind})
		handler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			if req.URL.Path == "/api/linux-gateway/status" {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(instRaw(map[string]any{
					"success":   true,
					"supported": true,
					"managed":   map[string]bool{"active": false},
					"detection": map[string]any{
						"suggested":      r.Network,
						"interfaces":     []any{map[string]any{"name": r.Network.Interface, "addresses": []string{r.Network.Address + "/24"}}},
						"default_routes": []any{map[string]any{"interface": r.Network.Interface, "gateway": r.Network.UpstreamGateway}},
					},
				}))
				return
			}
			fake.ServeHTTP(w, req)
		})
		receiver := httptest.NewServer(handler)
		t.Cleanup(receiver.Close)
		dest[fmt.Sprintf("%s:%d", r.Network.Address, r.Ports[kind])] = receiver.URL
	}
	m.SetClient(instClient{dest})

	login := s.Post("/api/auth/login", httpxtest.JSON(map[string]string{"username": httpxtest.Username, "password": httpxtest.Password})).WantStatus(200)
	admin := instStr(instObj(login.Body)["token"])

	record := func(label, method, reqPath, body, token string) {
		t.Helper()
		var opts []httpxtest.Option
		if body != "" {
			opts = append(opts, httpxtest.Body([]byte(body), "text/plain"))
		}
		if token != "" {
			opts = append(opts, httpxtest.Bearer(token))
		}
		resp := s.Do(method, reqPath, opts...)
		var parsed any
		if method == "HEAD" {
			parsed = ""
		} else if err := json.Unmarshal(resp.Body, &parsed); err != nil {
			parsed = string(resp.Body)
		}
		key, _ := json.Marshal(map[string]string{"case": label})
		actual, _ := json.Marshal(normalizeInstGolden(map[string]any{
			"status":          resp.Status,
			"cache_control":   resp.Header.Get("Cache-Control"),
			"referrer_policy": resp.Header.Get("Referrer-Policy"),
			"body":            parsed,
		}, r.ID, r.Token))
		ref.Assert(t, key, actual)
	}

	path := instBase + "/" + r.ID
	record("list-initial", "GET", instBase+"?profile_id=default", "", admin)
	record("get-initial", "GET", path, "", admin)
	record("head-initial", "HEAD", path, "", admin)
	record("create-replay", "POST", instBase, string(instRaw(createBody)), admin)

	full := instObj(instRaw(createBody))
	full["name"] = instRaw("different-linux")
	record("create-snapshots-full", "POST", instBase, string(instRaw(full)), admin)
	record("create-malformed", "POST", instBase, "{", admin)
	record("create-array", "POST", instBase, "[]", admin)
	invalid := instObj(instRaw(createBody))
	invalid["auto_network"] = instRaw("true")
	record("create-bad-auto-network", "POST", instBase, string(instRaw(invalid)), admin)
	invalid = instObj(instRaw(createBody))
	delete(invalid, "config_revision")
	record("create-missing-revision", "POST", instBase, string(instRaw(invalid)), admin)
	record("create-oversized", "POST", instBase, strings.Repeat(" ", 16385), admin)
	record("get-unauth", "GET", path, "", "")
	record("enroll-empty", "POST", path+"/enroll", `{}`, r.Token)
	record("enroll-bad-proof", "POST", path+"/enroll",
		`{"agents":{"mihomo":{"id":"synthetic-mihomo","token":"wrong"},"oxidns":{"id":"synthetic-oxidns","token":"wrong"}}}`, r.Token)
	record("script-bad-token", "GET", path+"/script?token=wrong", "", admin)
	scriptResp := s.Get(path + "/script?token=" + r.Token).WantStatus(200)
	if scriptResp.Header.Get("Referrer-Policy") != "no-referrer" || !strings.Contains(string(scriptResp.Body), "#!/") {
		t.Fatalf("unexpected frozen script response: headers=%v", scriptResp.Header)
	}
	record("network-before-enroll", "POST", path+"/network", `{"auto_network":true}`, admin)
	record("publish-before-enroll", "POST", path+"/publish", `{}`, admin)

	for _, kind := range []string{"mihomo", "oxidns"} {
		reg := instRaw(map[string]any{
			"name":               r.Name + "-" + kind,
			"host":               r.Network.Address,
			"port":               r.Ports[kind],
			"service_type":       kind,
			"deployment_method":  "shell",
			"version":            "1.8.60-go",
			"installation_id":    r.ID,
			"installation_token": r.Token,
		})
		record("register-"+kind, "POST", "/api/agents/register", string(reg), "")
	}
	proofs := map[string]any{}
	for _, kind := range []string{"mihomo", "oxidns"} {
		proofs[kind] = map[string]string{"id": "synthetic-" + kind, "token": "synthetic-token-" + kind}
	}
	record("enroll-valid", "POST", path+"/enroll", string(instRaw(map[string]any{"agents": proofs})), r.Token)
	record("list-after-enroll", "GET", instBase+"?profile_id=default", "", admin)
	record("publish-bad-retry", "POST", path+"/publish", `{"retry_failed":1}`, admin)
	record("get-missing", "GET", instBase+"/bad", "", admin)
	record("network-apply", "POST", path+"/network", `{"auto_network":true}`, admin)
	record("publish-start", "POST", path+"/publish", `{}`, admin)

	waitInstPublished(t, s, path, admin)
	record("get-published", "GET", path, "", admin)

	if _, err := s.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		p.Description = "changed"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	record("publish-stale-profile", "POST", path+"/publish", `{}`, admin)
}
