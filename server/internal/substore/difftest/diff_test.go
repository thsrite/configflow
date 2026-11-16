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
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"configflow/internal/auth"
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
	_, err = r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set("server_domain", "http://127.0.0.1:5001")
		return nil, nil
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
	value, err := jsonx.Parse(data)
	if err != nil {
		t.Fatalf("login: %s", data)
	}
	return "Bearer " + value.(*jsonx.Object).GetString("token")
}

type request struct {
	name, method, path, body string
	anonymous                bool
	contains                 string // both bodies must contain it
}

func send(t *testing.T, base, token string, r request) (int, []byte) {
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
	return resp.StatusCode, data
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
		ps, pb := send(t, pyBase, pyToken, r)
		gs, gb := send(t, goBase, goToken, r)
		pv, perr := jsonx.Parse(pb)
		gv, gerr := jsonx.Parse(gb)
		same := ps == gs && ((perr == nil && gerr == nil && jsonx.Equal(pv, gv)) || bytes.Equal(pb, gb))
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
	for _, dir := range []string{pyDir, goDir} {
		r, err := repo.Open(dir, repo.Options{})
		if err != nil {
			t.Fatal(err)
		}
		settings, err := r.GetSystemSettings(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: sub_store_mode=%s", filepath.Base(dir), jsonx.Repr(settings.Value("sub_store_mode")))
	}
	pyRepo, _ := repo.Open(pyDir, repo.Options{})
	goRepo, _ := repo.Open(goDir, repo.Options{})
	ps, _ := pyRepo.GetSystemSettings(context.Background())
	gs, _ := goRepo.GetSystemSettings(context.Background())
	for _, key := range []string{"sub_store_mode"} {
		if !jsonx.Equal(ps.Value(key), gs.Value(key)) {
			failed++
			t.Errorf("stored %s python %s go %s", key, jsonx.Repr(ps.Value(key)), jsonx.Repr(gs.Value(key)))
		}
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

// recordingProxy answers every proxied request as a Sub-Store of another
// version, so a response shows which way the request went.
func recordingProxy(t *testing.T) (*httptest.Server, *int32) {
	var seen int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.RequestURI, "http://") {
			atomic.AddInt32(&seen, 1)
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"success","data":{"backend":"Node","version":"2.99.0"}}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &seen
}

// TestDifferentialReviewLoopbackProxy (review C13): with HTTP_PROXY set and no NO_PROXY,
// requests sends the loopback Sub-Store requests through the proxy; both servers must
// report the proxy's answer.
func TestDifferentialReviewLoopbackProxy(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	fake := fakeSubStore(t)
	proxy, seen := recordingProxy(t)
	bundle := t.TempDir()
	os.WriteFile(filepath.Join(bundle, substore.SubStoreAsset), []byte("bundle"), 0o644)
	os.WriteFile(filepath.Join(bundle, "VERSION"), []byte("2.40.0\n"), 0o644)
	port := fake.URL[strings.LastIndex(fake.URL, ":")+1:]
	mode := "/api/settings/dependencies/sub-store/mode"
	runWith(t, map[string]string{"SUB_STORE_BUILTIN_DIR": bundle, "SUB_STORE_BACKEND_API_PORT": port,
		"HTTPS_PROXY": closedProxy(t), "HTTP_PROXY": proxy.URL, "NO_PROXY": "", "no_proxy": "", "SUB_STORE_URL": ""},
		nil, []request{
			{name: "list via proxy", method: "GET", path: "/api/settings/dependencies", contains: `2.99.0`},
			{name: "mode always via proxy", method: "POST", path: mode, body: `{"mode": "always"}`},
			{name: "update via proxy", method: "POST", path: "/api/settings/dependencies/sub-store/update"},
		})
	if atomic.LoadInt32(seen) < 4 {
		t.Errorf("proxy saw %d requests", atomic.LoadInt32(seen))
	}
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
