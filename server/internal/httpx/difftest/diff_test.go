// Package difftest is the HTTP-level differential check of package #3: the
// real Flask app (python -m backend.app on waitress) and the Go application
// run on copies of the same data directory, both on loopback only, and the
// same request list is replayed against both. Status, the relevant headers
// and the body (JSON as values after masking tokens, times and paths; raw
// bytes otherwise) must match.
//
// It needs the Python environment of the test server and runs only when
// CONFIGFLOW_DIFF=1:
//
//	rt -- bash -lc 'cd server && CONFIGFLOW_DIFF=1 go test -count=1 -v ./internal/httpx/difftest/'
//
// CONFIGFLOW_DIFF_PYTHON overrides the interpreter (~/cf-test/.venv/bin/python).
package difftest

import (
	"bytes"
	"configflow/internal/repo/repotest"
	"context"
	"encoding/json"
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
	"testing"
	"time"

	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/server"
	"configflow/internal/testjson"
)

const (
	username      = "diff-admin"
	password      = "diff-password"
	secret        = "diff-shared-jwt-secret"
	internalToken = "diff-internal-rule-proxy-token"
	agentToken    = "diff-agent-token-0123456789"
)

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// seed creates the shared data directory: a second profile, an internal
// rule proxy token and an Agent token (both must be redacted), a fixed
// server domain (no local IP lookup), and nothing that points at a remote
// host, so neither server has anything to fetch.
func seed(t *testing.T, dir string) {
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "other", Name: "Other"}, ""); err != nil {
		t.Fatal(err)
	}
	profileID, _ := r.DefaultProfileID(ctx)
	err = r.UpdateSystem(ctx, func(_ context.Context, system *model.System) error {
		system.SystemConfig.ServerDomain = "http://127.0.0.1:5001"
		system.SystemConfig.RuleProxyToken = internalToken
		system.SystemConfig.RetiredRuleProxyTokens = []string{"diff-retired-token"}
		agentsRaw, err := json.Marshal([]any{map[string]any{"id": "agent-1", "name": "diff agent", "token": agentToken,
			"profile_id": profileID, "service_type": "mihomo", "host": "127.0.0.1", "port": 9}})
		if err != nil {
			return err
		}
		if err := system.SetJSON("agents", agentsRaw); err != nil {
			return err
		}
		return system.SetJSON("backup", json.RawMessage(`{"webdav_url":"","webdav_username":"","webdav_password":"","auto_backup":false}`))
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

func startPython(t *testing.T, dataDir string) string {
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
	cmd.Env = append(os.Environ(), "DATA_DIR="+dataDir, "LOG_FILE="+filepath.Join(dataDir, "app.log"),
		"CONFIGFLOW_BACKEND_HOST=127.0.0.1", "CONFIGFLOW_BACKEND_PORT="+strconv.Itoa(port),
		"ADMIN_USERNAME="+username, "ADMIN_PASSWORD="+password, "JWT_SECRET_KEY="+secret, "LOG_LEVEL=INFO",
		"SUB_STORE_BUILTIN_DIR=/nonexistent", "PYTHONDONTWRITEBYTECODE=1")
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
	t.Setenv("LOG_FILE", filepath.Join(dataDir, "app.log"))
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
	full, err := server.New(context.Background(), server.Options{Repo: r})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(full.Agents.Close)
	app := full.HTTP
	ctx := repo.WithScope(context.Background())
	if _, err := r.PrepareConfig(ctx); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(app)
	t.Cleanup(srv.Close)
	return srv.URL
}

type request struct {
	c06Difference string
	binding       *bindingExpectation
	name          string
	method        string
	path          string
	auth          string // "", "admin", "cross", "expired", "forged", "malformed", "claims-empty"
	headers       map[string]string
	body          string
	contentType   string
	// mask lists top-level (or dotted) JSON keys whose values differ by
	// design (random tokens, times, paths).
	mask []string
}

type response struct {
	status int
	header http.Header
	body   []byte
}

func send(t *testing.T, base string, r request, tokens map[string]string) response {
	var body io.Reader
	if r.body != "" {
		body = strings.NewReader(r.body)
	}
	req, err := http.NewRequest(r.method, base+r.path, body)
	if err != nil {
		t.Fatal(err)
	}
	if r.contentType != "" {
		req.Header.Set("Content-Type", r.contentType)
	}
	for k, v := range r.headers {
		req.Header.Set(k, v)
	}
	if token := tokens[r.auth]; token != "" {
		req.Header.Set("Authorization", token)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s: %v", r.name, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return response{resp.StatusCode, resp.Header, data}
}

func decodeJSONValue(data []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value any
	if err := d.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, fmt.Errorf("trailing JSON token")
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

func requests(t *testing.T) []request {
	J := "application/json"
	origin := map[string]string{"Origin": "https://client.example"}
	rows := []request{
		{name: "auth status", method: "GET", path: "/api/auth/status"},
		{name: "auth status with origin", method: "GET", path: "/api/auth/status", headers: origin},
		{name: "login wrong password", method: "POST", path: "/api/auth/login", body: `{"username":"diff-admin","password":"x"}`, contentType: J},
		{name: "login ok", method: "POST", path: "/api/auth/login", body: `{"username":"diff-admin","password":"diff-password"}`, contentType: J, mask: []string{"token"}},
		{name: "login GET 405", method: "GET", path: "/api/auth/login"},
		{name: "verify no token", method: "GET", path: "/api/auth/verify"},
		{name: "verify admin", method: "GET", path: "/api/auth/verify", auth: "admin"},
		{name: "verify cross-issued token", method: "GET", path: "/api/auth/verify", auth: "cross"},
		{name: "verify expired", method: "GET", path: "/api/auth/verify", auth: "expired"},
		{name: "verify forged", method: "GET", path: "/api/auth/verify", auth: "forged"},
		{name: "verify malformed bearer", method: "GET", path: "/api/auth/verify", auth: "malformed"},
		{name: "verify empty claims", method: "GET", path: "/api/auth/verify", auth: "claims-empty"},
		{name: "version", method: "GET", path: "/api/version"},
		{name: "version HEAD", method: "HEAD", path: "/api/version"},
		{name: "version PUT 405", method: "PUT", path: "/api/version"},
		{name: "version trailing slash", method: "GET", path: "/api/version/"},
		{name: "version merged slashes", method: "GET", path: "//api//version"},
		{name: "unknown path no token", method: "GET", path: "/api/missing"},
		{name: "unknown path admin", method: "GET", path: "/api/missing", auth: "admin"},
		{name: "unknown non-api path", method: "GET", path: "/anything"},
		{name: "root path", method: "GET", path: "/"},
		{name: "internal header guess", method: "GET", path: "/api/server-domain", headers: map[string]string{"X-ConfigFlow-Internal": "guess"}},
		{name: "preflight public", method: "OPTIONS", path: "/api/auth/login", headers: map[string]string{
			"Origin": "https://client.example", "Access-Control-Request-Method": "POST", "Access-Control-Request-Headers": "Content-Type, authorization"}},
		{name: "preflight protected", method: "OPTIONS", path: "/api/server-domain", headers: map[string]string{
			"Origin": "https://client.example", "Access-Control-Request-Method": "GET"}},
		{name: "preflight protected admin", method: "OPTIONS", path: "/api/server-domain", auth: "admin", headers: map[string]string{
			"Origin": "https://client.example", "Access-Control-Request-Method": "GET"}},
		{name: "server domain", method: "GET", path: "/api/server-domain", auth: "admin"},
		{name: "server domain HEAD", c06Difference: "head-500", method: "HEAD", path: "/api/server-domain", auth: "admin"},
		{name: "server domain set", method: "POST", path: "/api/server-domain", auth: "admin", body: `{"new_domain":" http://127.0.0.1:5002 "}`, contentType: J},
		{name: "server domain empty", method: "POST", path: "/api/server-domain", auth: "admin", body: `{"new_domain":""}`, contentType: J},
		{name: "server domain not json", c06Difference: "binding-415", method: "POST", path: "/api/server-domain", auth: "admin", body: `x`, contentType: "text/plain"},
		{name: "server domain number", method: "POST", path: "/api/server-domain", auth: "admin", body: `{"new_domain":5}`, contentType: J},
		{name: "server domain read back", method: "GET", path: "/api/server-domain", auth: "admin"},
		{name: "config token empty", method: "GET", path: "/api/config-token", auth: "admin"},
		{name: "config token internal", method: "POST", path: "/api/config-token", auth: "admin", body: `{"token":"x-` + internalToken + `"}`, contentType: J},
		{name: "config token retired encoded", method: "POST", path: "/api/config-token", auth: "admin", body: `{"token":"a%2Ddiff-retired-token"}`, contentType: J},
		{name: "config token blank", method: "POST", path: "/api/config-token", auth: "admin", body: `{"token":"  "}`, contentType: J},
		{name: "config token set", method: "POST", path: "/api/config-token", auth: "admin", body: `{"token":" pull-token "}`, contentType: J},
		{name: "config token generate", method: "POST", path: "/api/config-token", auth: "admin", body: `{"generate":true}`, contentType: J, mask: []string{"config_token"}},
		{name: "config token set again", method: "POST", path: "/api/config-token", auth: "admin", body: `{"token":"pull-token"}`, contentType: J},
		{name: "config token agent token redacted", method: "POST", path: "/api/config-token", auth: "admin", body: `{"token":"` + agentToken + `"}`, contentType: J},
		{name: "config token get", method: "GET", path: "/api/config-token", auth: "admin"},
		{name: "config token delete", method: "DELETE", path: "/api/config-token", auth: "admin"},
		{name: "rule fetch proxy", method: "GET", path: "/api/settings/rule-fetch-proxy", auth: "admin"},
		{name: "rule fetch proxy set", method: "POST", path: "/api/settings/rule-fetch-proxy", auth: "admin", body: `{"rule_fetch_proxy":" http://u:p@192.0.2.3:7890/ "}`, contentType: J},
		{name: "rule fetch proxy read", method: "GET", path: "/api/settings/rule-fetch-proxy", auth: "admin"},
		{name: "rule fetch proxy invalid", method: "POST", path: "/api/settings/rule-fetch-proxy", auth: "admin", body: `{"rule_fetch_proxy":"socks5://x:1"}`, contentType: J},
		{name: "rule fetch proxy missing", method: "POST", path: "/api/settings/rule-fetch-proxy", auth: "admin", body: `{}`, contentType: J},
		{name: "rule fetch proxy HEAD", c06Difference: "head-400", method: "HEAD", path: "/api/settings/rule-fetch-proxy", auth: "admin"},
		{name: "rule fetch proxy clear", method: "POST", path: "/api/settings/rule-fetch-proxy", auth: "admin", body: `{"rule_fetch_proxy":""}`, contentType: J},
		{name: "aggregation", method: "GET", path: "/api/settings/subscription-aggregation", auth: "admin"},
		{name: "aggregation set", method: "POST", path: "/api/settings/subscription-aggregation", auth: "admin", body: `{"enabled":true}`, contentType: J},
		{name: "aggregation set odd", method: "POST", path: "/api/settings/subscription-aggregation", auth: "admin", body: `{"enabled":[1]}`, contentType: J},
		{name: "aggregation bad", c06Difference: "binding-400", method: "POST", path: "/api/settings/subscription-aggregation", auth: "admin", body: `[]`, contentType: J},
		{name: "backup config", method: "GET", path: "/api/backup/config", auth: "admin"},
		{name: "backup config save", method: "POST", path: "/api/backup/config", auth: "admin", body: `{"webdav_url":"https://dav.invalid","webdav_password":"pw","keep_latest":7}`, contentType: J},
		{name: "backup config masked", method: "POST", path: "/api/backup/config", auth: "admin", body: `{"webdav_password":"******","auto_backup":false}`, contentType: J},
		{name: "backup config read", method: "GET", path: "/api/backup/config", auth: "admin"},
		{name: "backup config empty", method: "POST", path: "/api/backup/config", auth: "admin", body: `{}`, contentType: J},
		{name: "backup config bad type", method: "POST", path: "/api/backup/config", auth: "admin", body: `{"keep_daily":4000}`, contentType: J},
		{name: "web port", method: "GET", path: "/api/settings/web-port", auth: "admin"},
		{name: "web port missing", method: "POST", path: "/api/settings/web-port", auth: "admin", body: `{}`, contentType: J},
		{name: "web port reserved", method: "POST", path: "/api/settings/web-port", auth: "admin", body: `{"port":3001}`, contentType: J},
		{name: "web port unsupported", method: "POST", path: "/api/settings/web-port", auth: "admin", body: `{"port":8081}`, contentType: J},
		{name: "web port bad", method: "POST", path: "/api/settings/web-port", auth: "admin", body: `{"port":"80"}`, contentType: J},
		{name: "web port HEAD", c06Difference: "head-400", method: "HEAD", path: "/api/settings/web-port", auth: "admin"},
		{name: "stats overview", method: "GET", path: "/api/stats/overview", auth: "admin", mask: []string{"data.profile.updated_at"}},
		{name: "stats overview other profile", method: "GET", path: "/api/stats/overview", auth: "admin", headers: map[string]string{"X-ConfigFlow-Profile": "other"}, mask: []string{"data.profile.updated_at"}},
		{name: "stats overview query profile", method: "GET", path: "/api/stats/overview?profile=other", auth: "admin", mask: []string{"data.profile.updated_at"}},
		{name: "stats overview missing profile anonymous", method: "GET", path: "/api/stats/overview", headers: map[string]string{"X-ConfigFlow-Profile": "missing"}},
		{name: "stats overview bad profile", method: "GET", path: "/api/stats/overview?profile_id=bad!id", auth: "admin"},
		{name: "stats no token", method: "GET", path: "/api/stats/overview"},
		{name: "logs info", method: "GET", path: "/api/logs/info", auth: "admin", mask: []string{"path", "size", "size_mb", "modified", "created"}},
		{name: "logs tail bad level", method: "GET", path: "/api/logs/tail?level=%5B", auth: "admin", mask: []string{"error"}},
		{name: "logs clear", method: "POST", path: "/api/logs/clear", auth: "admin"},
		{name: "logs tail after clear", method: "GET", path: "/api/logs/tail?lines=0", auth: "admin", mask: []string{"log_file", "logs", "total_lines", "filtered_lines"}},
		{name: "notifications read invalid", method: "POST", path: "/api/notifications/read", auth: "admin", body: `{"versions":[]}`, contentType: J},
		{name: "notifications read empty", method: "POST", path: "/api/notifications/read", auth: "admin", body: `{"versions":{}}`, contentType: J},
		{name: "notifications read unknown", method: "POST", path: "/api/notifications/read", auth: "admin", body: `{"versions":{"x":"y"}}`, contentType: J},
		{name: "notifications read no token", method: "POST", path: "/api/notifications/read", body: `{"versions":{}}`, contentType: J},
	}
	rows = append(rows, bindingRequests(t)...)
	for _, ct := range []string{J, J + "; charset=utf-8", "application/merge-patch+json"} {
		rows = append(rows, request{name: "frontend login " + ct, method: "POST", path: "/api/auth/login", body: `{"username":"diff-admin","password":"diff-password"}`, contentType: ct, mask: []string{"token"}})
	}
	for _, body := range []string{`{"username":true,"password":"diff-password"}`, `{"username":"diff-admin","password":12}`, `{"username":[],"password":{}}`, `{"Username":"diff-admin","password":"diff-password"}`} {
		rows = append(rows, request{name: "typed credential " + body, method: "POST", path: "/api/auth/login", body: body, contentType: J})
	}
	for _, value := range []string{"60", "60.0", "6e1", "true", "false", `"60"`, "9223372036854775808", "null", "[]", "{}"} {
		rows = append(rows, request{name: "R226 literal " + value, method: "PUT", path: "/api/tasks/interval", auth: "admin", body: `{"key":"subscription:missing","interval_seconds":` + value + `}`, contentType: J})
	}
	for _, body := range []string{`{"Key":"subscription:missing","interval_seconds":60}`, `{"key":"subscription:missing","interval_seconds":60,"extra":0}`, `{"key":"wrong","key":"subscription:missing","interval_seconds":59,"interval_seconds":60}`} {
		rows = append(rows, request{name: "R226 shape " + body, method: "PUT", path: "/api/tasks/interval", auth: "admin", body: body, contentType: J})
	}
	frontend := `{"username":"diff-admin","password":"diff-password"}`
	rows = append(rows, request{name: "BOM login", method: "POST", path: "/api/auth/login", body: "\xef\xbb\xbf" + frontend, contentType: J, mask: []string{"token"}})
	encoded := []byte{0xff, 0xfe}
	for _, b := range []byte(frontend) {
		encoded = append(encoded, b, 0)
	}
	rows = append(rows, request{name: "UTF16 login", method: "POST", path: "/api/auth/login", body: string(encoded), contentType: J, mask: []string{"token"}})

	return rows

}

var portPattern = regexp.MustCompile(`127\.0\.0\.1:\d+`)

func maskJSON(value any, mask []string) any {
	for _, path := range mask {
		parts := strings.Split(path, ".")
		current := value
		for i, part := range parts {
			object, ok := current.(map[string]any)
			if !ok {
				break
			}
			if i == len(parts)-1 {
				if _, exists := object[part]; exists {
					object[part] = "<masked>"
				}
				break
			}
			current = object[part]
		}
	}
	return value
}

func compare(r request, py, gor response) []string {
	if r.c06Difference != "" {
		return c06ExpectedDifference(r, py, gor)
	}
	// O9-H1/L5: compare the exact approved security headers, retaining all others.
	py.header = httpxtest.O9ExpectedHeaders(py.header, gor.header)
	if r.name == "stats overview missing profile anonymous" {
		// O9-L6: verify the original 404 and the new exact 401 independently.
		var oldBody, newBody map[string]any
		if py.status != 404 || gor.status != 401 || json.Unmarshal(py.body, &oldBody) != nil || json.Unmarshal(gor.body, &newBody) != nil || len(oldBody) != 2 || oldBody["success"] != false || oldBody["message"] != "Profile not found: missing" || len(newBody) != 2 || newBody["success"] != false || newBody["message"] != "请先登录后再操作。" || gor.header.Get("X-ConfigFlow-Profile") != "" {
			return []string{"O9-L6 profile-before-auth migration mismatch"}
		}
		py.status, py.body = gor.status, gor.body
	}
	// O9-L6 also removes the old profile echo from the existing-profile 401.
	if r.name == "stats no token" && py.status == 401 && gor.status == 401 && py.header.Get("X-ConfigFlow-Profile") == "default" {
		py.header.Del("X-ConfigFlow-Profile")
	}
	var diffs []string
	if r.binding != nil {
		py, diffs = bindingDifference(r, py, gor)
	}
	migrated := httpxtest.MigratedAPIError(r.method, py.status, gor.status, py.header, gor.header, py.body, gor.body)
	if py.status != gor.status {
		diffs = append(diffs, fmt.Sprintf("status python %d go %d", py.status, gor.status))
	}
	for _, name := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Profile", "Access-Control-Allow-Origin",
		"Access-Control-Allow-Headers", "Access-Control-Allow-Methods", "Vary", "Content-Disposition", "X-Content-Type-Options"} {
		if migrated && name == "Content-Type" {
			continue
		}
		a, b := strings.Join(py.header.Values(name), ","), strings.Join(gor.header.Values(name), ",")
		if a != b {
			diffs = append(diffs, fmt.Sprintf("header %s python %q go %q", name, a, b))
		}
	}
	if a, b := portPattern.ReplaceAllString(py.header.Get("Location"), "HOST"), portPattern.ReplaceAllString(gor.header.Get("Location"), "HOST"); a != b {
		diffs = append(diffs, fmt.Sprintf("header Location python %q go %q", a, b))
	}
	if a, b := methodSet(py.header.Get("Allow")), methodSet(gor.header.Get("Allow")); a != b {
		diffs = append(diffs, fmt.Sprintf("header Allow python %q go %q", a, b))
	}
	pv, perr := decodeJSONValue(py.body)
	gv, gerr := decodeJSONValue(gor.body)
	switch {
	case migrated:
		// Step B #3/O12: the exact JSON error envelope was verified above.
	case perr == nil && gerr == nil:
		if r.name == "server domain number" && py.status == 500 && gor.status == 500 {
			expected := map[string]any{"success": false, "message": "setting must be a string"}
			original := map[string]any{"success": false, "message": "'int' object has no attribute 'strip'"}
			if !testjson.Equal(pv, original) || !testjson.Equal(gv, expected) {
				diffs = append(diffs, "unexpected native setting type error envelope")
			}
			break
		}
		if !testjson.Equal(maskJSON(pv, r.mask), maskJSON(gv, r.mask)) {
			diffs = append(diffs, fmt.Sprintf("json python %s go %s", py.body, gor.body))
		}
	default:
		if !bytes.Equal(py.body, gor.body) {
			diffs = append(diffs, fmt.Sprintf("body python %q go %q", truncate(py.body), truncate(gor.body)))
		}
		if len(r.mask) == 0 && py.header.Get("Content-Length") != gor.header.Get("Content-Length") && r.method != "HEAD" {
			diffs = append(diffs, fmt.Sprintf("Content-Length python %s go %s", py.header.Get("Content-Length"), gor.header.Get("Content-Length")))
		}
	}
	return diffs
}

func methodSet(allow string) string {
	if allow == "" {
		return ""
	}
	parts := strings.Split(allow, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	sort.Strings(parts)
	return strings.Join(parts, ", ")
}

func truncate(b []byte) string {
	if len(b) > 400 {
		return string(b[:400]) + "…"
	}
	return string(b)
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
	pyBase := startPython(t, pyDir)
	goBase := startGo(t, goDir)

	pyToken, goToken := login(t, pyBase), login(t, goBase)
	expired, _ := auth.EncodeHS256(map[string]any{"username": username, "exp": time.Now().Add(-time.Hour).Unix(),
		"iat": time.Now().Add(-25 * time.Hour).Unix()}, secret)
	forged, _ := auth.EncodeHS256(map[string]any{"username": username, "exp": time.Now().Add(time.Hour).Unix()}, "not-the-secret")
	empty, _ := auth.EncodeHS256(map[string]any{}, secret)
	common := map[string]string{"expired": "Bearer " + expired, "forged": "Bearer " + forged,
		"malformed": "Bearer  " + strings.TrimPrefix(goToken, "Bearer "), "claims-empty": "Bearer " + empty}
	pyTokens, goTokens := map[string]string{"admin": pyToken, "cross": goToken}, map[string]string{"admin": goToken, "cross": pyToken}
	for k, v := range common {
		pyTokens[k], goTokens[k] = v, v
	}
	failed := 0
	for _, r := range requests(t) {
		py := send(t, pyBase, r, pyTokens)
		gor := send(t, goBase, r, goTokens)
		if diffs := compare(r, py, gor); len(diffs) > 0 {
			failed++
			t.Errorf("DIFF %s %s %s:\n  %s", r.name, r.method, r.path, strings.Join(diffs, "\n  "))
		} else if r.binding != nil {
			t.Logf("expected binding migration %-25s Python %d → Go %d (both full bodies checked)", r.name, py.status, gor.status)
		} else {
			t.Logf("same %-40s %s %s → %d", r.name, r.method, r.path, py.status)
		}
	}
	t.Logf("%d requests, %d differences", len(requests(t)), failed)
}
