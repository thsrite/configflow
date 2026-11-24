package discovery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server/servertest"
	"configflow/internal/testgolden"
)

var (
	discGenID = regexp.MustCompile(`^(?:lib|ruleset)_[0-9a-f-]{32,36}$`)
	discISO   = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	discJobID = regexp.MustCompile(`^[0-9a-f]{16,}$`)
)

func normalizeDiscGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			switch k {
			case "created_at", "updated_at", "started_at", "finished_at", "checked_at", "probed_at", "timestamp", "at":
				if s, ok := val.(string); ok && s != "" {
					out[k] = "<time>"
					continue
				}
			case "id":
				if s, ok := val.(string); ok && (discGenID.MatchString(s) || discJobID.MatchString(s)) {
					out[k] = "<id>"
					continue
				}
			}
			out[k] = normalizeDiscGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeDiscGolden(item)
		}
		return out
	case string:
		if discISO.MatchString(x) {
			return "<time>"
		}
		if discGenID.MatchString(x) {
			return "<id>"
		}
		return x
	}
	return v
}

func TestDiscoveryGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	var mu sync.Mutex
	var calls []map[string]any
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"success": true,
				"targets": []any{
					map[string]any{"name": "DIRECT", "type": "Direct"},
					map[string]any{"name": "PROXY", "type": "Selector"},
					map[string]any{"name": "Z-PREFERRED", "type": "Selector"},
					map[string]any{"name": "A-OTHER", "type": "Selector"},
				},
			})
			return
		}
		var p map[string]any
		_ = json.NewDecoder(r.Body).Decode(&p)
		mu.Lock()
		calls = append(calls, p)
		mu.Unlock()
		results := []any{}
		if r.URL.Path == "/api/domain-probe" {
			for i, target := range p["targets"].([]any) {
				paths := map[string]any{}
				for _, path := range p["paths"].([]any) {
					ok := 3
					if i >= 2 && path == "DIRECT" {
						ok = 0
					}
					paths[path.(string)] = map[string]any{"ok": ok, "total": 3, "delays": []int{100, 100, 100}, "errors": map[string]int{}}
				}
				results = append(results, map[string]any{"host": target.(map[string]any)["host"], "paths": paths})
			}
		} else {
			for _, req := range p["requests"].([]any) {
				rr := req.(map[string]any)
				entry := map[string]any{"id": rr["id"], "status": 200, "final_url": rr["url"]}
				if !strings.Contains(fmt.Sprint(rr["url"]), "nomarkers.example.net") {
					entry["markers"] = map[string]string{"title": "Synthetic Example", "loc": "US", "ip": "192.0.2.1"}
				}
				results = append(results, entry)
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"success": true, "results": results})
	}))
	t.Cleanup(agent.Close)
	port := agent.Listener.Addr().(*net.TCPAddr).Port

	srv, _ := servertest.New(t, httpxtest.Options{
		Auth: false,
	})
	ctx := srv.Context()
	if _, err := srv.Repo.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
		t.Fatal(err)
	}
	if err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, sys *model.System) error {
		raw, _ := json.Marshal([]any{
			map[string]any{"id": "ag-1", "name": "Gateway", "host": "127.0.0.1", "port": port, "token": "tok", "service_type": "mihomo", "profile_id": "default", "enabled": true, "version": "1.8.60-go"},
		})
		return sys.SetJSON("agents", raw)
	}); err != nil {
		t.Fatal(err)
	}
	if err := srv.Repo.UpdateShared(ctx, func(_ context.Context, sh *model.Shared) error {
		return sh.SetJSON("rule_library", json.RawMessage(`[
			{"id":"lib-direct","name":"Direct","source_type":"content","content":"DOMAIN,exists.example.net","behavior":"classical","format":"text","enabled":true},
			{"id":"lib-proxy","name":"Proxy","source_type":"content","content":"","behavior":"classical","format":"text","enabled":true},
			{"id":"lib-url","name":"Remote","source_type":"url","url":"https://rules.example/list.txt","behavior":"domain","format":"text","enabled":true}
		]`))
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
		if err := p.SetJSON("proxy_groups", json.RawMessage(`[
			{"id":"pg-proxy","name":"PROXY","type":"select","manual_nodes":["DIRECT"]},
			{"id":"pg-region","name":"Region","type":"select","manual_nodes":["DIRECT"]}
		]`)); err != nil {
			return err
		}
		if err := p.SetJSON("rule_configs", json.RawMessage(`[
			{"id":"d1","itemType":"ruleset","library_rule_id":"lib-direct","policy":"DIRECT","enabled":true},
			{"id":"p1","itemType":"ruleset","library_rule_id":"lib-proxy","policy":"PROXY","enabled":true},
			{"id":"u1","itemType":"ruleset","library_rule_id":"lib-url","policy":"PROXY","enabled":true}
		]`)); err != nil {
			return err
		}
		return p.SetJSON("domain_discovery", json.RawMessage(`{"direct_ruleset":"d1","proxy_ruleset":"p1","agent_id":"ag-1"}`))
	}); err != nil {
		t.Fatal(err)
	}

	waitJob := func(body any, query string) any {
		t.Helper()
		m, ok := body.(map[string]any)
		if !ok {
			return body
		}
		job, ok := m["job"].(map[string]any)
		if !ok {
			return body
		}
		id, _ := job["id"].(string)
		for end := time.Now().Add(10 * time.Second); time.Now().Before(end); {
			r := srv.Get("/api/domain-discovery/probe/" + id + query)
			var v any
			_ = json.Unmarshal(r.Body, &v)
			if jm, ok := v.(map[string]any)["job"].(map[string]any); ok && jm["status"] != "running" {
				return v
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatal("discovery job timed out")
		return nil
	}

	type req struct{ name, method, path, body string }
	reqs := []req{
		{"apply-array", "POST", "apply", `[1]`},
		{"probe-array", "POST", "probe", `[1]`},
		{"note-array", "PUT", "website/note", `[1]`},
		{"identify-array", "POST", "website/identify", `[1]`},
		{"get-settings", "GET", "settings", ""},
		{"get-agents", "GET", "agents", ""},
		{"get-domains", "GET", "domains", ""},
		{"get-domains-failing", "GET", "domains?view=failing", ""},
		{"get-domains-ignored", "GET", "domains?view=ignored", ""},
		{"get-region", "GET", "region", ""},
		{"get-history", "GET", "history", ""},
		{"get-probe-missing", "GET", "probe/missing", ""},
		{"put-settings-array", "PUT", "settings", `[]`},
		{"put-settings-url-ruleset", "PUT", "settings", `{"proxy_ruleset":"u1"}`},
		{"put-settings-bad-autoprobe", "PUT", "settings", `{"auto_probe":1}`},
		{"put-settings-bad-port", "PUT", "settings", `{"region_probe_port":80}`},
		{"put-settings-valid", "PUT", "settings", `{"region_probe_port":18003,"region_check_enabled":true,"auto_apply_min_confidence":95}`},
		{"ignore-bad", "POST", "ignore", `{"values":["bad host"]}`},
		{"ignore-add", "POST", "ignore", `{"values":["EXAMPLE.DEV","example.dev"]}`},
		{"ignore-list", "GET", "domains?view=ignored", ""},
		{"ignore-del-empty", "DELETE", "ignore", `{"values":[]}`},
		{"ignore-del", "DELETE", "ignore", `{"values":["example.dev"]}`},
		{"note-bad-type", "PUT", "website/note", `{"domain":"example.com","note":3}`},
		{"note-set", "PUT", "website/note", `{"domain":"Example.Com.","note":"  synthetic note  "}`},
		{"note-clear", "PUT", "website/note", `{"domain":"example.com","note":""}`},
		{"probe-empty", "POST", "probe", `{"domains":[]}`},
		{"probe-example", "POST", "probe", `{"domains":["example.com"]}`},
		{"apply-null-type", "POST", "apply", `{"items":[{"value":"null-type.example.net","target":"direct","rule_type":null}]}`},
		{"apply-empty", "POST", "apply", `{"items":[]}`},
		{"apply-ip", "POST", "apply", `{"items":[{"value":"192.0.2.1","target":"direct"}]}`},
		{"apply-valid", "POST", "apply", `{"items":[{"value":"EXAMPLE.COM","target":"proxy"},{"value":"www.example.com","rule_type":"DOMAIN","target":"proxy"},{"value":"exists.example.net","target":"direct"},{"value":"new.example.net","rule_type":"DOMAIN","target":"direct"}]}`},
		{"history-after-apply", "GET", "history", ""},
		{"undo-bad", "POST", "undo", `{"value":"bad host","target":"proxy"}`},
		{"undo-ok", "POST", "undo", `{"value":"example.com","target":"proxy"}`},
		{"undo-missing", "POST", "undo", `{"value":"missing.example.net","target":"proxy"}`},
		{"region-check-bad", "POST", "region/check", `{"services":[]}`},
		{"region-check-domain", "POST", "region/check", `{"services":false,"domains":["example.com"]}`},
		{"region-route-ok", "POST", "region/route", `{"kind":"domain","value":"example.com","policy":"Region"}`},
		{"undo-region", "POST", "undo", `{"value":"example.com","target":"policy:Region"}`},
		{"init-bad-policy", "POST", "rulesets/init", `{"proxy_policy":"missing"}`},
		{"init-ok", "POST", "rulesets/init", `{"proxy_policy":"PROXY"}`},
	}

	for _, r := range reqs {
		t.Run(r.name, func(t *testing.T) {
			var opts []httpxtest.Option
			if r.body != "" {
				opts = append(opts, httpxtest.Body([]byte(r.body), "application/json"))
			}
			resp := srv.Do(r.method, "/api/domain-discovery/"+r.path, opts...)
			var parsed any
			if err := json.Unmarshal(resp.Body, &parsed); err != nil {
				parsed = string(resp.Body)
			}
			if resp.Status == 202 {
				parsed = waitJob(parsed, "")
			}
			key, _ := json.Marshal(map[string]string{"case": r.name})
			val, _ := json.Marshal(normalizeDiscGolden(map[string]any{
				"status": resp.Status,
				"body":   parsed,
			}))
			ref.Assert(t, key, val)
		})
	}
}
