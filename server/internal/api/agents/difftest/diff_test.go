// Package difftest is the Flask↔Go differential check of the registry
// routes of package #20 (registration, heartbeat, manual creation, edit and
// delete of one Agent), built on the pattern of internal/httpx/difftest: both
// applications run on copies of one data directory, on loopback only, and the
// same requests are replayed against both. Values that are random per server
// (Agent IDs and tokens) are captured from each side's own answers and filled
// into later requests; the resulting Agent records are compared at the end.
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/api/agents/difftest/'
package difftest

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/server"
)

const (
	username   = "diff-admin"
	password   = "diff-password"
	secret     = "diff-shared-jwt-secret"
	agentToken = "diff-agent-token-0123456789"
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
	if _, err := r.CreateProfile(ctx, jsonx.Obj("id", "other", "name", "Other"), ""); err != nil {
		t.Fatal(err)
	}
	_, err = r.UpdateSystemTransaction(ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set("server_domain", "http://127.0.0.1:5001")
		system.Set("agents", []any{jsonx.Obj("id", "agent-1", "name", "diff agent", "token", agentToken,
			"profile_id", "default", "service_type", "mihomo", "host", "127.0.0.1", "port", 9, "version", "1.0.0",
			"config_version", "0", "enabled", true),
			// An unfinished deployment: the profile binding cannot change (409).
			jsonx.Obj("id", "agent-busy", "name", "busy agent", "token", agentToken+"-busy",
				"profile_id", "default", "service_type", "mihomo", "host", "10.9.0.2", "port", 9, "version", "1.0.0",
				"config_version", "0", "enabled", true,
				"deployments", jsonx.Obj("binding-request", jsonx.Obj("deployment_id", "binding-request", "status", "preparing",
					"profile_id", "default", "service_type", "mihomo"))),
			// An update waiting for confirmation: no edit, no delete (409).
			jsonx.Obj("id", "agent-upgrading", "name", "upgrading agent", "token", agentToken+"-upgrading",
				"profile_id", "default", "service_type", "mihomo", "host", "127.0.0.3", "port", 9, "version", "1.0.0",
				"config_version", "0", "enabled", true, "latest_upgrade", jsonx.Obj("status", "downloading")),
		})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	seedMetrics(t, dir)
}

// naiveISO is datetime.isoformat() of a local naive time.
func naiveISO(at time.Time) string {
	text := at.Format("2006-01-02T15:04:05")
	if micro := at.Nanosecond() / 1000; micro != 0 {
		text += fmt.Sprintf(".%06d", micro)
	}
	return text
}

// seedMetrics writes saved monitoring history (A04) for the service start
// to load: agent-1's JSON Lines file (an expired point, two recent ones in
// json.dumps spacing and a line cut short), agent-busy's old whole-JSON
// file to migrate, a deleted Agent's file and a compaction leftover to
// remove.
func seedMetrics(t *testing.T, dir string) {
	metricsDir := filepath.Join(dir, "metrics")
	if err := os.MkdirAll(metricsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	point := func(at time.Time, cpu, sent int) string {
		return fmt.Sprintf(`{"timestamp": "%s", "cpu": {"usage_percent": %d.5, "core_count": 4}, "memory": {"used_percent": 40}, "disk": {}, "network": {"bytes_sent": %d, "bytes_recv": 20, "speed_sent": %d, "speed_recv": 2.50}}`,
			naiveISO(at), cpu, sent, sent/10)
	}
	files := map[string]string{
		"agent-1_metrics.jsonl": point(now.Add(-30*time.Hour), 9, 900) + "\n" + point(now.Add(-3*time.Minute), 10, 1000) + "\n" +
			point(now.Add(-2*time.Minute).Add(123456*time.Microsecond), 20, 3000) + "\n" + `{"timestamp": "2026-10-2`,
		"agent-busy_metrics.json": `{"agent_id": "agent-busy", "metrics": [` + point(now.Add(-time.Minute), 5, 50) + `, "skipped", ` +
			point(now.Add(-26*time.Hour), 6, 60) + `]}`,
		"gone_metrics.jsonl":   point(now.Add(-time.Minute), 1, 10) + "\n",
		".agent-1abcdefgh.tmp": "partial",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(metricsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func copyDir(t *testing.T, from, to string) {
	if out, err := exec.Command("cp", "-a", from+"/.", to).CombinedOutput(); err != nil {
		t.Fatalf("cp: %v %s", err, out)
	}
}

func startPython(t *testing.T, dataDir string) (string, func()) {
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, _ := filepath.Abs("../../../../..")
	port := freePort(t)
	cmd := exec.Command(python, "-m", "backend.app")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "DATA_DIR="+dataDir, "LOG_FILE="+filepath.Join(dataDir, "app.log"),
		"CONFIGFLOW_BACKEND_HOST=127.0.0.1", "CONFIGFLOW_BACKEND_PORT="+strconv.Itoa(port),
		"ADMIN_USERNAME="+username, "ADMIN_PASSWORD="+password, "JWT_SECRET_KEY="+secret, "LOG_LEVEL=INFO",
		"SUB_STORE_BUILTIN_DIR=/nonexistent", "PYTHONDONTWRITEBYTECODE=1", "AGENT_REGISTRATION_KEY=")
	var output bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	stopped := false
	t.Cleanup(func() {
		if !stopped {
			cmd.Process.Kill()
			cmd.Wait()
		}
		if t.Failed() {
			t.Logf("python output:\n%s", output.String())
		}
	})
	// stop is docker stop: SIGTERM, then wait for the final save.
	stop := func() {
		stopped = true
		cmd.Process.Signal(syscall.SIGTERM)
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(60 * time.Second):
			cmd.Process.Kill()
			<-done
			t.Errorf("Flask did not stop on SIGTERM")
		}
	}
	base := "http://127.0.0.1:" + strconv.Itoa(port)
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(base + "/api/auth/status")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				return base, stop
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("Flask did not start:\n%s", output.String())
	return "", nil
}

func startGo(t *testing.T, dataDir string) (string, func()) {
	t.Setenv("DATA_DIR", dataDir)
	t.Setenv("AGENT_REGISTRATION_KEY", "")
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
	app := server.NewApp()
	if _, err := repo.LoadConfig(repo.WithScope(context.Background())); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)
	// main starts the loops once listening and, on SIGTERM, stops the HTTP
	// server before lifecycle.Stop runs the shutdown hooks.
	lifecycle.Start(context.Background())
	return srv.URL, func() {
		srv.Close()
		lifecycle.Stop(30 * time.Second)
	}
}

type request struct {
	name        string
	method      string
	path        string
	admin       bool
	headers     map[string]string
	body        string
	contentType string
	mask        []string
	// capture stores top-level (or dotted) answer fields under a name for
	// "{name}" placeholders in later requests of the same side.
	capture map[string]string
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func fill(text string, values map[string]string) string {
	for name, value := range values {
		text = strings.ReplaceAll(text, "{"+name+"}", value)
	}
	return text
}

func send(t *testing.T, base string, r request, token string, values map[string]string) response {
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(fill(r.body, values))
	}
	req, err := http.NewRequest(r.method, base+fill(r.path, values), body)
	if err != nil {
		t.Fatal(err)
	}
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	for k, v := range r.headers {
		req.Header.Set(k, fill(v, values))
	}
	if r.admin {
		req.Header.Set("Authorization", token)
	}
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Do(req)
	if err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return response{resp.StatusCode, resp.Header, data}
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

func lookup(value any, path string) any {
	for _, part := range strings.Split(path, ".") {
		object, ok := value.(*jsonx.Object)
		if !ok {
			return nil
		}
		value = object.Value(part)
	}
	return value
}

func maskJSON(value any, mask []string) any {
	for _, path := range mask {
		maskPath(value, strings.Split(path, "."))
	}
	return value
}

// maskPath masks one dotted path; "*" stands for every item of a list.
func maskPath(value any, parts []string) {
	current := value
	for i, part := range parts {
		if list, ok := current.([]any); ok && part == "*" {
			for _, item := range list {
				maskPath(item, parts[i+1:])
			}
			break
		}
		object, ok := current.(*jsonx.Object)
		if !ok {
			break
		}
		if i == len(parts)-1 {
			if object.Has(part) {
				object.Set(part, "<masked>")
			}
			break
		}
		current = object.Value(part)
	}
}

func compare(r request, py, gor response) []string {
	var diffs []string
	if py.status != gor.status {
		diffs = append(diffs, fmt.Sprintf("status python %d go %d", py.status, gor.status))
	}
	for _, name := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Profile"} {
		if a, b := strings.Join(py.header.Values(name), ","), strings.Join(gor.header.Values(name), ","); a != b {
			diffs = append(diffs, fmt.Sprintf("header %s python %q go %q", name, a, b))
		}
	}
	pv, perr := jsonx.Parse(py.body)
	gv, gerr := jsonx.Parse(gor.body)
	if perr == nil && gerr == nil && len(r.mask) > 0 {
		if !jsonx.Equal(maskJSON(pv, r.mask), maskJSON(gv, r.mask)) {
			diffs = append(diffs, fmt.Sprintf("json python %s go %s", py.body, gor.body))
		}
	} else if !bytes.Equal(py.body, gor.body) {
		diffs = append(diffs, fmt.Sprintf("body python %q go %q", py.body, gor.body))
	}
	return diffs
}

func requests() []request {
	J := "application/json"
	agent := `{"name": "edge", "host": "10.0.0.8", "port": 8080, "service_type": "mihomo", "version": "1.2.3"}`
	idToken := []string{"id", "token"}
	metrics := `{"system_metrics": {"cpu": {"usage_percent": 12.5, "core_count": 4}, "memory": {"total": 1024, "used": 512, "available": 512, "used_percent": 50.0}, "collected_at": "2026-08-31T00:00:00Z"}}`
	metrics2 := `{"system_metrics": {"cpu": {"usage_percent": 33, "core_count": 4}, "disk": {"total": 18446744073709551615, "used": 1, "free": 2, "used_percent": 0.1}, "network": {"bytes_sent": 18446744073709551615, "bytes_recv": 7, "speed_sent": 18446744073709551615, "speed_recv": 3}, "collected_at": "2026-08-31T00:00:01Z"}}`
	stamps := []string{"data.history.*.timestamp"}
	return append([]request{
		{name: "register new", method: "POST", path: "/api/agents/register", body: agent, contentType: J, mask: idToken,
			capture: map[string]string{"id": "id", "token": "token"}},
		{name: "register duplicate without token", method: "POST", path: "/api/agents/register", body: agent, contentType: J},
		{name: "register duplicate wrong token", method: "POST", path: "/api/agents/register", body: agent, contentType: J,
			headers: map[string]string{"Authorization": "Bearer wrong"}},
		{name: "register duplicate malformed bearer", method: "POST", path: "/api/agents/register", body: agent, contentType: J,
			headers: map[string]string{"Authorization": "Bearer  {token}"}},
		{name: "re-register with token", method: "POST", path: "/api/agents/register", contentType: J, mask: []string{"id"},
			body:    `{"name": "edge", "host": "10.0.0.8", "port": 9090, "service_type": "mihomo", "version": "1.3.0"}`,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "register without content type", method: "POST", path: "/api/agents/register", mask: idToken,
			body: `{"name": "plain", "host": "10.0.0.9"}`},
		{name: "register bad json", method: "POST", path: "/api/agents/register", body: `{"name":`, contentType: J},
		{name: "register empty body", method: "POST", path: "/api/agents/register", contentType: J},
		{name: "register null", method: "POST", path: "/api/agents/register", body: `null`, contentType: J},
		{name: "register list", method: "POST", path: "/api/agents/register", body: `[1]`, contentType: J},
		{name: "register string", method: "POST", path: "/api/agents/register", body: `"x"`, contentType: J},
		{name: "register numeric host", method: "POST", path: "/api/agents/register", body: `{"name": "n", "host": 5}`, contentType: J},
		{name: "register missing profile", method: "POST", path: "/api/agents/register", body: `{"name": "p", "host": "10.0.0.20", "profile_id": "missing"}`, contentType: J},
		{name: "register bad profile id", method: "POST", path: "/api/agents/register", body: `{"name": "p", "host": "10.0.0.20", "profile_id": "bad id!"}`, contentType: J},
		{name: "register other profile", method: "POST", path: "/api/agents/register", body: `{"name": "p", "host": "10.0.0.20", "profile_id": "other", "service_type": "oxidns", "deployment_method": "docker"}`, contentType: J, mask: idToken},
		{name: "register docker host replaced", method: "POST", path: "/api/agents/register", body: `{"name": "docker", "host": "172.20.0.5"}`, contentType: J, mask: idToken},
		{name: "register forwarded client", method: "POST", path: "/api/agents/register", body: `{"name": "fwd"}`, contentType: J, mask: idToken,
			headers: map[string]string{"X-Forwarded-For": "203.0.113.9, 172.18.0.1"}},
		{name: "heartbeat", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"version": "2.0.0", "service_status": "running", "config_version": "cfg-1"}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat unchanged", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"version": "2.0.0"}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat metrics", method: "POST", path: "/api/agents/{id}/heartbeat", body: metrics, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat wrong token", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer wrong"}},
		{name: "heartbeat no token", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{}`, contentType: J},
		{name: "heartbeat unknown agent", method: "POST", path: "/api/agents/missing/heartbeat", body: `{}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat oversized", method: "POST", path: "/api/agents/{id}/heartbeat", body: strings.Repeat(" ", 32769), contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat bad field", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"version": 2}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat unknown field", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"x": 1}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat bad metrics", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"system_metrics": {"cpu": {"usage_percent": 101}}}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat malformed json", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"version":`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat text/plain", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{}`, contentType: "text/plain",
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat null", method: "POST", path: "/api/agents/{id}/heartbeat", body: `null`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat seeded agent", method: "POST", path: "/api/agents/agent-1/heartbeat", body: `{"service_status": "running"}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer " + agentToken}},
		{name: "heartbeat non-ascii bearer", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"version": "attacker"}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer café"}},
		{name: "heartbeat cjk bearer", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"version": "attacker"}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer 令牌"}},
		{name: "heartbeat bearer trailing field", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"version": "attacker"}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token} extra"}},
		{name: "heartbeat bearer tab", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"version": "attacker"}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer\t{token}"}},
		{name: "heartbeat bad encoding", method: "POST", path: "/api/agents/{id}/heartbeat", body: "\xff", contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat bad encoding in string", method: "POST", path: "/api/agents/{id}/heartbeat", body: "{\"version\": \"\xff\"}", contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat utf-8 bom", method: "POST", path: "/api/agents/{id}/heartbeat", body: "\xef\xbb\xbf{\"version\": \"bom\"}", contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat utf-16", method: "POST", path: "/api/agents/{id}/heartbeat", body: utf16(`{"version": "utf16"}`), contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat nan metric", method: "POST", path: "/api/agents/{id}/heartbeat", body: `{"system_metrics": {"cpu": {"usage_percent": NaN}}}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer {token}"}},
		{name: "heartbeat busy agent", method: "POST", path: "/api/agents/agent-busy/heartbeat", body: `{"version": "1.0.1"}`, contentType: J,
			headers: map[string]string{"Authorization": "Bearer " + agentToken + "-busy"}},
		{name: "heartbeat seeded agent metrics", method: "POST", path: "/api/agents/agent-1/heartbeat", body: metrics2, contentType: J,
			headers: map[string]string{"Authorization": "Bearer " + agentToken}},
		{name: "heartbeat busy agent metrics", method: "POST", path: "/api/agents/agent-busy/heartbeat", body: metrics, contentType: J,
			headers: map[string]string{"Authorization": "Bearer " + agentToken + "-busy"}},
		{name: "metrics latest", method: "GET", path: "/api/agents/agent-1/metrics", admin: true},
		{name: "metrics latest registered", method: "GET", path: "/api/agents/{id}/metrics", admin: true, mask: []string{"data.agent_id"}},
		{name: "metrics latest no heartbeat", method: "GET", path: "/api/agents/agent-upgrading/metrics", admin: true},
		{name: "metrics latest no login", method: "GET", path: "/api/agents/agent-1/metrics"},
		{name: "metrics latest unknown", method: "GET", path: "/api/agents/missing/metrics", admin: true},
		{name: "metrics history", method: "GET", path: "/api/agents/agent-1/metrics/history", admin: true, mask: stamps},
		{name: "metrics history 1h", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=1", admin: true, mask: stamps},
		{name: "metrics history bad hours", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=abc", admin: true, mask: stamps},
		{name: "metrics history arabic digit", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=%D9%A3", admin: true, mask: stamps},
		{name: "metrics history zero", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=0", admin: true, mask: stamps},
		{name: "metrics history negative", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=-5", admin: true, mask: stamps},
		{name: "metrics history too many digits", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=" + strings.Repeat("9", 4301), admin: true, mask: stamps},
		{name: "metrics history date overflow", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=10000000000", admin: true},
		{name: "metrics history days overflow", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=24000000000", admin: true},
		{name: "metrics history c int overflow", method: "GET", path: "/api/agents/agent-1/metrics/history?hours=1000000000000", admin: true},
		{name: "metrics history migrated", method: "GET", path: "/api/agents/agent-busy/metrics/history", admin: true, mask: stamps},
		{name: "metrics history no login", method: "GET", path: "/api/agents/agent-1/metrics/history"},
		{name: "metrics history unknown", method: "GET", path: "/api/agents/missing/metrics/history", admin: true},
		{name: "metrics summary", method: "GET", path: "/api/agents/agent-1/metrics/summary", admin: true},
		{name: "metrics summary 24h", method: "GET", path: "/api/agents/agent-1/metrics/summary?hours=24", admin: true},
		{name: "metrics summary migrated", method: "GET", path: "/api/agents/agent-busy/metrics/summary?hours=24", admin: true},
		{name: "metrics summary empty", method: "GET", path: "/api/agents/agent-upgrading/metrics/summary", admin: true},
		{name: "metrics summary overflow", method: "GET", path: "/api/agents/agent-1/metrics/summary?hours=-10000000000", admin: true},
		{name: "metrics summary no login", method: "GET", path: "/api/agents/agent-1/metrics/summary"},
		{name: "metrics summary unknown", method: "GET", path: "/api/agents/missing/metrics/summary", admin: true},
		{name: "register duplicate non-ascii bearer", method: "POST", path: "/api/agents/register", body: agent, contentType: J,
			headers: map[string]string{"Authorization": "Bearer 令牌🔐"}},
		{name: "register duplicate oversized bearer", method: "POST", path: "/api/agents/register", body: agent, contentType: J,
			headers: map[string]string{"Authorization": "Bearer " + strings.Repeat("x", 4097)}},
		{name: "register bad encoding", method: "POST", path: "/api/agents/register", body: "\xff", contentType: J},
		{name: "register busy agent other profile", method: "POST", path: "/api/agents/register", contentType: J,
			body:    `{"name": "busy agent", "host": "10.9.0.2", "port": 9, "profile_id": "other"}`,
			headers: map[string]string{"Authorization": "Bearer " + agentToken + "-busy"}},
		{name: "profile binding conflict", method: "PUT", path: "/api/agents/agent-busy", admin: true, body: `{"profile_id": "other"}`, contentType: J},
		{name: "busy agent rename", method: "PUT", path: "/api/agents/agent-busy", admin: true, body: `{"name": "renamed busy"}`, contentType: J},
		{name: "upgrading agent edit", method: "PUT", path: "/api/agents/agent-upgrading", admin: true, body: `{"name": "x"}`, contentType: J},
		{name: "upgrading agent delete", method: "DELETE", path: "/api/agents/agent-upgrading", admin: true},
		{name: "manual create no login", method: "POST", path: "/api/agents", body: `{"name": "manual", "host": "10.0.0.40"}`, contentType: J},
		{name: "manual create", method: "POST", path: "/api/agents", admin: true, body: `{"name": "manual", "host": "10.0.0.40"}`, contentType: J,
			mask: []string{"data.id"}, capture: map[string]string{"manual": "data.id"}},
		{name: "manual create duplicate", method: "POST", path: "/api/agents", admin: true, body: `{"name": "manual", "host": "10.0.0.40"}`, contentType: J},
		{name: "manual create list", method: "POST", path: "/api/agents", admin: true, body: `[]`, contentType: J},
		{name: "manual create null", method: "POST", path: "/api/agents", admin: true, body: `null`, contentType: J},
		{name: "manual create text/plain", method: "POST", path: "/api/agents", admin: true, body: `{}`, contentType: "text/plain"},
		{name: "manual create bad json", method: "POST", path: "/api/agents", admin: true, body: `{`, contentType: J},
		{name: "update name", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `{"name": "renamed", "token": "x", "port": 7000}`, contentType: J},
		{name: "update profile", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `{"profile_id": "other"}`, contentType: J},
		{name: "update missing profile", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `{"profile_id": "missing"}`, contentType: J},
		{name: "update bad profile", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `{"profile_id": 5}`, contentType: J},
		{name: "update null", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `null`, contentType: J},
		{name: "update list", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `[]`, contentType: J},
		{name: "update number", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `5`, contentType: J},
		{name: "update string", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `"plain"`, contentType: J},
		{name: "update string with profile_id", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `"xprofile_idx"`, contentType: J},
		{name: "update list with profile_id", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `["profile_id"]`, contentType: J},
		{name: "update bool", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `true`, contentType: J},
		{name: "update text/plain", method: "PUT", path: "/api/agents/{manual}", admin: true, body: `{}`, contentType: "text/plain"},
		{name: "update unknown", method: "PUT", path: "/api/agents/missing", admin: true, body: `{"name": "x"}`, contentType: J},
		{name: "update no login", method: "PUT", path: "/api/agents/{manual}", body: `{"name": "x"}`, contentType: J},
		{name: "delete no login", method: "DELETE", path: "/api/agents/{manual}"},
		{name: "delete", method: "DELETE", path: "/api/agents/{manual}", admin: true},
		{name: "delete again", method: "DELETE", path: "/api/agents/{manual}", admin: true},
		{name: "delete seeded", method: "DELETE", path: "/api/agents/agent-1", admin: true},
	}, reviewRequests()...)
}

// reviewRequests are the cases added by the A03 review: int() forms of the
// Docker bridge check, latin-1 X-Forwarded-For values, metric ints beyond the
// float range (OverflowError) and beyond the int digit limit (ValueError),
// and the order in which the metric checks fail.
func reviewRequests() []request {
	J := "application/json"
	idToken := []string{"id", "token"}
	var out []request
	for i, host := range []string{"172.١٧.0.1", "172.１７.0.1", "172. 17.0.1", "172.1_7.0.1", "172.+17.0.1", "172.017.0.1",
		"172.16.0.1", "172.32.0.1", "172.17.0", "LOCALHOST", "127.x", "localhost", "172.17 .0.1", "172.0x11.0.1"} {
		out = append(out, request{name: "register docker form " + host, method: "POST", path: "/api/agents/register", contentType: J, mask: idToken,
			body: `{"name": "docker-` + strconv.Itoa(i) + `", "host": "` + host + `"}`})
	}
	for i, forwarded := range []string{"\xa0203.0.113.7\xa0, 1.2.3.4", "caf\xc3\xa9, x", "\x85198.51.100.1\x85,y", "\xff"} {
		out = append(out, request{name: "register latin-1 forwarded " + strconv.Itoa(i), method: "POST", path: "/api/agents/register",
			contentType: J, mask: idToken, body: `{"name": "fwd-` + strconv.Itoa(i) + `"}`,
			headers: map[string]string{"X-Forwarded-For": unescapeBytes(forwarded)}})
	}
	maxFloat := "179769313486231570814527423731704356798070567525844996598917476803157260780028538760589558632766878171540458953514382464234321326889464182768467546703537516986049910576551282076245490090389328944075868508455133942304583236903222948165808559332123348274797826204144723168738177180919299881250404026184124858368"
	halfway := "179769313486231575804128197568503885939002350117941411767545627891801114536396644853619288305177042633935372685103635187590438437370702292699562517687521668833979406288629832876259672468103520237920172119362601898937975098263032931492834697134299320496935997324255116936540444370309403987146642102044149678080"
	below := "179769313486231575804128197568503885939002350117941411767545627891801114536396644853619288305177042633935372685103635187590438437370702292699562517687521668833979406288629832876259672468103520237920172119362601898937975098263032931492834697134299320496935997324255116936540444370309403987146642102044149678079"
	for name, metrics := range map[string]string{
		"huge total":           `{"memory": {"total": 1` + strings.Repeat("0", 400) + `}}`,
		"huge percent":         `{"cpu": {"usage_percent": -1` + strings.Repeat("0", 400) + `}}`,
		"max float int":        `{"disk": {"free": ` + maxFloat + `}}`,
		"halfway int":          `{"disk": {"free": ` + halfway + `}}`,
		"below halfway int":    `{"disk": {"free": ` + below + `}}`,
		"2**64":                `{"network": {"bytes_sent": 18446744073709551616}}`,
		"earlier section bad":  `{"network": {"bytes_sent": 1` + strings.Repeat("0", 400) + `}, "cpu": {"usage_percent": 101}}`,
		"later section bad":    `{"cpu": {"usage_percent": 101}, "network": {"bytes_sent": 1` + strings.Repeat("0", 400) + `}}`,
		"bad key after huge":   `{"cpu": {"core_count": 1` + strings.Repeat("0", 400) + `, "bogus": 1}}`,
		"bad value after huge": `{"cpu": {"core_count": 1` + strings.Repeat("0", 400) + `, "usage_percent": true}}`,
		"digit limit":          `{"cpu": {"core_count": 1` + strings.Repeat("0", 5000) + `}}`,
		"digit limit version":  `{"cpu": {}}, "version": 1` + strings.Repeat("0", 4300),
		"4300 digits":          `{"cpu": {"core_count": 1` + strings.Repeat("0", 4299) + `}}`,
	} {
		out = append(out, request{name: "heartbeat metrics " + name, method: "POST", path: "/api/agents/{id}/heartbeat", contentType: J,
			body: `{"system_metrics": ` + metrics + `}`, headers: map[string]string{"Authorization": "Bearer {token}"}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].name < out[j].name })
	return out
}

// unescapeBytes turns \xNN escapes into raw bytes (header values that are
// not UTF-8).
func unescapeBytes(s string) string {
	var out []byte
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && s[i+1] == 'x' {
			value, _ := strconv.ParseUint(s[i+2:i+4], 16, 8)
			out = append(out, byte(value))
			i += 3
			continue
		}
		out = append(out, s[i])
	}
	return string(out)
}

// utf16 is s encoded as UTF-16-LE without a BOM (json.loads detects it).
func utf16(s string) string {
	var out []byte
	for _, r := range s {
		out = append(out, byte(r), byte(r>>8))
	}
	return string(out)
}

// dataFiles lists the files under a data directory, without logs and lock
// files (the Flask process starts every background loop, each with its own
// lock file; those belong to the modules that port the loops).
func dataFiles(t *testing.T, dir string) []string {
	var files []string
	filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		if strings.HasPrefix(filepath.Base(rel), "app.log") || strings.HasSuffix(rel, ".lock") {
			return nil
		}
		files = append(files, rel)
		return nil
	})
	sort.Strings(files)
	return files
}

// finalAgents is the stored Agent list with per-server values masked.
func finalAgents(t *testing.T, dir string) any {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	system, err := r.GetSystem(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	agents := system.GetList("agents")
	for _, item := range agents {
		agent := item.(*jsonx.Object)
		for _, key := range []string{"id", "token", "last_heartbeat", "created_at", "updated_at"} {
			if agent.Has(key) {
				agent.Set(key, "<masked>")
			}
		}
	}
	return agents
}

func TestDifferentialAgainstFlask(t *testing.T) {
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on the test server")
	}
	base := t.TempDir()
	seedDir, pyDir, goDir := filepath.Join(base, "seed"), filepath.Join(base, "python"), filepath.Join(base, "go")
	for _, dir := range []string{seedDir, pyDir, goDir} {
		os.MkdirAll(dir, 0o755)
	}
	seed(t, seedDir)
	copyDir(t, seedDir, pyDir)
	copyDir(t, seedDir, goDir)
	pyBase, stopPython := startPython(t, pyDir)
	goBase, stopGo := startGo(t, goDir)
	pyToken, goToken := login(t, pyBase), login(t, goBase)
	pyValues, goValues := map[string]string{}, map[string]string{}
	failed := 0
	for _, r := range requests() {
		py := send(t, pyBase, r, pyToken, pyValues)
		gor := send(t, goBase, r, goToken, goValues)
		for name, path := range r.capture {
			for _, side := range []struct {
				body   []byte
				values map[string]string
			}{{py.body, pyValues}, {gor.body, goValues}} {
				value, _ := jsonx.Parse(side.body)
				side.values[name] = jsonx.Str(lookup(value, path))
			}
		}
		if diffs := compare(r, py, gor); len(diffs) > 0 {
			failed++
			t.Errorf("DIFF %s %s %s:\n  %s", r.name, r.method, r.path, strings.Join(diffs, "\n  "))
		} else {
			t.Logf("same %-38s %s %s → %d", r.name, r.method, r.path, py.status)
		}
	}
	pyAgents, goAgents := finalAgents(t, pyDir), finalAgents(t, goDir)
	if !jsonx.Equal(pyAgents, goAgents) {
		failed++
		pyText, _ := jsonx.Sorted(pyAgents)
		goText, _ := jsonx.Sorted(goAgents)
		t.Errorf("DIFF stored agents:\n  python %s\n  go     %s", pyText, goText)
	} else {
		keys := func(v any) string {
			var out []string
			for _, item := range v.([]any) {
				out = append(out, strings.Join(item.(*jsonx.Object).Keys(), ","))
			}
			sort.Strings(out)
			return strings.Join(out, " | ")
		}
		if keys(pyAgents) != keys(goAgents) {
			failed++
			t.Errorf("DIFF stored key order:\n  python %s\n  go     %s", keys(pyAgents), keys(goAgents))
		}
	}
	if py, gofiles := strings.Join(dataFiles(t, pyDir), " "), strings.Join(dataFiles(t, goDir), " "); py != gofiles {
		failed++
		t.Errorf("DIFF files under DATA_DIR:\n  python %s\n  go     %s", py, gofiles)
	}
	// docker stop: both save the monitoring history (A04) on the way out.
	stopPython()
	stopGo()
	failed += compareAfterStop(t, pyDir, goDir, pyValues, goValues)
	t.Logf("%d requests + stored records + DATA_DIR files + files after stop, %d differences", len(requests()), failed)
}

var timestampField = regexp.MustCompile(`"timestamp": ?"[^"]*"`)

// normalizedFiles is dataFiles with the per-server captured values (Agent
// IDs) replaced by their placeholder names.
func normalizedFiles(t *testing.T, dir string, values map[string]string) map[string]string {
	out := map[string]string{}
	for _, rel := range dataFiles(t, dir) {
		name := rel
		for key, value := range values {
			if value != "" {
				name = strings.ReplaceAll(name, value, "{"+key+"}")
			}
		}
		out[name] = rel
	}
	return out
}

// compareAfterStop compares the files under DATA_DIR after a graceful
// stop: the same names, the metrics files byte for byte except the
// heartbeat timestamps, and the metrics_history_flush run in task-status.json.
func compareAfterStop(t *testing.T, pyDir, goDir string, pyValues, goValues map[string]string) int {
	failed := 0
	pyFiles, goFiles := normalizedFiles(t, pyDir, pyValues), normalizedFiles(t, goDir, goValues)
	names := func(files map[string]string) string {
		var out []string
		for name := range files {
			out = append(out, name)
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	if names(pyFiles) != names(goFiles) {
		failed++
		t.Errorf("DIFF files under DATA_DIR after stop:\n  python %s\n  go     %s", names(pyFiles), names(goFiles))
	}
	compared := 0
	for name, pyRel := range pyFiles {
		goRel, ok := goFiles[name]
		if !ok || !strings.HasPrefix(name, "metrics/") {
			continue
		}
		pyData, _ := os.ReadFile(filepath.Join(pyDir, pyRel))
		goData, _ := os.ReadFile(filepath.Join(goDir, goRel))
		pyText := timestampField.ReplaceAllString(string(pyData), `"timestamp":"<t>"`)
		goText := timestampField.ReplaceAllString(string(goData), `"timestamp":"<t>"`)
		compared++
		if pyText != goText {
			failed++
			t.Errorf("DIFF %s:\n  python %q\n  go     %q", name, pyData, goData)
		} else {
			t.Logf("same %s (%d bytes)", name, len(pyData))
		}
	}
	status := func(dir string) any {
		data, err := os.ReadFile(filepath.Join(dir, "task-status.json"))
		if err != nil {
			return err.Error()
		}
		value, _ := jsonx.Parse(data)
		entry, _ := lookup(value, "metrics_history_flush").(*jsonx.Object)
		out := jsonx.NewObject()
		for _, key := range []string{"last_status", "last_trigger", "last_error", "run_count", "failure_count", "consecutive_failures"} {
			out.Set(key, entry.Value(key))
		}
		return out
	}
	if py, gor := status(pyDir), status(goDir); !jsonx.Equal(py, gor) {
		failed++
		t.Errorf("DIFF task-status metrics_history_flush:\n  python %s\n  go     %s", jsonx.Repr(py), jsonx.Repr(gor))
	}
	t.Logf("after stop: %d files, %d metrics files compared", len(pyFiles), compared)
	return failed
}
