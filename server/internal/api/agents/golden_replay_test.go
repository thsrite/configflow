package agents_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
)

type fakePanelAgent struct {
	mu    sync.Mutex
	phase string
	calls []string
}

func (f *fakePanelAgent) setPhase(phase string) {
	f.mu.Lock()
	f.phase = phase
	f.calls = nil
	f.mu.Unlock()
}

func (f *fakePanelAgent) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}

func (f *fakePanelAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	phase := f.phase
	f.calls = append(f.calls, fmt.Sprintf("%s %s?%s body=%s", r.Method, r.URL.EscapedPath(), r.URL.RawQuery, string(body)))
	f.mu.Unlock()
	send := func(status int, v any) {
		b, _ := json.Marshal(v)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(b)
	}
	switch phase {
	case "401":
		send(401, map[string]any{"message": "nope"})
		return
	case "302":
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
		return
	case "404text":
		w.WriteHeader(404)
		_, _ = io.WriteString(w, "404 page not found")
		return
	case "notjson":
		w.WriteHeader(200)
		_, _ = io.WriteString(w, "not json")
		return
	case "empty":
		w.WriteHeader(204)
		return
	case "controller":
		send(409, map[string]any{"success": false, "code": "CONTROLLER_UNAVAILABLE", "message": "secret detail"})
		return
	case "timeout":
		send(504, map[string]any{"message": "Timeout"})
		return
	case "partial":
		send(502, map[string]any{"success": false, "code": "CACHE_FLUSH_FAILED", "message": "core secret", "flushed": []string{"main_cache"}})
		return
	case "cacheunknown":
		send(409, map[string]any{"success": false, "code": "CACHE_UNKNOWN"})
		return
	}
	switch r.URL.Path {
	case "/api/mihomo/version":
		send(200, map[string]any{"version": "v1.19.0", "meta": true})
	case "/api/mihomo/proxies":
		send(200, map[string]any{"proxies": map[string]any{
			"HK": map[string]any{"name": "HK", "type": "Trojan", "alive": true, "history": []any{map[string]any{"time": "t1", "delay": 120}}},
		}})
	case "/api/oxidns/cache":
		send(200, map[string]any{"success": true, "caches": []any{map[string]any{"tag": "main_cache", "size": 42}}})
	case "/api/oxidns/cache/flush":
		send(200, map[string]any{"success": true, "flushed": []string{"probe", "main_cache"}})
	default:
		send(200, map[string]any{"success": true, "path": r.URL.Path})
	}
}

var (
	agISO    = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	agHexID  = regexp.MustCompile(`^(agent_[0-9a-f_]{8,}|[0-9a-f]{32,})$`)
	agLocalP = regexp.MustCompile(`127\.0\.0\.1:\d+`)
)

func normalizeAgentsGolden(v any, captured map[string]string) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			switch k {
			case "created_at", "updated_at", "last_heartbeat", "timestamp", "collected_at":
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			case "token":
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<token>"
					continue
				}
			case "id", "agent_id":
				if s, ok := val.(string); ok && (agHexID.MatchString(s) || (captured["id"] != "" && s == captured["id"])) {
					out[k] = "<agent_id>"
					continue
				}
			}
			out[k] = normalizeAgentsGolden(val, captured)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeAgentsGolden(item, captured)
		}
		return out
	case string:
		s := x
		if captured["id"] != "" {
			s = strings.ReplaceAll(s, captured["id"], "<agent_id>")
		}
		if captured["token"] != "" {
			s = strings.ReplaceAll(s, captured["token"], "<token>")
		}
		if agISO.MatchString(s) {
			return "<time>"
		}
		return agLocalP.ReplaceAllString(s, "127.0.0.1:<port>")
	}
	return v
}

func TestAgentsAPIGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	t.Setenv("AGENT_REGISTRATION_KEY", "test-reg-key")

	fake := &fakePanelAgent{}
	fakeSrv := httptest.NewServer(fake)
	t.Cleanup(fakeSrv.Close)
	_, portStr, _ := net.SplitHostPort(strings.TrimPrefix(fakeSrv.URL, "http://"))
	fakePort, _ := strconv.Atoi(portStr)

	s, _ := servertest.New(t, httpxtest.Options{
		Auth: true,
	})
	ctx := s.Context()
	if _, err := s.Repo.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
		t.Fatal(err)
	}
	const agentTok = "diff-agent-token-0123456789"
	if err := s.Repo.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		sys.SystemConfig.ServerDomain = "http://127.0.0.1:5001"
		raw, err := json.Marshal([]any{
			map[string]any{"id": "agent-1", "name": "diff agent", "token": agentTok, "profile_id": "default", "service_type": "mihomo", "host": "127.0.0.1", "port": 9, "version": "1.0.0", "config_version": "0", "enabled": true},
			map[string]any{"id": "agent-busy", "name": "busy agent", "token": agentTok + "-busy", "profile_id": "default", "service_type": "mihomo", "host": "192.0.2.2", "port": 9, "version": "1.0.0", "config_version": "0", "enabled": true,
				"deployments": map[string]any{"binding-request": map[string]any{"deployment_id": "binding-request", "status": "preparing", "profile_id": "default", "service_type": "mihomo"}}},
			map[string]any{"id": "agent-upgrading", "name": "upgrading agent", "token": agentTok + "-upgrading", "profile_id": "default", "service_type": "mihomo", "host": "127.0.0.3", "port": 9, "version": "1.0.0", "config_version": "0", "enabled": true,
				"latest_upgrade": map[string]any{"status": "downloading"}},
			map[string]any{"id": "gw", "name": "panel gw", "token": "tok-gw", "profile_id": "default", "service_type": "mihomo", "host": "127.0.0.1", "port": fakePort, "version": "1.8.60-go", "enabled": true},
			map[string]any{"id": "dns", "name": "panel dns", "token": "tok-dns", "profile_id": "default", "service_type": "oxidns", "host": "127.0.0.1", "port": fakePort, "version": "1.8.60-go", "enabled": true},
		})
		if err != nil {
			return err
		}
		return sys.SetJSON("agents", raw)
	}); err != nil {
		t.Fatal(err)
	}

	admin := s.Login()
	captured := map[string]string{}

	record := func(label, method, path, body string, useAdmin bool, headers map[string]string, phase string) map[string]any {
		t.Helper()
		fake.setPhase(phase)
		for k, v := range captured {
			path = strings.ReplaceAll(path, "{"+k+"}", v)
			body = strings.ReplaceAll(body, "{"+k+"}", v)
		}
		var opts []httpxtest.Option
		if useAdmin {
			opts = append(opts, admin)
		}
		for k, v := range headers {
			for ck, cv := range captured {
				v = strings.ReplaceAll(v, "{"+ck+"}", cv)
			}
			opts = append(opts, httpxtest.Header(k, v))
		}
		if body != "" {
			opts = append(opts, httpxtest.Body([]byte(body), "application/json"))
		}
		resp := s.Do(method, path, opts...)
		var parsed any
		if method == "HEAD" {
			parsed = ""
		} else if err := json.Unmarshal(resp.Body, &parsed); err != nil {
			parsed = string(resp.Body)
		}
		key, _ := json.Marshal(map[string]string{"case": label})
		actual, _ := json.Marshal(normalizeAgentsGolden(map[string]any{
			"status": resp.Status,
			"body":   parsed,
			"calls":  fake.take(),
		}, captured))
		ref.Assert(t, key, actual)
		m, _ := parsed.(map[string]any)
		return m
	}

	// Registry + heartbeat + metrics + CRUD.
	regBody := `{"name":"edge","host":"192.0.2.8","port":8080,"service_type":"mihomo","version":"1.2.3","registration_key":"test-reg-key"}`
	created := record("reg-new", "POST", "/api/agents/register", regBody, false, nil, "")
	if id, ok := created["id"].(string); ok {
		captured["id"] = id
	}
	if tok, ok := created["token"].(string); ok {
		captured["token"] = tok
	}
	record("reg-dup-notoken", "POST", "/api/agents/register", regBody, false, nil, "")
	record("reg-dup-wrongtoken", "POST", "/api/agents/register", regBody, false, map[string]string{"Authorization": "Bearer wrong"}, "")
	record("reg-rereg-token", "POST", "/api/agents/register", `{"name":"edge","host":"192.0.2.8","port":9090,"service_type":"mihomo","version":"1.3.0"}`, false, map[string]string{"Authorization": "Bearer {token}"}, "")
	record("reg-bad-json", "POST", "/api/agents/register", `{"name":`, false, nil, "")
	record("reg-null", "POST", "/api/agents/register", `null`, false, nil, "")
	record("reg-list", "POST", "/api/agents/register", `[1]`, false, nil, "")
	record("reg-missing-profile", "POST", "/api/agents/register", `{"name":"p","host":"192.0.2.20","profile_id":"missing"}`, false, nil, "")
	record("reg-other-profile", "POST", "/api/agents/register", `{"name":"p","host":"192.0.2.20","profile_id":"other","service_type":"oxidns","deployment_method":"docker"}`, false, nil, "")

	hbMetrics := `{"service_status":"running","system_metrics":{"cpu":{"usage_percent":12.5,"core_count":4},"memory":{"total":1024,"used":512,"available":512,"used_percent":50.0},"collected_at":"2026-08-31T00:00:00Z"}}`
	record("hb-ok", "POST", "/api/agents/agent-1/heartbeat", hbMetrics, false, map[string]string{"Authorization": "Bearer " + agentTok}, "")
	record("hb-wrong-bearer", "POST", "/api/agents/{id}/heartbeat", `{"version":"attacker"}`, false, map[string]string{"Authorization": "Bearer wrong"}, "")
	record("hb-nan-metric", "POST", "/api/agents/{id}/heartbeat", `{"system_metrics":{"cpu":{"usage_percent":NaN}}}`, false, map[string]string{"Authorization": "Bearer {token}"}, "")

	record("metrics-latest", "GET", "/api/agents/agent-1/metrics", "", true, nil, "")
	record("metrics-no-login", "GET", "/api/agents/agent-1/metrics", "", false, nil, "")
	record("metrics-missing", "GET", "/api/agents/missing/metrics", "", true, nil, "")
	record("metrics-history-bad-hours", "GET", "/api/agents/agent-1/metrics/history?hours=abc", "", true, nil, "")

	record("update-busy-409", "PUT", "/api/agents/agent-busy", `{"profile_id":"other"}`, true, nil, "")
	record("delete-upgrading-409", "DELETE", "/api/agents/agent-upgrading", "", true, nil, "")
	record("delete-registered", "DELETE", "/api/agents/{id}", "", true, nil, "")
	record("delete-missing", "DELETE", "/api/agents/missing", "", true, nil, "")

	// Mihomo panel relay + OxiDNS cache routes.
	record("panel-version", "GET", "/api/agents/gw/mihomo/version", "", true, nil, "")
	record("panel-unauth", "GET", "/api/agents/gw/mihomo/version", "", false, nil, "")
	record("panel-agent-token", "GET", "/api/agents/gw/mihomo/version", "", false, map[string]string{"Authorization": "Bearer tok-gw"}, "")
	record("panel-not-allowed", "GET", "/api/agents/gw/mihomo/restart", "", true, nil, "")
	record("panel-head", "HEAD", "/api/agents/gw/mihomo/version", "", true, nil, "")
	record("panel-proxies", "GET", "/api/agents/gw/mihomo/proxies", "", true, nil, "")
	record("panel-switch", "PUT", "/api/agents/gw/mihomo/proxy?name=Group", `{"name":"HK"}`, true, nil, "")
	record("panel-mode", "PATCH", "/api/agents/gw/mihomo/configs", `{"mode":"global"}`, true, nil, "")
	record("panel-mode-bad", "PATCH", "/api/agents/gw/mihomo/configs", `{"mode":"global","tun":{"enable":false}}`, true, nil, "")
	record("panel-del-conn", "DELETE", "/api/agents/gw/mihomo/connection?name=abc", "", true, nil, "empty")
	record("panel-oxidns-wrong-type", "GET", "/api/agents/dns/mihomo/version", "", true, nil, "")
	record("panel-missing-agent", "GET", "/api/agents/missing/mihomo/version", "", true, nil, "")
	for _, phase := range []string{"401", "302", "404text", "notjson", "controller", "timeout"} {
		record("panel-phase-"+phase, "GET", "/api/agents/gw/mihomo/version", "", true, nil, phase)
	}

	record("cache-list", "GET", "/api/agents/dns/oxidns/cache", "", true, nil, "")
	record("cache-flush-ok", "POST", "/api/agents/dns/oxidns/cache/flush", `{"tags":["probe","main_cache"]}`, true, nil, "")
	record("cache-flush-unauth", "POST", "/api/agents/dns/oxidns/cache/flush", `{"tags":["probe"]}`, false, nil, "")
	record("cache-flush-bad-tag", "POST", "/api/agents/dns/oxidns/cache/flush", `{"tags":["../reload"]}`, true, nil, "")
	record("cache-flush-dup", "POST", "/api/agents/dns/oxidns/cache/flush", `{"tags":["a","a"]}`, true, nil, "")
	record("cache-flush-partial", "POST", "/api/agents/dns/oxidns/cache/flush", `{"tags":["main_cache","probe"]}`, true, nil, "partial")
	record("cache-flush-unknown", "POST", "/api/agents/dns/oxidns/cache/flush", `{"tags":["probe"]}`, true, nil, "cacheunknown")
	record("cache-mihomo-wrong-type", "GET", "/api/agents/gw/oxidns/cache", "", true, nil, "")
}
