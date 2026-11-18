// Package difftest is the Flask↔Go differential of C16's routes
// (/api/nodes/latency, /api/nodes/latency/settings,
// /api/nodes/latency/dns/check): the real Flask app and the Go application
// run on copies of one data directory and call the same fake node-probe
// worker (a shell script named by CONFIGFLOW_NODE_PROBE_BIN whose stdout,
// exit code and delay the test sets before each request; it records its
// stdin). Sub-Store is a closed loopback port, so URI nodes fail conversion
// on both sides. Statuses, JSON bodies (times masked), the worker's stdin
// and the stored data (shared/node_latency.json, system_config) must match.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/health/difftest/'
package difftest

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"configflow/internal/auth"
	"configflow/internal/health"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/substore"
)

const (
	username = "diff-admin"
	password = "diff-password"
	secret   = "diff-shared-jwt-secret"
)

const probeScript = `#!/bin/sh
cat > "$FAKE_PROBE_DIR/input.json"
if [ -f "$FAKE_PROBE_DIR/sleep" ]; then sleep "$(cat "$FAKE_PROBE_DIR/sleep")"; fi
cat "$FAKE_PROBE_DIR/stdout"
echo "worker stderr with https://user:secret@dns.example" >&2
exit "$(cat "$FAKE_PROBE_DIR/code")"
`

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func seed(t *testing.T, dir string) {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := repo.WithRepository(context.Background(), r)
	_, err = r.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("nodes", append(shared.GetList("nodes"),
			jsonx.Obj("id", "a", "name", "A", "type", "trojan", "server", "example.invalid", "port", jsonx.Int(443),
				"password", "test-only", "tls", true),
			jsonx.Obj("id", "j", "name", "香港 J", "proxy_string", `{"type":"http","server":"local.invalid","port":8080}`),
			jsonx.Obj("id", "u", "name", "URI", "proxy_string", "ss://YWVzLTEyOC1nY206cA@uri.invalid:8388#x"),
			jsonx.Obj("id", "off", "name", "Off", "type", "http", "server", "o.invalid", "port", jsonx.Int(1), "enabled", false),
		))
		shared.Set("subscriptions", append(shared.GetList("subscriptions"),
			jsonx.Obj("id", "s1", "name", "Feed", "url", "http://127.0.0.1:9/sub", "enabled", true, "regex_filter", "^HK")))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = substore.SaveSubscriptionNodes(ctx, "s1", []any{
		jsonx.Obj("name", "HK 01", "type", "ss", "server", "hk.invalid", "port", jsonx.Int(443),
			"params", jsonx.Obj("cipher", "aes-128-gcm", "password", "p")),
		jsonx.Obj("name", "广告", "type", "ss", "server", "ad.invalid", "port", jsonx.Int(443)),
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Previous results: one of the current method (history continues),
	// one of an old method (hidden), one of a deleted node (dropped).
	_, err = r.WriteSharedJSON(ctx, health.LatencyFile, jsonx.Obj(
		`["node","a"]`, jsonx.Obj("latency", jsonx.Int(7), "history", []any{jsonx.Int(5), jsonx.Int(7)}, "method", health.Method,
			"url", "https://www.gstatic.com/generate_204", "unified_delay", true, "dns_servers", []any{}, "dns_bootstrap", []any{}),
		`["node","j"]`, jsonx.Obj("latency", jsonx.Int(9), "history", []any{jsonx.Int(9)}, "method", "proxy_http_v1"),
		`["node","gone"]`, jsonx.Obj("latency", jsonx.Int(3), "history", []any{jsonx.Int(3)}, "method", health.Method)))
	if err != nil {
		t.Fatal(err)
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
	cmd := exec.Command(python, "-m", "backend.app")
	cmd.Dir = root
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
	t.Cleanup(substore.SetDefault(substore.NewService(substore.NewEnv())))
	t.Cleanup(health.SetEnv(health.DefaultEnv()))
	app := server.NewApp()
	if _, err := repo.LoadConfig(repo.WithScope(context.Background())); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)
	return srv.URL
}

func login(t *testing.T, base string) string {
	resp, err := http.Post(base+"/api/auth/login", "application/json",
		strings.NewReader(`{"username": "`+username+`", "password": "`+password+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatalf("login: %s", data)
	}
	return "Bearer " + value.(*jsonx.Object).GetString("token")
}

// worker is what the fake worker does for one request.
type worker struct {
	stdout     string
	code       int
	sleep      string
	executable *bool
}

type request struct {
	name, method, path, body string
	worker                   worker
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
	value, _ := jsonx.Parse(data)
	return resp.StatusCode, value, data
}

func mask(value any) any {
	switch v := value.(type) {
	case *jsonx.Object:
		out := jsonx.NewObject()
		for key, item := range v.All() {
			if key == "tested_at" && item != nil {
				out.Set(key, "<set>")
			} else {
				out.Set(key, mask(item))
			}
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = mask(item)
		}
		return out
	}
	return value
}

func sameWorkerStdin(pyInput, goInput string) bool {
	if pyInput == goInput {
		return true
	}
	if !utf8.ValidString(goInput) {
		return false
	}
	pyVal, errPy := jsonx.Parse([]byte(pyInput))
	goVal, errGo := jsonx.Parse([]byte(goInput))
	if errPy != nil || errGo != nil {
		return false
	}
	pyCompact, errPy := jsonx.Compact(pyVal)
	goCompact, errGo := jsonx.Compact(goVal)
	return errPy == nil && errGo == nil && bytes.Equal(pyCompact, goCompact)
}

func readFile(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "<missing>"
	}
	return string(data)
}

func stored(t *testing.T, dir string) map[string]string {
	latency := readFile(filepath.Join(dir, "shared", health.LatencyFile))
	if value, err := jsonx.Parse([]byte(latency)); err == nil {
		data, _ := jsonx.Indent2(mask(value))
		latency = string(data)
	}
	r, err := repo.Open(dir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	system, err := r.GetSystem(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	settings := system.GetObject("system_config")
	dns, _ := jsonx.Compact(jsonx.Obj("node_test_dns", settings.Value("node_test_dns"),
		"node_test_dns_config", settings.Value("node_test_dns_config")))
	return map[string]string{"node_latency.json (masked)": latency, "system_config dns": string(dns)}
}

func results(entries ...string) string {
	var items []string
	for _, e := range entries {
		items = append(items, e)
	}
	return `{"version": 4, "results": [` + strings.Join(items, ", ") + `]}`
}

func TestDifferentialLatencyRoutes(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	fake := t.TempDir()
	script := filepath.Join(fake, "configflow-node-probe")
	os.WriteFile(script, []byte(probeScript), 0o755)
	closed := "http://127.0.0.1:" + strconv.Itoa(freePort(t))
	env := map[string]string{"SUB_STORE_BUILTIN_DIR": "/nonexistent", "SUB_STORE_URL": closed,
		"CONFIGFLOW_NODE_PROBE_BIN": script, "FAKE_PROBE_DIR": fake, "CONFIGFLOW_NODE_TEST_TIMEOUT_MS": "500",
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
	seed(t, seedDir)
	for _, dir := range []string{pyDir, goDir} {
		if out, err := exec.Command("cp", "-a", seedDir+"/.", dir).CombinedOutput(); err != nil {
			t.Fatalf("cp: %v %s", err, out)
		}
	}
	pyBase := startPython(t, pyDir, pyEnv)
	goBase := startGo(t, goDir)
	pyToken, goToken := login(t, pyBase), login(t, goBase)

	a, j, s := `"[\"node\",\"a\"]"`, `"[\"node\",\"j\"]"`, `"[\"subscription\",\"s1\",\"HK 01\"]"`
	all := results(`{"name": `+a+`, "latency": 120, "first_latency": 300}`, `{"name": `+j+`, "latency": null, "error_code": "timeout", "error": "连接或请求超时"}`,
		`{"name": `+s+`, "latency": 88, "extra": {"k": [1, 2.50]}, "url": "worker-url", "name2": "中"}`)
	onlyA := results(`{"name": ` + a + `, "latency": 99}`)
	check := func(result string) string { return `{"version": 4, "dns_check": ` + result + `}` }
	no, yes := false, true
	post := func(name, path, body string, w worker) request {
		return request{name: name, method: "POST", path: path, body: body, worker: w}
	}
	L, S, C := "/api/nodes/latency", "/api/nodes/latency/settings", "/api/nodes/latency/dns/check"
	var many []string
	for i := 0; i < 21; i++ {
		many = append(many, `"`+strconv.Itoa(i)+`"`)
	}
	requests := []request{
		{name: "results initial", method: "GET", path: L},
		{name: "settings initial", method: "GET", path: S},
		post("all candidates", L, `{}`, worker{stdout: all}),
		post("names A", L, `{"names": ["A", "A", "missing"]}`, worker{stdout: onlyA}),
		post("keys with missing", L, `{"keys": ["[\"node\",\"a\"]", "[\"node\",\"off\"]", "[\"subscription\",\"s1\",\"广告\"]"]}`, worker{stdout: onlyA}),
		post("uri only (conversion fails)", L, `{"keys": ["[\"node\",\"u\"]"]}`, worker{stdout: onlyA}),
		post("old worker", L, `{"names": ["A"]}`, worker{stdout: `{"version": 3, "results": []}`}),
		post("worker dns exit 3", L, `{"names": ["A"]}`, worker{code: 3}),
		post("worker exit 1", L, `{"names": ["A"]}`, worker{stdout: onlyA, code: 1}),
		post("worker garbage", L, `{"names": ["A"]}`, worker{stdout: "not json"}),
		post("worker wrong names", L, `{"names": ["A"]}`, worker{stdout: results(`{"name": "A", "latency": 1}`)}),
		post("worker bool latency", L, `{"names": ["A"]}`, worker{stdout: results(`{"name": ` + a + `, "latency": true}`)}),
		post("worker invalid utf-8", L, `{"names": ["A"]}`, worker{stdout: "\xff\xfe"}),
		post("worker timeout", L, `{"names": ["A"]}`, worker{stdout: onlyA, sleep: "7"}),
		post("missing worker", L, `{"names": ["A"]}`, worker{stdout: onlyA, executable: &no}),
		post("names not list", L, `{"names": "A"}`, worker{executable: &yes}),
		post("keys not strings", L, `{"keys": [1]}`, worker{}),
		post("names and keys", L, `{"names": [], "keys": []}`, worker{}),
		post("too many names", L, `{"names": [`+strings.Join(many, ",")+`]}`, worker{}),
		post("list body", L, `[]`, worker{}),
		post("broken json runs all", L, `{"names"`, worker{stdout: all}),
		{name: "results after", method: "GET", path: L},
		post("settings dns_servers", S, `{"dns_servers": [" 192.0.2.53 ", "192.0.2.53:53", "127.0.0.1:5353"]}`, worker{}),
		post("settings invalid", S, `{"dns_servers": ["https://user:secret@dns.example/q"]}`, worker{}),
		post("settings too many", S, `{"dns_servers": ["192.0.2.1","192.0.2.2","192.0.2.3","192.0.2.4","192.0.2.5"]}`, worker{}),
		post("settings managed", S, `{"dns": {"mode": "managed"}}`, worker{}),
		post("settings none", S, `{}`, worker{}),
		post("settings not object", S, `"x"`, worker{}),
		post("settings custom bootstrap", S, `{"dns": {"mode": "custom", "servers": ["https://DNS.EXAMPLE", "tls://192.0.2.9:853"], "bootstrap": ["192.0.2.53:53"]}}`, worker{}),
		post("settings custom no bootstrap", S, `{"dns": {"mode": "custom", "servers": ["tls://dns.example"]}}`, worker{}),
		{name: "settings after", method: "GET", path: S},
		post("history reset by dns", L, `{"names": ["A"]}`, worker{stdout: onlyA}),
		post("check success", C, `{"dns": {"mode": "custom", "servers": ["tls://dns.example"], "bootstrap": ["192.0.2.53"]}}`,
			worker{stdout: check(`{"success": true, "addresses": ["192.0.2.1", "2001:DB8::1", "192.0.2.1", "::ffff:1.2.3.4"], "latency_ms": 12}`)}),
		post("check saved (dns null)", C, `{"dns": null}`, worker{stdout: check(`{"success": false, "addresses": [], "error_code": "tls", "error": "raw secret"}`)}),
		post("check unknown code", C, `{"dns": {"mode": "system"}}`, worker{stdout: check(`{"success": false, "addresses": [], "error_code": "weird"}`)}),
		post("check fake ip", C, `{"dns": {"mode": "system"}}`, worker{stdout: check(`{"success": true, "addresses": ["198.18.0.5"]}`)}),
		post("check bool latency", C, `{"dns": {"mode": "system"}}`, worker{stdout: check(`{"success": true, "addresses": ["192.0.2.1"], "latency_ms": true}`)}),
		post("check old worker", C, `{"dns": {"mode": "system"}}`, worker{stdout: `{"version": 3}`}),
		post("check exit 1", C, `{"dns": {"mode": "system"}}`, worker{stdout: check(`{}`), code: 1}),
		post("check invalid dns", C, `{"dns": {"mode": "custom", "servers": []}}`, worker{}),
		post("check no dns", C, `{}`, worker{}),
		post("check missing worker", C, `{"dns": {"mode": "system"}}`, worker{executable: &no}),
		post("settings back to system", S, `{"dns_servers": []}`, worker{executable: &yes}),
		{name: "results final", method: "GET", path: L},
	}
	failed := 0
	for _, r := range requests {
		inputs := [2]string{}
		var outcomes [2]struct {
			status int
			value  any
			body   []byte
		}
		for side, target := range []struct{ base, token string }{{pyBase, pyToken}, {goBase, goToken}} {
			os.WriteFile(filepath.Join(fake, "stdout"), []byte(r.worker.stdout), 0o644)
			os.WriteFile(filepath.Join(fake, "code"), []byte(strconv.Itoa(r.worker.code)), 0o644)
			os.Remove(filepath.Join(fake, "sleep"))
			os.Remove(filepath.Join(fake, "input.json"))
			if r.worker.sleep != "" {
				os.WriteFile(filepath.Join(fake, "sleep"), []byte(r.worker.sleep), 0o644)
			}
			if r.worker.executable != nil {
				mode := os.FileMode(0o644)
				if *r.worker.executable {
					mode = 0o755
				}
				os.Chmod(script, mode)
			}
			status, value, body := send(t, target.base, target.token, r)
			outcomes[side].status, outcomes[side].value, outcomes[side].body = status, value, body
			inputs[side] = readFile(filepath.Join(fake, "input.json"))
		}
		p, g := outcomes[0], outcomes[1]
		same := p.status == g.status && ((p.value != nil && g.value != nil && jsonx.Equal(mask(p.value), mask(g.value))) || bytes.Equal(p.body, g.body))
		if !same {
			failed++
			pm, _ := jsonx.Compact(mask(p.value))
			gm, _ := jsonx.Compact(mask(g.value))
			t.Errorf("DIFF %s %s %s:\n  python %d %s\n  go     %d %s", r.name, r.method, r.path, p.status, pm, g.status, gm)
		} else if !sameWorkerStdin(inputs[0], inputs[1]) {
			failed++
			t.Errorf("DIFF worker stdin %s:\n  python %s\n  go     %s", r.name, inputs[0], inputs[1])
		} else {
			t.Logf("same %-30s %s %s → %d", r.name, r.method, r.path, p.status)
		}
		if strings.Contains(string(g.body), "secret") {
			t.Errorf("%s: Go response leaks the worker's stderr", r.name)
		}
	}
	pyFiles, goFiles := stored(t, pyDir), stored(t, goDir)
	for name, p := range pyFiles {
		if p != goFiles[name] {
			failed++
			t.Errorf("DIFF file %s:\n  python %s\n  go     %s", name, p, goFiles[name])
		} else {
			t.Logf("same file %s", name)
		}
	}
	t.Logf("%d requests, %d files, %d differences", len(requests), len(pyFiles), failed)
}
