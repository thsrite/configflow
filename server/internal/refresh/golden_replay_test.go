package refresh_test

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/model"
	"configflow/internal/repo"
	appserver "configflow/internal/server"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testgolden"
)

const refProxiesYAML = `proxies:
- {name: HK 01, type: ss, server: hk.example.test, port: 443, cipher: aes-128-gcm, password: p}
- {name: JP 01, type: ss, server: jp.example.test, port: 443, cipher: aes-128-gcm, password: p}
- {name: 广告, type: ss, server: ad.example.test, port: 443, cipher: aes-128-gcm, password: p}
hosts:
  hk.example.test: 203.0.113.10
`

var refTimeKeys = map[string]bool{
	"last_run_at": true, "last_finished_at": true, "last_success_at": true,
	"next_run_at": true, "cache_updated_at": true, "last_duration_ms": true,
	"at": true, "traffic_at": true, "updated_at": true, "fetched_at": true,
}

var refLocalPortRe = regexp.MustCompile(`http://127\.0\.0\.1:\d+`)

func normalizeRefGolden(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			if refTimeKeys[k] && val != nil {
				out[k] = "<set>"
				continue
			}
			out[k] = normalizeRefGolden(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = normalizeRefGolden(item)
		}
		return out
	case string:
		return refLocalPortRe.ReplaceAllString(x, "http://127.0.0.1:<port>")
	}
	return v
}

func TestRefreshTasksGoldenReplay(t *testing.T) {
	ref := testgolden.New(t, testgolden.Spec{Compress: true})

	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sub":
			w.Header().Set("subscription-userinfo", "upload=1024; download=2048; total=10737418240; expire=1798732800")
			_, _ = io.WriteString(w, refProxiesYAML)
		case "/empty":
			_, _ = io.WriteString(w, "proxies: []\n")
		case "/blocked":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/list.txt":
			_, _ = io.WriteString(w, "+.new.test\nexample.test\n")
		case "/bad.txt":
			_, _ = io.WriteString(w, "DOMAIN,bad.test\n")
		case "/down.txt":
			http.Error(w, "down", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(up.Close)

	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := "http://127.0.0.1:" + strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	_ = l.Close()

	dataDir := t.TempDir()
	t.Setenv("DATA_DIR", dataDir)
	t.Setenv("SUB_STORE_BUILTIN_DIR", "/nonexistent")
	t.Setenv("SUB_STORE_URL", closed)
	t.Setenv("NO_PROXY", "127.0.0.1,localhost")

	prev := auth.Configure(auth.Config{AdminUsername: "diff-admin", AdminPassword: "diff-password", SecretKey: "refresh-secret"})
	t.Cleanup(func() { auth.Configure(prev) })

	r, err := repo.OpenDefaultWithOptions(dataDir, appserver.RepoOptions())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Close)

	ctx := context.Background()
	if err := r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		origSubs, _ := json.Marshal(shared.Subscriptions)
		var subsList []any
		_ = json.Unmarshal(origSubs, &subsList)
		subsList = append(subsList,
			map[string]any{"id": "s-ok", "name": "OK Feed", "url": up.URL + "/sub", "enabled": true, "interval": 3600},
			map[string]any{"id": "s-empty", "name": "Empty Feed", "url": up.URL + "/empty", "enabled": true, "interval": 3600},
			map[string]any{"id": "s-blocked", "name": "Blocked Feed", "url": up.URL + "/blocked", "enabled": true, "interval": 3600},
			map[string]any{"id": "s-nourl", "name": "No URL", "url": "", "enabled": true, "interval": 3600},
		)
		subsRaw, _ := json.Marshal(subsList)
		if err := shared.SetJSON("subscriptions", subsRaw); err != nil {
			return err
		}
		origRules, _ := json.Marshal(shared.RuleLibrary)
		var rulesList []any
		_ = json.Unmarshal(origRules, &rulesList)
		rulesList = append(rulesList,
			map[string]any{"id": "r-ok", "name": "OK Rules", "source_type": "url", "url": up.URL + "/list.txt", "behavior": "domain", "format": "text", "enabled": true, "update_interval": 3600},
			map[string]any{"id": "r-bad", "name": "Bad Rules", "source_type": "url", "url": up.URL + "/bad.txt", "behavior": "domain", "format": "text", "enabled": true, "update_interval": 3600},
			map[string]any{"id": "r-down", "name": "Down Rules", "source_type": "url", "url": up.URL + "/down.txt", "behavior": "domain", "format": "text", "enabled": true, "update_interval": 3600},
			map[string]any{"id": "r-content", "name": "Inline Rules", "source_type": "content", "content": "+.inline.test", "behavior": "domain", "format": "text", "enabled": true},
		)
		rulesRaw, _ := json.Marshal(rulesList)
		return shared.SetJSON("rule_library", rulesRaw)
	}); err != nil {
		t.Fatal(err)
	}

	full, err := appserver.New(ctx, appserver.Options{Repo: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	app := full.HTTP
	t.Cleanup(func() { full.Runtime.Stop(5 * time.Second) })
	none := func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }
	for _, slot := range []string{tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains} {
		full.Tasks.SetRowBuilder(slot, none)
	}
	started := time.Now()
	full.Metrics.SetStartedAt(&started)

	loginReq := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"diff-admin","password":"diff-password"}`))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRec := httptest.NewRecorder()
	app.ServeHTTP(loginRec, loginReq)
	var loginResp struct {
		Token string `json:"token"`
	}
	_ = json.Unmarshal(loginRec.Body.Bytes(), &loginResp)
	authHeader := "Bearer " + loginResp.Token

	doReq := func(method, path, body string) (int, any) {
		httpReq := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			httpReq.Header.Set("Content-Type", "application/json")
		}
		httpReq.Header.Set("Authorization", authHeader)
		rec := httptest.NewRecorder()
		app.ServeHTTP(rec, httpReq)
		var parsed any
		if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
			parsed = rec.Body.String()
		}
		return rec.Code, parsed
	}

	waitDone := func(taskKey string) {
		t.Helper()
		for end := time.Now().Add(30 * time.Second); time.Now().Before(end); {
			_, val := doReq("GET", "/api/tasks", "")
			running := false
			if m, ok := val.(map[string]any); ok {
				if list, ok := m["tasks"].([]any); ok {
					for _, row := range list {
						if rm, ok := row.(map[string]any); ok && rm["key"] == taskKey && rm["running"] == true {
							running = true
						}
					}
				}
			}
			if !running {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("task %s still running", taskKey)
	}

	type tc struct {
		name, method, path, body, wait string
	}
	run := func(key string) tc {
		return tc{name: "run-" + key, method: "POST", path: "/api/tasks/run", body: `{"key":"` + key + `"}`, wait: key}
	}
	cases := []tc{
		{"list-initial", "GET", "/api/tasks", "", ""},
		run("subscription:s-ok"),
		run("subscription:s-empty"),
		run("subscription:s-blocked"),
		run("rule_source:r-ok"),
		run("rule_source:r-bad"),
		run("rule_source:r-down"),
		{"list-after-runs", "GET", "/api/tasks", "", ""},
		{"run-content-source", "POST", "/api/tasks/run", `{"key":"rule_source:r-content"}`, ""},
		{"run-no-url", "POST", "/api/tasks/run", `{"key":"subscription:s-nourl"}`, ""},
		{"run-gone", "POST", "/api/tasks/run", `{"key":"subscription:gone"}`, ""},
		{"run-empty-id", "POST", "/api/tasks/run", `{"key":"rule_source:"}`, ""},
		{"run-other-category", "POST", "/api/tasks/run", `{"key":"agent_rule_sync:a"}`, ""},
		{"run-no-key", "POST", "/api/tasks/run", `{}`, ""},
		{"run-key-not-string", "POST", "/api/tasks/run", `{"key":5}`, ""},
		{"run-broken-json", "POST", "/api/tasks/run", `{"key"`, ""},
		{"interval-sub", "PUT", "/api/tasks/interval", `{"key":"subscription:s-ok","interval_seconds":600}`, ""},
		{"interval-rule", "PUT", "/api/tasks/interval", `{"key":"rule_source:r-ok","interval_seconds":900}`, ""},
		{"interval-too-low", "PUT", "/api/tasks/interval", `{"key":"subscription:s-ok","interval_seconds":10}`, ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, parsed := doReq(c.method, c.path, c.body)
			if c.wait != "" {
				waitDone(c.wait)
			}
			key, _ := json.Marshal(map[string]string{"case": c.name})
			val, _ := json.Marshal(normalizeRefGolden(map[string]any{
				"status": status,
				"body":   parsed,
			}))
			ref.Assert(t, key, val)
		})
	}
}
