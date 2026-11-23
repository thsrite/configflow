package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

// o10Fixture is a fully wired application on a synthetic data set of about
// the production snapshot's size (O10 R11): two configuration spaces from the
// default template, two subscriptions with cached nodes (90 and 160), one
// aggregation, five manual nodes, one RouterOS router and three Agents whose
// deployment and IPv6 verification histories make up most of the document,
// like in production. Every value is synthetic.
type o10Fixture struct {
	t       *testing.T
	srv     *httpxtest.Server
	app     *httpx.App
	agents  []o10Agent
	routers []string
}

type o10Agent struct{ id, token string }

const (
	o10SubA        = "sub_1760688109177" // the template's subscription id, referenced by its groups
	o10SubB        = "sub-synthetic-b"
	o10Aggregation = "agg-synthetic"
)

func newO10Fixture(t *testing.T) *o10Fixture {
	t.Helper()
	app := NewApp()
	srv := httpxtest.New(t, httpxtest.Options{DefaultFactories: true})
	f := &o10Fixture{t: t, srv: srv, app: app}
	startO10Runtime(t)
	ctx := srv.Context()

	raw, err := srv.Repo.ExportAllJSON(ctx, false)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	profiles := doc["profiles"].(map[string]any)
	base := profiles["default"].(map[string]any)
	// The second configuration space is a copy of the template.
	var phone map[string]any
	copyJSON(t, base, &phone)
	phone["id"], phone["name"] = "phone", "Synthetic phone"
	profiles["phone"] = phone
	for _, profile := range []map[string]any{base, phone} {
		for _, item := range profile["proxy_groups"].([]any) {
			group := item.(map[string]any)
			subs, _ := group["subscriptions"].([]any)
			if len(subs) > 0 {
				group["subscriptions"] = append(subs, o10SubB)
			}
			// Region groups also take the aggregation's nodes, as in production.
			if regex, _ := group["regex"].(string); len(subs) > 0 || regex != "" {
				group["aggregations"] = []any{o10Aggregation}
				if regex != "" {
					group["aggregation_regex"] = regex
				}
			}
		}
	}
	shared := doc["shared"].(map[string]any)
	shared["subscriptions"] = []any{
		map[string]any{"id": o10SubA, "name": "Feed A", "type": "universal", "enabled": true, "interval": 86400, "url": o10FeedURL(o10SubA)},
		map[string]any{"id": o10SubB, "name": "Feed B", "type": "universal", "enabled": true, "interval": 86400, "url": o10FeedURL(o10SubB)},
	}
	shared["subscription_aggregations"] = []any{map[string]any{"id": o10Aggregation, "name": "Aggregate", "nodes": []any{}, "subscriptions": []any{o10SubA, o10SubB}}}
	var manual []any
	for i := range 5 {
		manual = append(manual, map[string]any{
			"id": fmt.Sprintf("node-manual-%d", i), "name": fmt.Sprintf("Manual %d", i), "type": "ss", "protocol": "ss",
			"server": fmt.Sprintf("198.51.100.%d", 10+i), "port": 8388 + i, "enabled": true,
			"params": map[string]any{"cipher": "aes-128-gcm", "password": fmt.Sprintf("synthetic-manual-%d", i)},
		})
	}
	shared["nodes"] = manual
	library := shared["rule_library"].([]any)
	for i := len(library); i < 44; i++ {
		var lines []string
		for j := range 40 {
			lines = append(lines, fmt.Sprintf("DOMAIN-SUFFIX,svc%02d-%02d.example.com", i, j))
		}
		library = append(library, map[string]any{"id": fmt.Sprintf("lib-synthetic-%02d", i), "name": fmt.Sprintf("Synthetic %02d", i),
			"behavior": "classical", "content": strings.Join(lines, "\n"), "enabled": true})
	}
	shared["rule_library"] = library

	system := doc["system"].(map[string]any)
	var agents []any
	for i, spec := range []struct{ service, profile string }{{"mihomo", "default"}, {"oxidns", "default"}, {"mihomo", "phone"}} {
		agent := o10Agent{id: fmt.Sprintf("agent-synthetic-%d", i), token: fmt.Sprintf("synthetic-agent-token-%d", i)}
		f.agents = append(f.agents, agent)
		agents = append(agents, o10AgentRecord(agent, spec.service, spec.profile, i))
	}
	system["agents"] = agents
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.ImportAllJSON(ctx, data); err != nil {
		t.Fatal(err)
	}
	if len(data) < 300_000 {
		t.Fatalf("fixture document is only %d bytes", len(data))
	}

	for _, sub := range []struct {
		id    string
		nodes int
	}{{o10SubA, 90}, {o10SubB, 160}} {
		proxies, err := substore.ParseProxyNodes(o10ProxiesYAML(sub.id, sub.nodes))
		if err != nil {
			t.Fatal(err)
		}
		nodes, err := substore.ProxyNodes(proxies)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := substore.SaveCache(ctx, sub.id, nodes, &substore.CacheMetadata{URL: new(o10FeedURL(sub.id)), SubscriptionName: sub.id, FetchedAt: new(time.Now().UTC().Format(time.RFC3339))}); err != nil {
			t.Fatal(err)
		}
	}

	created := f.do("POST", "/api/routers", "", map[string]any{"name": "Synthetic router", "type": "routeros", "endpoint": "http://192.0.2.1", "username": "synthetic-user", "password": "synthetic-router-password"})
	if created.Code != 200 {
		t.Fatalf("router: %d %s", created.Code, created.Body)
	}
	var router struct {
		Router struct {
			ID string `json:"id"`
		} `json:"router"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &router); err != nil || router.Router.ID == "" {
		t.Fatalf("router id: %v %s", err, created.Body)
	}
	f.routers = append(f.routers, router.Router.ID)
	return f
}

// startO10Runtime gives detached request work (provider renders) a fresh
// Runtime without any background loop.
func startO10Runtime(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 2 * time.Second})
		if err == nil {
			break
		}
		if !errors.Is(err, lifecycle.ErrRunning) || time.Now().After(deadline) {
			t.Fatal(err)
		}
		time.Sleep(time.Millisecond)
	}
	lifecycle.Start(context.Background())
	t.Cleanup(func() {
		lifecycle.Stop(3 * time.Second)
		if err := lifecycle.Configure(lifecycle.Options{Registry: lifecycle.NewRegistry()}); err != nil {
			t.Error(err)
		}
	})
}

func copyJSON(t *testing.T, in, out any) {
	t.Helper()
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatal(err)
	}
}

func o10FeedURL(id string) string { return "https://feed.example.com/" + id + "?token=synthetic" }

func o10ProxiesYAML(id string, n int) string {
	regions := []string{"🇭🇰 香港", "🇯🇵 日本", "🇺🇸 美国", "🇸🇬 新加坡", "🇹🇼 台湾", "🇰🇷 韩国"}
	var b strings.Builder
	b.WriteString("proxies:\n")
	for i := range n {
		name := fmt.Sprintf("%s %02d | %s", regions[i%len(regions)], i, id)
		server := fmt.Sprintf("node%03d.%s.example.com", i, id)
		switch i % 3 {
		case 0:
			fmt.Fprintf(&b, "  - {name: %q, type: ss, server: %s, port: %d, cipher: aes-128-gcm, password: synthetic-%d, udp: true}\n", name, server, 10000+i, i)
		case 1:
			fmt.Fprintf(&b, "  - {name: %q, type: vmess, server: %s, port: 443, uuid: 00000000-0000-4000-8000-%012d, alterId: 0, cipher: auto, tls: true, servername: %s, network: ws, ws-opts: {path: /ws, headers: {Host: %s}}}\n", name, server, i, server, server)
		default:
			fmt.Fprintf(&b, "  - {name: %q, type: trojan, server: %s, port: 443, password: synthetic-%d, sni: %s, skip-cert-verify: false, udp: true}\n", name, server, i, server)
		}
	}
	return b.String()
}

// o10AgentRecord is an Agent with about 100 KB of deployment and IPv6
// verification history.
func o10AgentRecord(agent o10Agent, service, profile string, index int) map[string]any {
	deployments := map[string]any{}
	var latest map[string]any
	for d := range 40 {
		id := fmt.Sprintf("dep-%d-%03d", index, d)
		at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(d) * 7 * time.Hour).Format("2006-01-02T15:04:05.000000Z")
		var events []any
		for e, status := range []string{"queued", "preparing", "uploading", "validating", "reloading", "health", "dns", "completed"} {
			events = append(events, map[string]any{"at": at, "status": status, "message": fmt.Sprintf("步骤 %d：%s，耗时 %d ms，目标 %s", e, status, 100+e*37, agent.id)})
		}
		status := "success"
		var errText any
		if d%7 == 3 {
			status, errText = "failed", strings.Repeat("Connection refused while reloading the service; ", 8)
		}
		deployment := map[string]any{
			"success": status == "success", "deployment_id": id, "status": status, "profile_id": profile, "service_type": service,
			"created_at": at, "updated_at": at, "events": events, "error": errText, "error_details": errText,
			"config_version": fmt.Sprintf("%064x", d+index*1000), "config_revision": d + 1, "sha256": fmt.Sprintf("%064x", d*31+index),
			"dns_checks": []any{
				map[string]any{"name": "upstream", "level": "ok", "message": "上游 DNS 可用：198.51.100.53"},
				map[string]any{"name": "listen", "level": "warning", "message": "监听端口 53 已被占用，已改用 1053", "fix": "停止占用 53 端口的服务"},
			},
			"prepared_content": map[string]any{"fingerprint": fmt.Sprintf("%064x", d+7), "profile_id": profile, "service_type": service,
				"config_path": "config.yaml", "base_url": "https://panel.example.com",
				"files": map[string]any{"config.yaml": fmt.Sprintf("%064x", d), "rules/direct.list": fmt.Sprintf("%064x", d+1), "rules/proxy.list": fmt.Sprintf("%064x", d+2)}},
		}
		deployments[id] = deployment
		latest = deployment
	}
	verifications := map[string]any{}
	for v := range 12 {
		run := fmt.Sprintf("run-%d-%02d", index, v)
		var checks []any
		for c := range 6 {
			checks = append(checks, map[string]any{"id": fmt.Sprintf("check-%d", c), "status": "passed", "message": fmt.Sprintf("IPv6 检查 %d 通过：2001:db8::%x", c, c+1)})
		}
		verifications[run] = map[string]any{"status": "passed", "scope": "full", "checks": checks, "started_at": "2026-11-01T00:00:00Z",
			"started_epoch": 1793491200 + v, "run_id": run, "profile_id": profile, "settings_fingerprint": fmt.Sprintf("%064x", v),
			"target_fingerprint": fmt.Sprintf("%064x", v+1), "applied_fingerprint": fmt.Sprintf("%064x", v+2)}
	}
	return map[string]any{
		"id": agent.id, "name": "Synthetic " + agent.id, "host": fmt.Sprintf("192.0.2.%d", 20+index), "port": 8080,
		"token": agent.token, "service_type": service, "profile_id": profile, "deployment_method": "agent", "enabled": true,
		"created_at": "2026-09-01T00:00:00Z", "status": "online", "last_heartbeat": time.Now().UTC().Format("2006-01-02T15:04:05.000000"),
		"version": "1.9.1-go", "config_version": latest["config_version"], "config_revision": 40,
		"deployments": deployments, "latest_deployment": latest, "ipv6_verifications": verifications,
		"applied_content": map[string]any{"fingerprint": fmt.Sprintf("%064x", 46), "profile_id": profile, "service_type": service, "config_path": "config.yaml",
			"base_url": "https://panel.example.com", "files": map[string]any{"config.yaml": fmt.Sprintf("%064x", 39)}},
	}
}

// do sends one request through the whole application (authentication gate
// included) bound to the fixture repository.
func (f *o10Fixture) do(method, path, token string, body any) *httptest.ResponseRecorder {
	f.t.Helper()
	var req *http.Request
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			f.t.Fatal(err)
		}
		req = httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(string(data)))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, "http://localhost"+path, http.NoBody)
	}
	req.RemoteAddr = "127.0.0.1:40000"
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	f.app.ServeHTTP(recorder, req.WithContext(repo.WithRepository(req.Context(), f.srv.Repo)))
	return recorder
}
