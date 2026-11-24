// Package difftest is the Flask↔Go differential of C15 through the task
// routes (GET /api/tasks, POST /api/tasks/run, PUT /api/tasks/interval):
// the real Flask app and the Go application run on copies of one data
// directory; both download from the same loopback fake airport and rule
// server (an external Sub-Store address on a closed loopback port, never a
// real host). Statuses and JSON bodies must match (wall-clock times
// masked), and so must the data written: task-status.json,
// subscription_health.json, the node caches, the transport caches, the rule
// caches and the stored intervals.
//
// Go's /api/tasks needs every task slot; the slots of modules not ported
// yet (router_rule_sync, agent, domain_discovery) are empty fakes here,
// which is what Flask lists for the seeded data (no routers, no Agents,
// automatic domain probing off).
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/refresh/difftest/'
package difftest

import (
	"bytes"
	"configflow/internal/repo/repotest"
	"context"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/metrics"
	"configflow/internal/auth"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
)

const (
	username = "diff-admin"
	password = "diff-password"
	secret   = "diff-shared-jwt-secret"
)

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

const proxiesYAML = `proxies:
- {name: HK 01, type: ss, server: hk.example.test, port: 443, cipher: aes-128-gcm, password: p}
- {name: JP 01, type: ss, server: jp.example.test, port: 443, cipher: aes-128-gcm, password: p}
- {name: 广告, type: ss, server: ad.example.test, port: 443, cipher: aes-128-gcm, password: p}
hosts:
  hk.example.test: 203.0.113.10
`

// upstream is the fake airport and rule server.
func upstream(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sub":
			w.Header().Set("subscription-userinfo", "upload=1024; download=2048; total=10737418240; expire=1798732800")
			io.WriteString(w, proxiesYAML)
		case "/empty":
			io.WriteString(w, "proxies: []\n")
		case "/blocked":
			http.Error(w, "forbidden", http.StatusForbidden)
		case "/list.txt":
			io.WriteString(w, "+.new.test\nexample.test\n")
		case "/bad.txt":
			io.WriteString(w, "DOMAIN,bad.test\n")
		case "/down.txt":
			http.Error(w, "down", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func seed(t *testing.T, dir, base string) {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.ServerDomain = "http://127.0.0.1:5001"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.UpdateShared(ctx, func(_ context.Context, shared *model.Shared) error {
		// Keep the default catalogue (referenced by the default profile).
		rawSubs, err := json.Marshal(shared.Subscriptions)
		if err != nil {
			return err
		}
		var subs []any
		if err := json.Unmarshal(rawSubs, &subs); err != nil {
			return err
		}
		subs = append(subs,
			map[string]any{"id": "s-ok", "name": "Feed", "url": base + "/sub", "enabled": true, "interval": 86400,
				"exclude_regex": "广告"},
			map[string]any{"id": "s-empty", "name": "Empty", "url": base + "/empty", "enabled": true},
			map[string]any{"id": "s-blocked", "name": "Blocked", "url": base + "/blocked", "enabled": true, "interval": 3600},
			map[string]any{"id": "s-off", "name": "Off", "url": base + "/sub?off", "enabled": false},
			map[string]any{"id": "s-nourl", "name": "No URL", "url": ""},
			// Review C15: fixed receipts far in the future (never due),
			// with failure counts beyond int64; next_run_at compared unmasked.
			map[string]any{"id": "s-future", "name": "Future", "url": base + "/sub?future", "enabled": true, "interval": 86400},
		)
		mergedSubs, err := json.Marshal(subs)
		if err != nil {
			return err
		}
		if err := shared.SetJSON("subscriptions", mergedSubs); err != nil {
			return err
		}
		rawLib, err := json.Marshal(shared.RuleLibrary)
		if err != nil {
			return err
		}
		var lib []any
		if err := json.Unmarshal(rawLib, &lib); err != nil {
			return err
		}
		lib = append(lib,
			map[string]any{"id": "r-ok", "name": "Good", "source_type": "url", "url": base + "/list.txt", "behavior": "domain", "format": "text"},
			map[string]any{"id": "r-bad", "name": "Bad", "source_type": "url", "url": base + "/bad.txt", "behavior": "domain", "format": "text"},
			map[string]any{"id": "r-down", "name": "Down", "source_type": "url", "url": base + "/down.txt", "behavior": "domain",
				"format": "text", "update_interval": 3600},
			map[string]any{"id": "r-future", "name": "Future", "source_type": "url", "url": base + "/list.txt?future", "behavior": "domain",
				"format": "text", "update_interval": 3600},
			map[string]any{"id": "r-content", "name": "Inline", "source_type": "content", "content": "+.a.test", "behavior": "domain", "format": "text"},
		)
		mergedLib, err := json.Marshal(lib)
		if err != nil {
			return err
		}
		return shared.SetJSON("rule_library", mergedLib)
	})
	if err != nil {
		t.Fatal(err)
	}
	// Nothing is due during the run: Flask's scheduler starts 30 s after
	// the app and must not refresh behind the requests' back.
	now := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	statuses := map[string]any{}
	for _, key := range []string{"subscription:s-ok", "subscription:s-empty", "subscription:s-blocked",
		"rule_source:r-ok", "rule_source:r-bad", "rule_source:r-down"} {
		statuses[key] = map[string]any{"last_run_at": now, "last_status": "success", "run_count": 1}
	}
	// (String counts are left out: taskstatus.Record reads them as 0 where
	// task_status.record int()s them — a C02 finding, see PROGRESS C15.)
	statuses["subscription:s-blocked"] = map[string]any{"last_run_at": "2026-10-31T08:00:00+08:00", "last_status": "failed",
		"consecutive_failures": 2, "run_count": 4, "failure_count": 2}
	statuses["subscription:s-future"] = map[string]any{"last_run_at": "2099-01-01T00:00:00+05:30", "last_status": "failed",
		"consecutive_failures": json.Number("-18446744073709551611"), "run_count": 3, "failure_count": 3}
	statuses["rule_source:r-future"] = map[string]any{"last_run_at": "2099-01-01T00:00:00-03:00", "last_status": "failed",
		"consecutive_failures": json.Number("18446744073709551617"), "run_count": 1, "failure_count": 1}
	data, _ := json.Marshal(statuses)
	os.WriteFile(filepath.Join(dir, "task-status.json"), data, 0o644)
}

func copyDir(t *testing.T, from, to string) {
	if out, err := exec.Command("cp", "-a", from+"/.", to).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
}

func startPython(t *testing.T, dataDir string, env []string) string {
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, _ := filepath.Abs("../../../..")
	port := freePort(t)
	script := "import runpy\nimport backend.utils.scheduled_refresh as scheduled\nscheduled.start_loop = lambda: None\nrunpy.run_module('backend.app', run_name='__main__')"
	cmd := exec.Command(python, "-c", script)
	cmd.Dir = root
	repotest.PythonLayout(t, dataDir) // Python reads the version-1 layout (O17)
	cmd.Env = append(append(os.Environ(), env...), "DATA_DIR="+dataDir, "LOG_FILE="+filepath.Join(dataDir, "app.log"),
		"CONFIGFLOW_BACKEND_HOST=127.0.0.1", "CONFIGFLOW_BACKEND_PORT="+strconv.Itoa(port),
		"ADMIN_USERNAME="+username, "ADMIN_PASSWORD="+password, "JWT_SECRET_KEY="+secret, "LOG_LEVEL=INFO",
		"PYTHONDONTWRITEBYTECODE=1")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cmd.Process.Kill()
		cmd.Wait()
		if t.Failed() {
			t.Logf("python output:\n%s", output.String())
		}
	})
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/api/auth/status")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return base
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Flask did not start:\n%s", output.String())
	return ""
}

func none(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return nil, nil }

func startGo(t *testing.T, dataDir string) string {
	t.Setenv("DATA_DIR", dataDir)
	root, err := logx.Setup(logx.Config{Level: logx.Info, File: filepath.Join(dataDir, "app.log")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	previous := auth.Configure(auth.Config{AdminUsername: username, AdminPassword: password, SecretKey: secret})
	t.Cleanup(func() { auth.Configure(previous) })
	r, err := repo.OpenDefault(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	repo.SetRepository(r)
	t.Cleanup(func() { repo.SetRepository(nil) })

	for _, slot := range []string{tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains} {
		t.Cleanup(tasks.SetRowBuilder(slot, none))
	}
	app := server.NewApp()
	// Flask's metrics flush loop started with the app (its row shows the
	// next save); the Go loops are not started here.
	started := time.Now()
	metrics.SetStartedAt(&started)
	t.Cleanup(func() { metrics.SetStartedAt(nil) })
	if !app.Router.HasEndpoint("tasks.get_tasks") {
		t.Fatalf("task routes not registered; missing %v", tasks.Missing())
	}
	if _, err := repo.PrepareConfig(repo.WithScope(context.Background())); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)
	return srv.URL
}

func decodeJSONValue(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func login(t *testing.T, base string) string {
	resp, err := http.Post(base+"/api/auth/login", "application/json",
		strings.NewReader(`{"username": "`+username+`", "password": "`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("login: %s", data)
	}
	return "Bearer " + body.Token
}

type request struct {
	name, method, path, body string
	wait                     string // task key to wait for after the request
}

func send(t *testing.T, base, token string, r request) (int, any, []byte) {
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req, _ := http.NewRequest(r.method, base+r.path, body)
	if r.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Authorization", token)
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	value, _ := decodeJSONValue(data)
	return resp.StatusCode, value, data
}

// wait polls GET /api/tasks until the task's row is not running.
func wait(t *testing.T, base, token, key string) {
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		_, value, _ := send(t, base, token, request{name: "wait", method: "GET", path: "/api/tasks"})
		running := true
		if object, ok := value.(map[string]any); ok {
			if list, ok := object["tasks"].([]any); ok {
				for _, row := range list {
					if m, ok := row.(map[string]any); ok && m["key"] == key {
						running = m["running"] == true
					}
				}
			}
		}
		if !running {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("%s still running", key)
}

// timeKeys hold wall-clock times or durations: masked to "<set>" when
// present (null and missing stay visible).
var timeKeys = map[string]bool{"last_run_at": true, "last_finished_at": true, "last_success_at": true,
	"next_run_at": true, "cache_updated_at": true, "last_duration_ms": true, "at": true, "traffic_at": true,
	"updated_at": true, "fetched_at": true}

// templateSubscriptionKey is the task of the template subscription a new
// data directory starts with; its address is the placeholder "订阅链接".
const templateSubscriptionKey = "subscription:sub_1760688109177"

// placeholderTemplateRow applies the deliberate batch-36 change to Python's
// task list (behaviour-changes.md): a placeholder subscription address is
// not configured, so its row is neither scheduled nor runnable and carries
// the invalid-address hint (taken from Go's row after checking it names
// http(s)).
func placeholderTemplateRow(t *testing.T, python, native any) any {
	t.Helper()
	rowOf := func(list any) map[string]any {
		if root, ok := list.(map[string]any); ok {
			if rows, ok := root["tasks"].([]any); ok {
				for _, row := range rows {
					if m, ok := row.(map[string]any); ok && m["key"] == templateSubscriptionKey {
						return m
					}
				}
			}
		}
		return nil
	}
	py, gor := rowOf(python), rowOf(native)
	if py == nil || gor == nil {
		return python
	}
	hint, _ := gor["run_hint"].(string)
	if !strings.Contains(hint, "http(s)") {
		t.Errorf("template subscription run_hint %q", hint)
	}
	for key, value := range map[string]any{"can_run": false, "editable": false, "enabled": false,
		"notifications_enabled": false, "next_run_at": nil, "run_hint": hint} {
		py[key] = value
	}
	return python
}

func mask(value any, parent string) any {
	switch v := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			switch {
			case timeKeys[key] && item != nil:
				out[key] = "<set>"
			case key == "id" && parent == "nodes":
				out[key] = "<node id>"
			default:
				out[key] = mask(item, key)
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = mask(item, parent)
		}
		return out
	}
	return value
}

func readJSON(t *testing.T, path string) any {
	data, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	value, err := decodeJSONValue(data)
	if err != nil {
		return "<invalid> " + string(data)
	}
	return value
}

// retiredRuleLockGlob matches only the by-source SHA-256 cache locks retired
// in O3-6a, not arbitrary files or locks elsewhere in the data directory.
var retiredRuleLockGlob = filepath.Join("shared", "rules", "by-source", strings.Repeat("[0-9a-f]", 64)+".list.lock")

// files lists the data written below dir that the module owns, excluding
// Python's retiredRuleLockGlob resource locks.
func files(t *testing.T, dir string, cacheNames map[string]string) map[string]any {
	out := map[string]any{"task-status.json": mask(readJSON(t, filepath.Join(dir, "task-status.json")), "")}
	for _, pattern := range []string{"shared/subscription_health.json", "shared/subscribes/*.json",
		"shared/subscription-downloads/*.json"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		for _, path := range matches {
			rel, _ := filepath.Rel(dir, path)
			out[rel] = mask(readJSON(t, path), "")
		}
	}
	filepath.WalkDir(filepath.Join(dir, "shared/rules"), func(path string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			rel, _ := filepath.Rel(dir, path)
			if retired, _ := filepath.Match(retiredRuleLockGlob, rel); retired {
				return nil
			}
			data, _ := os.ReadFile(path)
			if key, known := cacheNames[rel]; known {
				rel = key
			}
			out[rel] = string(data)
		}
		return nil
	})
	r, err := repo.Open(dir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	shared, err := r.Shared(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out["shared.subscriptions"] = shared.Subscriptions
	out["shared.rule_library"] = shared.RuleLibrary
	return out
}

func TestDifferentialTaskRoutes(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	up := upstream(t)
	closed := "http://127.0.0.1:" + strconv.Itoa(freePort(t))
	env := map[string]string{"SUB_STORE_BUILTIN_DIR": "/nonexistent", "SUB_STORE_URL": closed,
		"HTTPS_PROXY": "", "HTTP_PROXY": "", "https_proxy": "", "http_proxy": "", "NO_PROXY": "127.0.0.1,localhost"}
	var pyEnv []string
	for k, v := range env {
		t.Setenv(k, v)
		pyEnv = append(pyEnv, k+"="+v)
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	seed(t, seedDir, up.URL)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase := startPython(t, pyDir, pyEnv)
	goBase := startGo(t, goDir)
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	run := func(key string) request {
		return request{name: "run " + key, method: "POST", path: "/api/tasks/run", body: `{"key": "` + key + `"}`, wait: key}
	}
	interval := func(name, body string) request {
		return request{name: "interval " + name, method: "PUT", path: "/api/tasks/interval", body: body}
	}
	requests := []request{
		{name: "list initial", method: "GET", path: "/api/tasks"},
		run("subscription:s-ok"),
		run("subscription:s-empty"),
		run("subscription:s-blocked"),
		run("rule_source:r-ok"),
		run("rule_source:r-bad"),
		run("rule_source:r-down"),
		{name: "list after runs", method: "GET", path: "/api/tasks"},
		run("rule_source:r-ok"),
		run("subscription:s-ok"),
		{name: "run content source", method: "POST", path: "/api/tasks/run", body: `{"key": "rule_source:r-content"}`},
		{name: "run without url", method: "POST", path: "/api/tasks/run", body: `{"key": "subscription:s-nourl"}`},
		{name: "run gone", method: "POST", path: "/api/tasks/run", body: `{"key": "subscription:gone"}`},
		{name: "run empty id", method: "POST", path: "/api/tasks/run", body: `{"key": "rule_source:"}`},
		{name: "run other category", method: "POST", path: "/api/tasks/run", body: `{"key": "agent_rule_sync:a"}`},
		{name: "run no key", method: "POST", path: "/api/tasks/run", body: `{}`},
		{name: "run key not string", method: "POST", path: "/api/tasks/run", body: `{"key": 5}`},
		{name: "run broken json", method: "POST", path: "/api/tasks/run", body: `{"key"`},
		interval("subscription 3600", `{"key": "subscription:s-ok", "interval_seconds": 3600}`),
		interval("rule off", `{"key": "rule_source:r-ok", "interval_seconds": 0}`),
		interval("rule 604800", `{"key": "rule_source:r-bad", "interval_seconds": 604800}`),
		interval("too small", `{"key": "subscription:s-ok", "interval_seconds": 30}`),
		interval("float", `{"key": "subscription:s-ok", "interval_seconds": 60.0}`),
		interval("bool", `{"key": "subscription:s-ok", "interval_seconds": true}`),
		interval("string", `{"key": "subscription:s-ok", "interval_seconds": "60"}`),
		interval("huge", `{"key": "subscription:s-ok", "interval_seconds": 100000000000000000000}`),
		interval("extra key", `{"key": "subscription:s-ok", "interval_seconds": 60, "x": 1}`),
		interval("gone", `{"key": "rule_source:gone", "interval_seconds": 60}`),
		interval("router", `{"key": "router_rule_sync:p:r", "interval_seconds": 60}`),
		interval("content source", `{"key": "rule_source:r-content", "interval_seconds": 60}`),
		{name: "list final", method: "GET", path: "/api/tasks"},
	}
	failed := 0
	for _, r := range requests {
		ps, pv, pb := send(t, pyBase, pyToken, r)
		gs, gv, gb := send(t, goBase, goToken, r)
		if r.wait != "" && ps == 202 {
			wait(t, pyBase, pyToken, r.wait)
		}
		if r.wait != "" && gs == 202 {
			wait(t, goBase, goToken, r.wait)
		}
		pv = placeholderTemplateRow(t, pv, gv)
		same := ps == gs && ((pv != nil && gv != nil && testjson.Equal(mask(pv, ""), mask(gv, ""))) || bytes.Equal(pb, gb))
		if !same {
			failed++
			pm, _ := json.Marshal(mask(pv, ""))
			gm, _ := json.Marshal(mask(gv, ""))
			diff := testjson.Diff(mask(pv, ""), mask(gv, ""))
			t.Errorf("DIFF %s %s %s (%s):\n  python %d %s\n  go     %d %s", r.name, r.method, r.path, diff, ps, pm, gs, gm)
		} else {
			t.Logf("same %-28s %s %s → %d", r.name, r.method, r.path, ps)
		}
	}
	// Review C15: the fixed future receipts give exact next_run_at values.
	_, pyList, _ := send(t, pyBase, pyToken, request{name: "list fixed", method: "GET", path: "/api/tasks"})
	_, goList, _ := send(t, goBase, goToken, request{name: "list fixed", method: "GET", path: "/api/tasks"})
	nextRun := func(list any, key string) any {
		if root, ok := list.(map[string]any); ok {
			if rows, ok := root["tasks"].([]any); ok {
				for _, row := range rows {
					if m, ok := row.(map[string]any); ok && m["key"] == key {
						return m["next_run_at"]
					}
				}
			}
		}
		return "<missing>"
	}
	for key, want := range map[string]string{"subscription:s-future": "2099-01-01T00:05:00+05:30",
		"rule_source:r-future": "2099-01-01T01:00:00-03:00"} {
		p, g := nextRun(pyList, key), nextRun(goList, key)
		if p != want || g != want {
			failed++
			t.Errorf("DIFF next_run_at %s: python %v, go %v, want %s", key, p, g, want)
		}
	}
	locks, err := filepath.Glob(filepath.Join(goDir, retiredRuleLockGlob))
	if err != nil {
		t.Fatal(err)
	}
	if len(locks) != 0 {
		t.Errorf("Go created retired rule cache locks: %v", locks)
	}
	pyNames, goNames := referenceCacheNames(t, up.URL+"/list.txt")
	pyFiles, goFiles := files(t, pyDir, pyNames), files(t, goDir, goNames)
	var names []string
	for name := range pyFiles {
		names = append(names, name)
	}
	for name := range goFiles {
		if _, ok := pyFiles[name]; !ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	for _, name := range names {
		p, g := pyFiles[name], goFiles[name]
		if !testjson.Equal(p, g) {
			failed++
			pt, _ := json.Marshal(p)
			gt, _ := json.Marshal(g)
			t.Errorf("DIFF file %s:\n  python %s\n  go     %s", name, pt, gt)
		} else {
			t.Logf("same file %s", name)
		}
	}
	t.Logf("%d requests, %d files, %d differences", len(requests), len(names), failed)
}
