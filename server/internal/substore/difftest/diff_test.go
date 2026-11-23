// Package difftest is the Flask↔Go differential of the C13 routes
// (GET /api/settings/dependencies, POST …/sub-store/mode, POST
// …/sub-store/update): the real Flask app and the Go application run on
// copies of one data directory, both pointed at the same loopback fake
// Sub-Store (bundled mode: SUB_STORE_BUILTIN_DIR with a bundle and
// SUB_STORE_BACKEND_API_PORT; external mode: SUB_STORE_URL). GitHub is
// never contacted: HTTPS_PROXY points at a closed loopback port, so both
// see a ProxyError. Statuses and JSON bodies must match, and the
// sub_store_mode written to configflow.sqlite3.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/substore/difftest/'
package difftest

import (
	"bytes"
	"configflow/internal/repo/repotest"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/substore"
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

func seed(t *testing.T, dir string) {
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
	cmd := exec.Command(python, "-m", "backend.app")
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
	// A fresh service reads the environment of this run.
	t.Cleanup(substore.SetDefault(substore.NewService(substore.NewEnv())))
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
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatalf("login: %s", data)
	}
	return "Bearer " + body.Token
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

type request struct {
	name, method, path, body string
	anonymous                bool
	contains                 string    // both bodies must contain it
	currentVersions          [2]string // explicit native loopback outcomes, Python then Go
	before                   func()
	observe                  func(*testing.T, string)
}

func send(t *testing.T, base, token string, r request) (int, []byte, http.Header) {
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req, _ := http.NewRequest(r.method, base+r.path, body)
	if r.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if !r.anonymous {
		req.Header.Set("Authorization", token)
	}
	resp, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, data, resp.Header
}

// fakeSubStore answers the version endpoint like a running Sub-Store.
func fakeSubStore(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/utils/env" {
			w.Header().Set("Content-Type", "application/json")
			io.WriteString(w, `{"status":"success","data":{"backend":"Node","version":"2.40.0"}}`)
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func run(t *testing.T, env map[string]string, requests []request) { runWith(t, env, nil, requests) }

// runWith is run with prepare(pyDir, goDir) called after seeding, before the
// servers start.
func runWith(t *testing.T, env map[string]string, prepare func(pyDir, goDir string), requests []request) {
	for k, v := range env {
		t.Setenv(k, v)
	}
	var pyEnv []string
	for k, v := range env {
		pyEnv = append(pyEnv, k+"="+v)
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	seed(t, seedDir)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	if prepare != nil {
		prepare(pyDir, goDir)
	}
	pyBase := startPython(t, pyDir, pyEnv)
	goBase := startGo(t, goDir)
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	failed := 0
	for _, r := range requests {
		if r.before != nil {
			r.before()
		}
		ps, pb, ph := send(t, pyBase, pyToken, r)
		if r.observe != nil {
			r.observe(t, "python")
		}
		if r.before != nil {
			r.before()
		}
		gs, gb, gh := send(t, goBase, goToken, r)
		if r.observe != nil {
			r.observe(t, "go")
		}
		pv, perr := decodeJSONValue(pb)
		gv, gerr := decodeJSONValue(gb)
		expected := pv
		if r.currentVersions != ([2]string{}) {
			if ps != 200 || gs != 200 || perr != nil || gerr != nil {
				t.Fatalf("%s: native version boundary did not return JSON 200", r.name)
			}
			// Only the named version leaf differs. Assert both actual values
			// before constructing the complete expected Go response.
			for side, body := range []any{pv, gv} {
				root, ok := body.(map[string]any)
				if !ok {
					t.Fatal("dependency response is not an object")
				}
				dependencies, ok := root["dependencies"].([]any)
				if !ok || len(dependencies) != 1 {
					t.Fatal("dependency list changed")
				}
				dependency, ok := dependencies[0].(map[string]any)
				if !ok || dependency["current_version"] != r.currentVersions[side] {
					t.Fatalf("%s: side %d unexpected current_version", r.name, side)
				}
			}
			if ph.Get("Content-Type") != "application/json" || gh.Get("Content-Type") != "application/json" {
				t.Fatal("native dependency content type changed")
			}
			t.Logf("native %s: current_version Python=%s Go=%s; remaining response strict", r.name, r.currentVersions[0], r.currentVersions[1])
			var cloned any
			cloned, _ = decodeJSONValue(pb)
			cloned.(map[string]any)["dependencies"].([]any)[0].(map[string]any)["current_version"] = r.currentVersions[1]
			expected = cloned
		}
		migrated := httpxtest.MigratedAPIError(r.method, ps, gs, ph, gh, pb, gb)
		same := migrated || ps == gs && ((perr == nil && gerr == nil && testjson.Equal(expected, gv)) || bytes.Equal(pb, gb))
		if r.contains != "" && (!strings.Contains(string(pb), r.contains) || !strings.Contains(string(gb), r.contains)) {
			failed++
			t.Errorf("%s: body lacks %q:\n  python %s\n  go     %s", r.name, r.contains, pb, gb)
		}
		if !same {
			failed++
			t.Errorf("DIFF %s %s %s:\n  python %d %s\n  go     %d %s", r.name, r.method, r.path, ps, pb, gs, gb)
		} else {
			t.Logf("same %-34s %s %s → %d", r.name, r.method, r.path, ps)
		}
	}
	// The data written: sub_store_mode in the stored system settings.
	readMode := func(dir string) json.RawMessage {
		r, err := repo.Open(dir, repo.Options{})
		if err != nil {
			t.Fatal(err)
		}
		system, err := r.System(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		raw, ok, err := model.EncodeMember(&system.SystemConfig, "sub_store_mode")
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			return json.RawMessage("null")
		}
		return raw
	}
	pm, gm := readMode(pyDir), readMode(goDir)
	t.Logf("%s: sub_store_mode=%s", filepath.Base(pyDir), pm)
	t.Logf("%s: sub_store_mode=%s", filepath.Base(goDir), gm)
	if !testjson.Equal(pm, gm) {
		failed++
		t.Errorf("stored sub_store_mode python %s go %s", pm, gm)
	}
	t.Logf("%d requests, %d differences", len(requests), failed)
}

func closedProxy(t *testing.T) string { return "http://127.0.0.1:" + strconv.Itoa(freePort(t)) }

func TestDifferentialBundledSubStore(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	fake := fakeSubStore(t)
	bundle := t.TempDir()
	os.WriteFile(filepath.Join(bundle, substore.SubStoreAsset), []byte("bundle"), 0o644)
	os.WriteFile(filepath.Join(bundle, "VERSION"), []byte("2.40.0\n"), 0o644)
	port := fake.URL[strings.LastIndex(fake.URL, ":")+1:]
	proxy := closedProxy(t)
	mode := "/api/settings/dependencies/sub-store/mode"
	run(t, map[string]string{"SUB_STORE_BUILTIN_DIR": bundle, "SUB_STORE_BACKEND_API_PORT": port,
		"HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "NO_PROXY": "127.0.0.1,localhost", "SUB_STORE_URL": ""},
		[]request{
			{name: "list anonymous", method: "GET", path: "/api/settings/dependencies", anonymous: true},
			{name: "list", method: "GET", path: "/api/settings/dependencies", contains: "exception_type=ProxyError"},
			{name: "list refresh", method: "GET", path: "/api/settings/dependencies?refresh=true"},
			{name: "mode no body", method: "POST", path: mode},
			{name: "mode list body", method: "POST", path: mode, body: `[]`},
			{name: "mode bad value", method: "POST", path: mode, body: `{"mode": "stop"}`},
			{name: "mode list value", method: "POST", path: mode, body: `{"mode": ["always"]}`},
			{name: "mode broken json", method: "POST", path: mode, body: `{"mode"`},
			{name: "mode anonymous", method: "POST", path: mode, body: `{"mode": "always"}`, anonymous: true},
			{name: "mode always", method: "POST", path: mode, body: `{"mode": "always"}`},
			{name: "list after always", method: "GET", path: "/api/settings/dependencies"},
			{name: "mode on_demand", method: "POST", path: mode, body: `{"mode": "on_demand"}`},
			{name: "mode always again", method: "POST", path: mode, body: `{"mode": "always"}`},
			{name: "update (GitHub unreachable)", method: "POST", path: "/api/settings/dependencies/sub-store/update"},
			{name: "update anonymous", method: "POST", path: "/api/settings/dependencies/sub-store/update", anonymous: true},
			{name: "update GET 405", method: "GET", path: "/api/settings/dependencies/sub-store/update"},
		})
}

func TestDifferentialExternalSubStore(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	fake := fakeSubStore(t)
	proxy := closedProxy(t)
	mode := "/api/settings/dependencies/sub-store/mode"
	run(t, map[string]string{"SUB_STORE_BUILTIN_DIR": "/nonexistent", "SUB_STORE_URL": fake.URL + "/",
		"HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "NO_PROXY": "127.0.0.1,localhost"},
		[]request{
			{name: "list", method: "GET", path: "/api/settings/dependencies"},
			{name: "mode external", method: "POST", path: mode, body: `{"mode": "always"}`},
			{name: "mode bad", method: "POST", path: mode, body: `{}`},
			{name: "update without bundle", method: "POST", path: "/api/settings/dependencies/sub-store/update"},
		})
}

// runtimeRoot is SubStoreRuntime.root for a data directory (tempfile.gettempdir() is /tmp
// on the test server unless TMPDIR is set).
func runtimeRoot(dataDir string) string {
	tmp := os.Getenv("TMPDIR")
	if tmp == "" {
		tmp = "/tmp"
	}
	abs, _ := filepath.Abs(dataDir)
	sum := sha256.Sum256([]byte(abs))
	return filepath.Join(tmp, "configflow-sub-store-"+hex.EncodeToString(sum[:])[:12])
}

// requestLog records every HTTP request, including its authority and credentials.
type requestLog struct {
	mu      sync.Mutex
	entries []string
	// python marks entries sent by Python's requests (its default
	// User-Agent); Go's Sub-Store client always sends "ConfigFlow".
	python []bool
}

func (l *requestLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, r.Method+" "+r.RequestURI+" host="+r.Host+" proxy-auth="+r.Header.Get("Proxy-Authorization")+" auth="+r.Header.Get("Authorization"))
	l.python = append(l.python, strings.HasPrefix(r.Header.Get("User-Agent"), "python-requests/"))
}
func (l *requestLog) take() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := l.entries
	l.entries, l.python = nil, nil
	return entries
}

// takeWithoutPython is take without the requests Python's requests sent: a
// background request of the Python server can arrive late, while the Go side
// is observed (seen once in a loaded gate run).
func (l *requestLog) takeWithoutPython(t *testing.T) []string {
	t.Helper()
	l.mu.Lock()
	defer l.mu.Unlock()
	entries := []string{}
	for i, entry := range l.entries {
		if l.python[i] {
			t.Logf("ignored a late Python request: %s", entry)
			continue
		}
		entries = append(entries, entry)
	}
	l.entries, l.python = nil, nil
	return entries
}

// recordingProxy returns a distinct version so accidental proxying is observable.
func recordingProxy(t *testing.T) (*httptest.Server, *requestLog) {
	log := &requestLog{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.record(r)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"success","data":{"backend":"Node","version":"2.99.0"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, log
}

// TestDifferentialReviewLoopbackProxy asserts the 2026-11-17 native N2
// decision independently: requests proxies loopback, Go goes direct.
func TestDifferentialReviewLoopbackProxy(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	direct := &requestLog{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		direct.record(r)
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"success","data":{"backend":"Node","version":"2.40.0"}}`)
	}))
	t.Cleanup(fake.Close)
	proxy, seen := recordingProxy(t)
	bundle := t.TempDir()
	os.WriteFile(filepath.Join(bundle, substore.SubStoreAsset), []byte("bundle"), 0o644)
	os.WriteFile(filepath.Join(bundle, "VERSION"), []byte("2.40.0\n"), 0o644)
	port := fake.URL[strings.LastIndex(fake.URL, ":")+1:]
	targetHost := strings.TrimPrefix(fake.URL, "http://")
	observe := func(want int) func(*testing.T, string) {
		return func(t *testing.T, side string) {
			t.Helper()
			var proxied, origin []string
			if side == "go" {
				proxied, origin = seen.takeWithoutPython(t), direct.takeWithoutPython(t)
			} else {
				proxied, origin = seen.take(), direct.take()
			}
			wantProxy, wantDirect := []string{}, []string{}
			if want != 0 {
				if side == "python" {
					wantProxy = []string{"GET " + fake.URL + "/api/utils/env host=" + targetHost + " proxy-auth= auth="}
				} else {
					wantDirect = []string{"GET /api/utils/env host=" + targetHost + " proxy-auth= auth="}
				}
			}
			if strings.Join(proxied, "\n") != strings.Join(wantProxy, "\n") || strings.Join(origin, "\n") != strings.Join(wantDirect, "\n") {
				t.Fatalf("%s: proxy=%q want=%q direct=%q want=%q", side, proxied, wantProxy, origin, wantDirect)
			}
		}
	}
	reset := func() { seen.take(); direct.take() }
	runWith(t, map[string]string{"SUB_STORE_BUILTIN_DIR": bundle, "SUB_STORE_BACKEND_API_PORT": port, "HTTPS_PROXY": closedProxy(t), "HTTP_PROXY": proxy.URL, "http_proxy": proxy.URL, "NO_PROXY": "", "no_proxy": "", "ALL_PROXY": "", "all_proxy": "", "REQUEST_METHOD": "", "SUB_STORE_URL": ""}, nil, []request{
		{name: "list native loopback", method: "GET", path: "/api/settings/dependencies", currentVersions: [2]string{"2.99.0", "2.40.0"}, before: reset, observe: observe(1)},
		{name: "mode always native loopback", method: "POST", path: "/api/settings/dependencies/sub-store/mode", body: `{"mode":"always"}`, before: reset, observe: observe(1)},
		{name: "update native loopback", method: "POST", path: "/api/settings/dependencies/sub-store/update", before: reset, observe: observe(0)},
	})
}

// TestDifferentialExplicitRemoteProxy compares real requests and Go with an
// explicit proxy for a synthetic non-loopback URL. No origin DNS is contacted.
func TestDifferentialExplicitRemoteProxy(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	proxy, seen := recordingProxy(t)
	target := "http://upstream.example:8080/subscription?token=synthetic"
	proxyURL := strings.Replace(proxy.URL, "://", "://review:synthetic-password@", 1)
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		python = filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python")
	}
	const script = `import json, requests, sys
r=requests.Session().get(sys.argv[1],proxies={'http':sys.argv[2],'https':sys.argv[2]},headers={'Authorization':'Bearer synthetic-token'},timeout=5)
print(json.dumps({'status':r.status_code,'type':r.headers['Content-Type'],'body':r.json()}))`
	cmd := exec.Command(python, "-I", "-c", script, target, proxyURL)
	cmd.Env = append(os.Environ(), "HTTP_PROXY=", "http_proxy=", "HTTPS_PROXY=", "https_proxy=", "ALL_PROXY=", "all_proxy=", "NO_PROXY=*", "no_proxy=*", "REQUEST_METHOD=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("real requests explicit proxy failed: %v %s", err, output)
	}
	pyTrace := seen.take()
	response, err := (substore.RequestsClient{}).Do(context.Background(), &substore.Request{Method: "GET", URL: target, Proxy: proxyURL, Timeout: substore.Seconds(5), Header: http.Header{"Authorization": {"Bearer synthetic-token"}}})
	if err != nil {
		t.Fatal(err)
	}
	goTrace := seen.take()
	expectedTrace := "GET " + target + " host=upstream.example:8080 proxy-auth=Basic cmV2aWV3OnN5bnRoZXRpYy1wYXNzd29yZA== auth=Bearer synthetic-token"
	if len(pyTrace) != 1 || pyTrace[0] != expectedTrace || len(goTrace) != 1 || goTrace[0] != expectedTrace {
		t.Fatalf("explicit proxy traces: python=%q Go=%q", pyTrace, goTrace)
	}
	py, err := testjson.Parse(output)
	if err != nil {
		t.Fatal(err)
	}
	body, err := testjson.Parse(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	actual := map[string]any{"status": response.StatusCode, "type": response.Header.Get("Content-Type"), "body": body}
	if !testjson.Equal(py, actual) {
		t.Fatalf("explicit proxy response differs: python=%s Go=%s", output, response.Body)
	}
	t.Log("explicit non-loopback proxy: status, content type, complete JSON and authenticated request trace match")
}

// TestDifferentialReviewMalformedState (review C13): a state.json that is not an object
// makes Python fail (AttributeError in status, TypeError in _write): 500 on both, the
// mode saved before the failing write on both.
func TestDifferentialReviewMalformedState(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	fake := fakeSubStore(t)
	bundle := t.TempDir()
	os.WriteFile(filepath.Join(bundle, substore.SubStoreAsset), []byte("bundle"), 0o644)
	os.WriteFile(filepath.Join(bundle, "VERSION"), []byte("2.40.0\n"), 0o644)
	port := fake.URL[strings.LastIndex(fake.URL, ":")+1:]
	proxy := closedProxy(t)
	mode := "/api/settings/dependencies/sub-store/mode"
	for _, state := range []string{`[1]`, `0`, `{"state": "running", "last_used": "x"}`} {
		t.Run(state, func(t *testing.T) {
			runWith(t, map[string]string{"SUB_STORE_BUILTIN_DIR": bundle, "SUB_STORE_BACKEND_API_PORT": port,
				"HTTPS_PROXY": proxy, "HTTP_PROXY": proxy, "NO_PROXY": "127.0.0.1,localhost", "SUB_STORE_URL": ""},
				func(pyDir, goDir string) {
					for _, dir := range []string{pyDir, goDir} {
						root := runtimeRoot(dir)
						os.MkdirAll(root, 0o700)
						os.WriteFile(filepath.Join(root, "state.json"), []byte(state), 0o644)
						t.Cleanup(func() { os.RemoveAll(root) })
					}
				}, []request{
					{name: "list", method: "GET", path: "/api/settings/dependencies"},
					{name: "mode on_demand", method: "POST", path: mode, body: `{"mode": "on_demand"}`},
					{name: "mode always", method: "POST", path: mode, body: `{"mode": "always"}`},
					{name: "list again", method: "GET", path: "/api/settings/dependencies"},
				})
		})
	}
}
