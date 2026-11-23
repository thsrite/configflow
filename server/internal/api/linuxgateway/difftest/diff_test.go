// Package difftest compares A20 HTTP, storage and generated configurations with real Flask.
package difftest

import (
	"bufio"
	"bytes"
	"configflow/internal/model/modeltest"
	"configflow/internal/repo/repotest"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	apiagents "configflow/internal/api/agents"
	apigateway "configflow/internal/api/linuxgateway"
	apirules "configflow/internal/api/rules"
	"configflow/internal/auth"
	"configflow/internal/convert"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/gateway"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/routersplit"
	"configflow/internal/yamlx"
)

func jsonValue(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if err = json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	return result
}
func snapshot(t *testing.T, s *httpxtest.Server) any {
	t.Helper()
	doc, err := s.Repo.DiagnosticExportJSON(s.Context())
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]any{}
	err = filepath.WalkDir(s.DataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if filepath.Dir(path) == s.DataDir && isDatabaseFile(filepath.Base(path)) || ext == ".lock" || ext == ".log" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any
		switch ext {
		case ".json":
			err = json.Unmarshal(data, &value)
		case ".yaml", ".yml":
			err = yamlx.Unmarshal(data, &value)
		default:
			value = fmt.Sprintf("sha256:%x", sha256.Sum256(data))
		}
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(s.DataDir, path)
		files[filepath.ToSlash(relative)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", filepath.Join(s.DataDir, "configflow.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	sections := [][]any{}
	rows, err := db.Query("SELECT scope, owner, position FROM sections ORDER BY scope, owner")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var scope, owner string
		var position int
		if err = rows.Scan(&scope, &owner, &position); err != nil {
			t.Fatal(err)
		}
		sections = append(sections, []any{scope, owner, position})
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	fields := [][]any{}
	rows, err = db.Query("SELECT scope, owner, field, position, value FROM fields ORDER BY scope, owner, field")
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var scope, owner, field, value string
		var position int
		if err = rows.Scan(&scope, &owner, &field, &position, &value); err != nil {
			t.Fatal(err)
		}
		if scope == "system" && field == "agents" {
			value = repotest.MergedAgentsRow(t, filepath.Join(s.DataDir, "configflow.sqlite3"), value) // as Python stores it (O17)
		}
		var data any
		if err = json.Unmarshal([]byte(value), &data); err != nil {
			t.Fatal(err)
		}
		fields = append(fields, []any{scope, owner, field, position, data})
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	return jsonValue(t, map[string]any{"document": doc, "files": files, "sections": sections, "fields": fields})
}

type answer struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
	State   any               `json:"state"`
	Error   string            `json:"oracle_error"`
}
type wireRequest struct{ Method, Path, Auth, Accept, ContentType, Body string }
type fakeAgent struct {
	mu    sync.Mutex
	phase string
	calls []wireRequest
}

func (f *fakeAgent) take() []wireRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := f.calls
	f.calls = nil
	return out
}
func (f *fakeAgent) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	phase := f.phase
	f.calls = append(f.calls, wireRequest{r.Method, r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("Accept"), r.Header.Get("Content-Type"), string(raw)})
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	status := 200
	body := `{"success":true,"supported":true,"managed":{"active":false},"ipv4_forwarding":{"supported":true,"current":"1","persisted":true,"layout":"native_aio"}}`
	switch phase {
	case "record":
		body = `{"success":true,"supported":true,"managed":{"active":true,"preserves_forwarding":false},"ipv4_forwarding":{"supported":true,"current":"1"}}`
	case "kept":
		body = `{"success":true,"managed":{"active":true,"preserves_forwarding":true}}`
	case "notjson":
		body = "not json"
	case "array":
		body = "[]"
	case "big":
		body = strings.Repeat("x", 512*1024+1)
	case "401":
		status = 401
	case "404":
		status = 404
	case "409":
		status = 409
		body = `{"message":"合成设备拒绝"}`
	case "500":
		status = 500
		body = `{"error":"合成设备错误"}`
	case "unsupported":
		body = `{"success":true,"supported":false,"ipv4_forwarding":{"supported":false,"unsupported_reason":"合成平台不支持"}}`
	case "needs":
		body = `{"success":true,"ipv4_forwarding":{"supported":true,"current":"0","notice":"合成启动提示"}}`
	case "system":
		body = `{"success":true,"ipv4_forwarding":{"supported":true,"current":"1","system_persisted":true,"system_source":"/etc/sysctl.d/example.conf:2"}}`
	case "foreign":
		body = `{"success":true,"ipv4_forwarding":{"supported":true,"config":"foreign"}}`
	case "hook":
		body = `{"success":true,"ipv4_forwarding":{"supported":true,"hook_script":"modified"}}`
	case "standalone":
		body = `{"success":true,"supported":true,"ipv4_forwarding":{"supported":false}}`
	case "old":
		body = `{"success":true,"supported":true}`
	case "filter":
		body = `{"success":true,"forward_filter":{"protocol":1,"state":"may_drop","device":"Meta","flows":["forward","dns"],"forward_protocols":["udp","tcp","udp"],"remedies":[{"source":"iptables","device":"Meta","commands":["iptables -I FORWARD -o Meta -j ACCEPT","iptables -F; reboot"]}]}}`
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

type world struct {
	t                 *testing.T
	srv               *httpxtest.Server
	stdin             io.WriteCloser
	scanner           *bufio.Scanner
	cmd               *exec.Cmd
	stderr            bytes.Buffer
	token             string
	fake              *fakeAgent
	profileHeaders    []string
	privacyCorrection bool
}

func newWorld(t *testing.T, sessions ...string) *world {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("CONFIGFLOW_DIFF=1 on test server")
	}
	w := &world{t: t, fake: &fakeAgent{}}
	fake := httptest.NewServer(w.fake)
	t.Cleanup(fake.Close)
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(fake.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	w.srv = httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: "a20-secret", Register: httpxtest.Routes(apiagents.Register, apigateway.Register)})
	gateway.Wire()
	mihomoenv.Wire()
	apirules.Wire()
	ctx := w.srv.Context()
	r := w.srv.Repo
	_, err := r.CreateProfileRecord(ctx, repo.ProfileCreation{ID: "home", Name: "Home"}, "")
	if err != nil {
		t.Fatal(err)
	}
	err = r.UpdateSystem(ctx, func(_ context.Context, c *model.System) error {
		c.SystemConfig.ConfigToken = "a20-subscription"
		c.SystemConfig.ServerDomain = "http://localhost"
		raw, err := json.Marshal([]any{map[string]any{"id": "gateway", "name": "Gateway", "host": "127.0.0.1", "port": port, "token": "a20-agent-token", "service_type": "mihomo", "deployment_method": "shell", "profile_id": "home", "version": "1.8.43-go", "status": "online", "enabled": true, "last_heartbeat": time.Now().UTC().Format(time.RFC3339Nano)}})
		if err != nil {
			return err
		}
		return c.SetJSON("agents", raw)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = r.UpdateShared(ctx, func(_ context.Context, c *model.Shared) error {
		return c.SetJSON("rule_library", json.RawMessage(`[{"id":"lib","name":"IP","source_type":"content","content":"192.0.2.0/24\n198.51.100.0/24","behavior":"ipcidr","format":"text","enabled":true}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.UpdateProfile(ctx, "home", func(_ context.Context, c *model.Profile) error {
		if err := c.SetJSON("mihomo", json.RawMessage(`{"custom_config":"mixed-port: 7890\ndns: {enable: true, enhanced-mode: fake-ip, fake-ip-range: 198.18.0.1/16}\n","linux_gateway":{"enabled":true,"rule_set_ids":["r"]},"future":{"unchanged":[null,true,1.5]}}`)); err != nil {
			return err
		}
		if err := c.SetJSON("oxidns", json.RawMessage(`{"native_config":"plugins: []\n","future":"keep"}`)); err != nil {
			return err
		}
		return c.SetJSON("rule_configs", json.RawMessage(`[{"id":"r","itemType":"ruleset","library_rule_id":"lib","policy":"DIRECT","enabled":true}]`))
	})
	if err != nil {
		t.Fatal(err)
	}
	for i, body := range sessions {
		root := filepath.Join(w.srv.DataDir, "setup-sessions")
		if err := os.MkdirAll(root, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, strings.Repeat(string(rune('a'+i)), 32)+".json"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	pyDir := t.TempDir()
	if out, err := exec.Command("cp", "-a", w.srv.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
		t.Fatalf("copy %v %s", err, out)
	}
	root, _ := filepath.Abs("../../../../..")
	script, _ := filepath.Abs("testdata/reference.py")
	home, _ := os.UserHomeDir()
	py := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if py == "" {
		py = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	w.cmd = exec.Command(py, script)
	w.cmd.Dir = root
	repotest.PythonLayout(t, pyDir) // Python reads the version-1 layout (O17)
	w.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+pyDir, "ADMIN_USERNAME=diff-admin", "ADMIN_PASSWORD=diff-password", "JWT_SECRET_KEY=a20-secret", "LOG_FILE="+filepath.Join(pyDir, "app.log"), "PYTHONDONTWRITEBYTECODE=1")
	w.cmd.Stderr = &w.stderr
	w.stdin, err = w.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := w.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	w.scanner = bufio.NewScanner(stdout)
	w.scanner.Buffer(make([]byte, 65536), 32<<20)
	if err = w.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = w.stdin.Close()
		_ = w.cmd.Process.Kill()
		_ = w.cmd.Wait()
		if t.Failed() {
			t.Log(w.stderr.String())
		}
	})
	w.token, err = auth.EncodeHS256(map[string]any{"username": "diff-admin", "exp": time.Now().Add(time.Hour).Unix()}, "a20-secret")
	if err != nil {
		t.Fatal(err)
	}
	return w
}
func isDatabaseFile(name string) bool {
	switch name {
	case "configflow.sqlite3", "configflow.sqlite3-wal", "configflow.sqlite3-shm", "configflow.sqlite3-journal":
		return true
	}
	return false
}

var previewIdentity = regexp.MustCompile(`^[0-9a-f]{64}$`)
var restoreIdentity = regexp.MustCompile(`^[0-9a-f]{32}$`)

// normalize changes only independently-generated identities/times at their
// declared schema paths. Identically named user fields remain fully compared.
func normalize(t *testing.T, value any, kind string) any {
	t.Helper()
	timestamp := func(v any) any {
		t.Helper()
		s, ok := v.(string)
		if !ok {
			t.Fatalf("metadata timestamp is not a string: %T", v)
		}
		if _, err := time.Parse(time.RFC3339Nano, s); err != nil {
			t.Fatalf("invalid metadata timestamp: %q", s)
		}
		return "<time>"
	}
	bodyMap, _ := value.(map[string]any)
	previewBody := kind == "body" && (bodyMap["action"] == "save" || bodyMap["action"] == "restore") && bodyMap["config_revision"] != nil && bodyMap["changes"] != nil
	editorBody := kind == "body" && bodyMap["config_revision"] != nil && (bodyMap["settings"] != nil || previewBody)
	var walk func(any, []string) any
	walk = func(value any, path []string) any {
		switch x := value.(type) {
		case map[string]any:
			profileMeta := len(path) == 4 && path[0] == "state" && path[1] == "document" && path[2] == "profiles"
			summary := editorBody && len(path) == 2 && path[0] == "body" && path[1] == "restore_point"
			point := len(path) == 6 && path[0] == "state" && path[1] == "document" && path[2] == "profiles" && path[4] == "mihomo" && path[5] == "linux_gateway_restore_point" || len(path) == 3 && path[0] == "sql-mihomo" && path[2] == "linux_gateway_restore_point"
			rootBody := len(path) == 1 && path[0] == "body"
			for key, item := range x {
				if item == nil && (slices.Contains(path, "agents") || key == "version") {
					// A stored null Agent member is a missing one since O18 T1b.
					delete(x, key)
					continue
				}
				if profileMeta && (key == "created_at" || key == "updated_at") || (summary || point) && key == "created_at" {
					x[key] = timestamp(item)
					continue
				}
				if (summary || point) && key == "id" {
					id, ok := item.(string)
					if ok && restoreIdentity.MatchString(id) {
						x[key] = "<restore-id>"
					}
					continue
				}
				if rootBody && previewBody && key == "preview_token" {
					token, ok := item.(string)
					if !ok || !previewIdentity.MatchString(token) {
						t.Fatalf("invalid preview identity: %T", item)
					}
					x[key] = "<preview-token>"
					continue
				}
				if rootBody && previewBody && key == "changes" {
					changes, ok := item.([]any)
					if !ok {
						t.Fatalf("changes is not an array: %T", item)
					}
					for _, entry := range changes {
						change, ok := entry.(map[string]any)
						if !ok {
							t.Fatalf("change is not an object: %T", entry)
						}
						for _, side := range []string{"before", "after"} {
							text, ok := change[side].(string)
							if !ok {
								t.Fatalf("change display is not text: %T", change[side])
							}
							if strings.HasPrefix(text, "内容摘要 ") {
								continue
							}
							if node, err := yamlx.Parse([]byte(text)); err == nil {
								if b, err := yamlx.ToJSON(node); err == nil {
									var decoded any
									if err = json.Unmarshal(b, &decoded); err != nil {
										t.Fatal(err)
									}
									change[side] = decoded
								}
							}
						}
					}
					continue
				}
				x[key] = walk(item, append(append([]string{}, path...), key))
			}
		case []any:
			sqlRow := len(path) == 3 && path[0] == "state" && path[1] == "fields" && len(x) == 5 && x[0] == "profile"
			if sqlRow {
				if x[2] == "created_at" || x[2] == "updated_at" {
					x[4] = timestamp(x[4])
					return x
				}
				if x[2] == "mihomo" {
					x[4] = walk(x[4], []string{"sql-mihomo", fmt.Sprint(x[1])})
					return x
				}
			}
			for i, item := range x {
				x[i] = walk(item, append(append([]string{}, path...), strconv.Itoa(i)))
			}
		}
		return value
	}
	return walk(value, []string{kind})
}

// An editor summary must name the exact snapshot persisted for its profile;
// random-ID normalization must never hide a response/storage disagreement.
func summaryMatchesStorage(t *testing.T, body, state any, path string) {
	t.Helper()
	response, ok := body.(map[string]any)
	if !ok {
		return
	}
	if response["config_revision"] == nil || response["settings"] == nil && response["action"] != "save" && response["action"] != "restore" {
		return
	}
	summary, ok := response["restore_point"].(map[string]any)
	if !ok {
		return
	}
	profile := "home"
	parts := strings.Split(strings.Split(path, "?")[0], "/")
	if len(parts) > 3 && parts[2] == "profiles" {
		profile = parts[3]
	}
	root := state.(map[string]any)
	document := root["document"].(map[string]any)
	profiles := document["profiles"].(map[string]any)
	stored := profiles[profile].(map[string]any)["mihomo"].(map[string]any)["linux_gateway_restore_point"].(map[string]any)
	if summary["id"] != stored["id"] || summary["created_at"] != stored["created_at"] {
		t.Fatal("restore summary does not name persisted snapshot")
	}
}

func (w *world) call(method, path, body string, authHeader bool) (answer, answer) {
	return w.callBodies(method, path, body, body, authHeader)
}
func (w *world) callBodies(method, path, goBody, pyBody string, authHeader bool) (answer, answer) {
	w.t.Helper()
	headers := map[string]string{}
	if authHeader {
		headers["Authorization"] = "Bearer " + w.token
	}
	headers["Content-Type"] = "application/json"
	if len(w.profileHeaders) > 0 {
		headers["X-ConfigFlow-Profile"] = strings.Join(w.profileHeaders, ", ")
	}
	input := map[string]any{"method": method, "path": path, "headers": headers, "body": pyBody}
	data, _ := json.Marshal(input)
	_, _ = fmt.Fprintln(w.stdin, string(data))
	if !w.scanner.Scan() {
		w.t.Fatalf("oracle stopped %v %s", w.scanner.Err(), w.stderr.String())
	}
	var want answer
	if err := json.Unmarshal(w.scanner.Bytes(), &want); err != nil || want.Error != "" {
		w.t.Fatalf("oracle %s err %v stderr %s", w.scanner.Bytes(), err, w.stderr.String())
	}
	pyCalls := w.fake.take()
	var got answer
	got.Headers = map[string]string{}
	if method == "EDIT" {
		var updates map[string]json.RawMessage
		if err := json.Unmarshal([]byte(goBody), &updates); err != nil {
			w.t.Fatal(err)
		}
		_, err := w.srv.Repo.UpdateProfile(w.srv.Context(), path, func(_ context.Context, c *model.Profile) error {
			for k, v := range updates {
				if err := c.SetJSON(k, v); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			w.t.Fatal(err)
		}
		got.Status = 200
		got.Body = map[string]any{}
	} else if method == "AGENT" {
		var updates map[string]json.RawMessage
		if err := json.Unmarshal([]byte(goBody), &updates); err != nil {
			w.t.Fatal(err)
		}
		err := w.srv.Repo.UpdateSystem(w.srv.Context(), func(_ context.Context, c *model.System) error {
			raw, err := json.Marshal(c.Agents[0])
			if err != nil {
				return err
			}
			var obj map[string]json.RawMessage
			if err := json.Unmarshal(raw, &obj); err != nil {
				return err
			}
			for k, v := range updates {
				obj[k] = v
			}
			merged, err := json.Marshal([]any{obj})
			if err != nil {
				return err
			}
			return c.SetJSON("agents", merged)
		})
		if err != nil {
			w.t.Fatal(err)
		}
		got.Status = 200
		got.Body = map[string]any{}
	} else if method == "GENERATE" {
		config, err := w.srv.Repo.Config(w.srv.Context(), path, repo.ResourceRoots{})
		if err != nil {
			w.t.Fatal(err)
		}
		text, err := convert.MihomoConfig(w.srv.Context(), config, convert.MihomoOptions{BaseURL: "http://localhost", SkipProviderPreflight: true})
		if err != nil {
			w.t.Fatal(err)
		}
		n, err := yamlx.Parse([]byte(text))
		if err != nil {
			w.t.Fatal(err)
		}
		b, err := yamlx.ToJSON(n)
		if err != nil {
			w.t.Fatal(err)
		}
		_ = json.Unmarshal(b, &got.Body)
		got.Status = 200
	} else {
		request := httptest.NewRequest(method, "http://localhost"+path, strings.NewReader(goBody))
		for k, v := range headers {
			request.Header.Set(k, v)
		}
		if len(w.profileHeaders) > 0 {
			request.Header.Del("X-ConfigFlow-Profile")
			for _, value := range w.profileHeaders {
				request.Header.Add("X-ConfigFlow-Profile", value)
			}
		}
		rec := httptest.NewRecorder()
		w.srv.Handler().ServeHTTP(rec, request)
		got.Status = rec.Code
		for _, k := range []string{"Content-Type", "Cache-Control"} {
			if value := rec.Header().Get(k); value != "" {
				got.Headers[k] = value
			}
		}
		if strings.HasPrefix(rec.Header().Get("Content-Type"), "application/json") {
			if err := json.Unmarshal(rec.Body.Bytes(), &got.Body); err != nil {
				w.t.Fatal(err)
			}
		} else if strings.HasPrefix(rec.Header().Get("Content-Type"), "text/yaml") {
			gn, err := yamlx.Parse(rec.Body.Bytes())
			if err != nil {
				w.t.Fatal(err)
			}
			gb, err := yamlx.ToJSON(gn)
			if err != nil {
				w.t.Fatal(err)
			}
			_ = json.Unmarshal(gb, &got.Body)
			pn, err := yamlx.Parse([]byte(want.Body.(string)))
			if err != nil {
				w.t.Fatal(err)
			}
			pb, err := yamlx.ToJSON(pn)
			if err != nil {
				w.t.Fatal(err)
			}
			_ = json.Unmarshal(pb, &want.Body)
		} else {
			got.Body = rec.Body.String()
		}
	}
	goCalls := w.fake.take()
	if len(pyCalls) != len(goCalls) {
		w.t.Fatalf("%s %s wire count PY=%v GO=%v", method, path, pyCalls, goCalls)
	}
	for i := range pyCalls {
		p, g := pyCalls[i], goCalls[i]
		if p.Method != g.Method || p.Path != g.Path || p.Auth != g.Auth || p.Accept != g.Accept || p.ContentType != g.ContentType {
			w.t.Fatalf("wire PY=%+v GO=%+v", p, g)
		}
		if p.Body != "" || g.Body != "" {
			var pv, gv any
			_ = json.Unmarshal([]byte(p.Body), &pv)
			_ = json.Unmarshal([]byte(g.Body), &gv)
			if !reflect.DeepEqual(pv, gv) || p.Body == "" || g.Body == "" {
				w.t.Fatalf("wire body PY=%q GO=%q", p.Body, g.Body)
			}
		}
	}
	if w.privacyCorrection {
		assertPrivacyCorrection(w.t, got.Body, want.Body)
	}
	got.State = snapshot(w.t, w.srv)
	summaryMatchesStorage(w.t, got.Body, got.State, path)
	summaryMatchesStorage(w.t, want.Body, want.State, path)
	for _, k := range []string{"Content-Type", "Cache-Control"} {
		if want.Headers[k] != got.Headers[k] {
			w.t.Errorf("%s %s header %s PY=%q GO=%q", method, path, k, want.Headers[k], got.Headers[k])
		}
	}
	if got.Status != want.Status {
		w.t.Fatalf("%s %s status PY=%d GO=%d PY=%v GO=%v", method, path, want.Status, got.Status, want.Body, got.Body)
	}
	nonFiniteDifference := goBody == `{"enabled":NaN}`
	if nonFiniteDifference {
		expectedGo := map[string]any{"success": false, "message": "Linux 网关设置必须是 JSON 对象"}
		expectedPy := map[string]any{"success": false, "message": "enabled 必须是布尔值"}
		if got.Status != 400 || !reflect.DeepEqual(got.Body, expectedGo) || !reflect.DeepEqual(want.Body, expectedPy) {
			w.t.Fatal("non-finite request contract changed", got, want)
		}
	}
	for _, pair := range []struct {
		name string
		g, p any
	}{{"body", got.Body, want.Body}, {"state", got.State, want.State}} {
		if nonFiniteDifference && pair.name == "body" {
			continue
		}
		gn, pn := normalize(w.t, jsonValue(w.t, pair.g), pair.name), normalize(w.t, jsonValue(w.t, pair.p), pair.name)
		if pair.name == "state" {
			// Stored documents compare as the plain records hold them (O18 T1c).
			gn, pn = modeltest.StateThroughModel(gn), modeltest.StateThroughModel(pn)
			maskSystemRevision(gn)
			maskSystemRevision(pn)
		} else {
			pn = modeltest.AlignResponse(pn, gn)
		}
		if !reflect.DeepEqual(gn, pn) {
			g, _ := json.Marshal(gn)
			p, _ := json.Marshal(pn)
			w.t.Fatalf("%s %s %s differs\nPY %s\nGO %s", method, path, pair.name, p, g)
		}
	}
	return got, want
}

func TestA20FlaskHTTPAndStorage(t *testing.T) {
	w := newWorld(t)
	for _, root := range []string{"/api/profiles/home/custom-config/linux-gateway", "/api/custom-config/linux-gateway?profile_id=home"} {
		_ = root
	}
	path := "/api/profiles/home/custom-config/linux-gateway"
	w.call("GET", path, "", true)
	w.call("GET", path, "", false)
	w.call("POST", path, `{"enabled":NaN}`, true)
	for _, body := range []string{`[]`, `null`, `{`, `{"enabled":1}`, `{"enabled":true,"rule_set_ids":["missing"]}`} {
		w.call("POST", path, body, true)
	}
	for _, prefix := range []string{"/api/profiles/home/custom-config/linux-gateway", "/api/custom-config/linux-gateway"} {
		query := ""
		if strings.HasPrefix(prefix, "/api/custom") {
			query = "?profile_id=home"
		}
		w.call("GET", prefix+query, "", true)
		for _, suffix := range []string{"/preview", "/restore/preview", "/restore"} {
			w.call("POST", prefix+suffix+query, `{}`, true)
			w.call("POST", prefix+suffix+query, `{}`, false)
		}
	}
	rev, _ := w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
	body := fmt.Sprintf(`{"settings":{"enabled":false,"rule_set_ids":[]},"expected_config_revision":%d}`, rev)
	g, p := w.call("POST", path+"/preview", body, true)
	gt := g.Body.(map[string]any)["preview_token"].(string)
	pt := p.Body.(map[string]any)["preview_token"].(string)
	w.call("POST", path, `{"settings":{"enabled":false},"preview_token":"bad","expected_config_revision":0}`, true)
	w.callBodies("POST", path, strings.TrimSuffix(body, "}")+`,"preview_token":"`+gt+`"}`, strings.TrimSuffix(body, "}")+`,"preview_token":"`+pt+`"}`, true)
	w.call("GENERATE", "home", "", true)
	w.call("EDIT", "home", `{"oxidns":{"native_config":"plugins: [{tag: cache, type: cache}]\n","future":"later"}}`, true)
	rev, _ = w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
	g, p = w.call("POST", path+"/restore/preview", fmt.Sprintf(`{"expected_config_revision":%d}`, rev), true)
	gb := fmt.Sprintf(`{"expected_config_revision":%d,"preview_token":%q}`, rev, g.Body.(map[string]any)["preview_token"])
	pb := fmt.Sprintf(`{"expected_config_revision":%d,"preview_token":%q}`, rev, p.Body.(map[string]any)["preview_token"])
	w.callBodies("POST", path+"/restore", gb, pb, true)
	for _, prefix := range []string{"/api/profiles/home/linux-gateway", "/api/custom-config/linux-gateway"} {
		for _, kind := range []string{"rules", "source"} {
			for _, id := range []string{"r", "fake-ip", "deleted", "a/b"} {
				base := prefix + "/" + kind + "/" + id
				w.call("GET", base+"?token=a20-subscription&profile_id=home", "", false)
				w.call("GET", base+"?token=wrong&profile_id=home", "", false)
			}
		}
	}
}

const spec = `{"interface":"eth0","address":"192.0.2.2","lan_cidr":"192.0.2.0/24","upstream_gateway":"192.0.2.1"}`

func TestA20HostFlask(t *testing.T) {
	w := newWorld(t)
	path := "/api/agents/gateway/linux-gateway/"
	for _, op := range []struct{ method, op, body string }{{"GET", "status", ""}, {"POST", "preview", spec}, {"POST", "apply", spec}, {"POST", "remove", `{}`}, {"POST", "forwarding", `{}`}, {"GET", "remove", ""}, {"POST", "status", `{}`}, {"GET", "unknown", ""}} {
		w.call(op.method, path+op.op+"?profile_id=home", op.body, true)
	}
	w.call("GET", path+"status", "", true)
	w.call("GET", path+"status?profile_id=home", "", false)
	w.call("GET", "/api/agents/missing/linux-gateway/status?profile_id=home", "", true)
	for _, raw := range []string{`{`, `[]`, `null`, strings.Repeat(" ", 4097), `{"interface":"lo","address":"192.0.2.2","lan_cidr":"192.0.2.0/24","upstream_gateway":"192.0.2.1"}`} {
		w.call("POST", path+"preview?profile_id=home", raw, true)
	}
	for _, phase := range []string{"filter", "record", "kept", "notjson", "array", "big", "401", "404", "409", "500"} {
		w.fake.mu.Lock()
		w.fake.phase = phase
		w.fake.mu.Unlock()
		w.call("GET", path+"status?profile_id=home", "", true)
	}
	w.fake.mu.Lock()
	w.fake.phase = ""
	w.fake.mu.Unlock()
	w.call("GET", "/api/agents/gateway-forwarding", "", true)
	w.call("POST", "/api/agents/gateway-forwarding", `{}`, true)
	w.call("POST", "/api/agents/gateway-forwarding", `{"agent_id":"gateway"}`, true)
	// Cross-profile link authorizes persistence without rebinding the device.
	w.call("EDIT", "default", `{"routers":{"items":[{"id":"router","name":"Main","proxy_gateway_agent_id":"gateway"}]}}`, true)
	w.call("GET", "/api/agents/gateway-forwarding", "", true)
	w.call("POST", "/api/agents/gateway-forwarding", `{"agent_id":"gateway"}`, true)
}

func TestA20LegacyEditorRoundTrip(t *testing.T) {
	w := newWorld(t)
	root := "/api/custom-config/linux-gateway"
	query := "?profile_id=home"
	rev, _ := w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
	body := fmt.Sprintf(`{"settings":{"enabled":false,"rule_set_ids":[]},"expected_config_revision":%d}`, rev)
	g, p := w.call("POST", root+"/preview"+query, body, true)
	w.callBodies("POST", root+query, strings.TrimSuffix(body, "}")+`,"preview_token":"`+g.Body.(map[string]any)["preview_token"].(string)+`"}`, strings.TrimSuffix(body, "}")+`,"preview_token":"`+p.Body.(map[string]any)["preview_token"].(string)+`"}`, true)
	rev, _ = w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
	g, p = w.call("POST", root+"/restore/preview"+query, fmt.Sprintf(`{"expected_config_revision":%d}`, rev), true)
	w.callBodies("POST", root+"/restore"+query, fmt.Sprintf(`{"expected_config_revision":%d,"preview_token":%q}`, rev, g.Body.(map[string]any)["preview_token"]), fmt.Sprintf(`{"expected_config_revision":%d,"preview_token":%q}`, rev, p.Body.(map[string]any)["preview_token"]), true)
	w.call("GENERATE", "home", "", true)
}

func TestA20SplitHTTPGuards(t *testing.T) {
	for _, mode := range []string{"full_tun", "invalid", "legacy", "legacy_full_tun"} {
		t.Run(mode, func(t *testing.T) {
			w := newWorld(t)
			profile, err := w.srv.Repo.Profile(w.srv.Context(), "home")
			if err != nil {
				t.Fatal(err)
			}
			rawMihomo, err := json.Marshal(profile.Mihomo)
			if err != nil {
				t.Fatal(err)
			}
			var mihomo map[string]any
			if err := json.Unmarshal(rawMihomo, &mihomo); err != nil {
				t.Fatal(err)
			}
			base := profile.Mihomo.CustomConfig
			switch mode {
			case "full_tun":
				text, err := routersplit.FullTunYAML(base)
				if err != nil {
					t.Fatal(err)
				}
				mihomo["custom_config"] = text
				mihomo["router_split"] = map[string]any{"version": 1, "takeover": "full_tun", "route_rule_set_ids": []any{"r"}, "origin": "setup"}
				mihomo["linux_gateway"] = map[string]any{"enabled": false, "rule_set_ids": []any{}}
			case "invalid":
				mihomo["router_split"] = map[string]any{"version": 2}
			case "legacy_full_tun":
				text, err := routersplit.FullTunYAML(base)
				if err != nil {
					t.Fatal(err)
				}
				mihomo["custom_config"] = text
			}
			updates := map[string]any{"mihomo": mihomo}
			if strings.HasPrefix(mode, "legacy") {
				updates["routers"] = map[string]any{"items": []any{map[string]any{"id": "r1", "name": "Main", "proxy_gateway_agent_id": "gateway"}}}
			}
			body, err := json.Marshal(updates)
			if err != nil {
				t.Fatal(err)
			}
			w.call("EDIT", "home", string(body), true)
			root := "/api/profiles/home/custom-config/linux-gateway"
			w.call("GET", root, "", true)
			rev, _ := w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
			for _, enabled := range []bool{false, true} {
				w.call("POST", root, fmt.Sprintf(`{"enabled":%t,"rule_set_ids":[]}`, enabled), true)
				rev, _ = w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
				w.call("POST", root+"/preview", fmt.Sprintf(`{"settings":{"enabled":%t,"rule_set_ids":[]},"expected_config_revision":%d}`, enabled, rev), true)
			}
			host := "/api/agents/gateway/linux-gateway/"
			for _, phase := range []string{"record", "kept", ""} {
				w.fake.mu.Lock()
				w.fake.phase = phase
				w.fake.mu.Unlock()
				w.call("POST", host+"apply?profile_id=home", spec, true)
				w.call("POST", host+"remove?profile_id=home", `{}`, true)
				w.call("POST", host+"remove?profile_id=home", `{"confirm_router_split_teardown":true}`, true)
				w.call("POST", host+"forwarding?profile_id=home", `{"prepare_tun":true}`, true)
			}
		})
	}
}

func TestA20ForwardingStatesAndVersions(t *testing.T) {
	w := newWorld(t)
	w.call("EDIT", "default", `{"routers":{"items":[{"id":"router","name":"Main","proxy_gateway_agent_id":"gateway"}]}}`, true)
	for _, phase := range []string{"", "needs", "unsupported", "system", "foreign", "hook", "standalone", "old", "404", "401", "409", "500"} {
		w.fake.mu.Lock()
		w.fake.phase = phase
		w.fake.mu.Unlock()
		w.call("GET", "/api/agents/gateway-forwarding", "", true)
	}
	w.fake.mu.Lock()
	w.fake.phase = ""
	w.fake.mu.Unlock()
	for _, update := range []string{`{"version":"1.8.24-go"}`, `{"version":"1.8.25-go"}`, `{"version":"1.8.39-go"}`, `{"version":"1.8.40-go"}`, `{"version":"1.8.41-go"}`, `{"version":"bad"}`, `{"version":null}`, `{"version":"1.8.43-go","deployment_method":"docker"}`, `{"deployment_method":"shell","service_type":"oxidns"}`, `{"service_type":"mihomo","profile_id":"default"}`} {
		w.call("AGENT", "gateway", update, true)
		w.call("GET", "/api/agents/gateway/linux-gateway/status?profile_id=home", "", true)
		w.call("GET", "/api/agents/gateway-forwarding", "", true)
		w.call("POST", "/api/agents/gateway-forwarding", `{"agent_id":"gateway"}`, true)
	}
}

func TestA20RealWizardFixtures(t *testing.T) {
	for _, goal := range []string{"router", "gateway"} {
		t.Run(goal, func(t *testing.T) {
			w := newWorld(t, `{"id":"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa","profile_id":"home","goal":"`+goal+`","completed":true,"updated_at":"2026-01-01"}`, `{"id":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb","profile_id":"home","goal":"router","abandoned":true,"updated_at":"2026-02-01"}`)
			w.call("GET", "/api/profiles/home/custom-config/linux-gateway", "", true)
			w.call("POST", "/api/profiles/home/custom-config/linux-gateway", `{"enabled":false}`, true)
			w.call("POST", "/api/agents/gateway/linux-gateway/remove?profile_id=home", `{}`, true)
		})
	}
}

func TestA20NormalizationKeepsUserFields(t *testing.T) {
	input := map[string]any{"document": map[string]any{"profiles": map[string]any{"home": map[string]any{"updated_at": "2026-01-01T00:00:00Z", "future": map[string]any{"created_at": "user-created", "updated_at": "user-updated", "preview_token": "user-token", "id": "user-id"}, "mihomo": map[string]any{"linux_gateway_restore_point": map[string]any{"version": 1, "id": strings.Repeat("a", 32), "created_at": "2026-01-01T00:00:00Z", "mihomo": map[string]any{}, "oxidns": map[string]any{}}}}}}}
	output := normalize(t, jsonValue(t, input), "state").(map[string]any)
	profile := output["document"].(map[string]any)["profiles"].(map[string]any)["home"].(map[string]any)
	if profile["updated_at"] != "<time>" {
		t.Fatal("metadata not normalized")
	}
	future := profile["future"].(map[string]any)
	if future["created_at"] != "user-created" || future["updated_at"] != "user-updated" || future["preview_token"] != "user-token" || future["id"] != "user-id" {
		t.Fatal("user fields ignored", future)
	}
	userBody := map[string]any{"preview_token": "user-token", "restore_point": map[string]any{"id": strings.Repeat("a", 32), "created_at": "user-created"}, "changes": []any{"user-change"}}
	if normalized := normalize(t, jsonValue(t, userBody), "body"); !reflect.DeepEqual(normalized, jsonValue(t, userBody)) {
		t.Fatal("unrelated response fields ignored", normalized)
	}
	if isDatabaseFile("configflow.sqlite3-extra") {
		t.Fatal("non-database file excluded")
	}
}

func TestA20CapturedProfileHeaderSemantics(t *testing.T) {
	w := newWorld(t)
	path := "/api/agents/gateway/linux-gateway/status?profile_id=default"
	for _, headers := range [][]string{{"home"}, {"home", "default"}, {"home", ""}} {
		w.profileHeaders = headers
		w.call("GET", path, "", true)
	}
}

func displayValue(t *testing.T, value any) any {
	t.Helper()
	text, ok := value.(string)
	if !ok {
		t.Fatalf("display not text: %T", value)
	}
	node, err := yamlx.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := yamlx.ToJSON(node)
	if err != nil {
		t.Fatal(err)
	}
	var result any
	if err = json.Unmarshal(raw, &result); err != nil {
		t.Fatal(err)
	}
	return result
}

// These exact Python leaks are independently asserted, then replaced by
// the required safe value. No other response member or storage field is ignored.
func assertPrivacyCorrection(t *testing.T, goBody, pyBody any) {
	t.Helper()
	expectedPy := map[string]any{
		"OxiDNS 草稿.note":                   "previous synthetic-old-password; current synthetic-new-password; encoded synthetic%2Dnew%2Dpassword",
		"OxiDNS 草稿.future":                 map[string]any{"password": "[已隐藏]", "note": "reference synthetic-nested-password", "synthetic-old-password": "[已隐藏]"},
		"OxiDNS 草稿.synthetic-new-password": "[已隐藏]",
	}
	expectedGo := map[string]any{"OxiDNS 草稿.note": "[REDACTED]", "OxiDNS 草稿.future": map[string]any{"password": "[已隐藏]", "note": "[REDACTED]", "[REDACTED]": "[已隐藏]"}, "OxiDNS 草稿.synthetic-new-password": "[已隐藏]"}
	goChanges := map[string]map[string]any{}
	for _, raw := range goBody.(map[string]any)["changes"].([]any) {
		c := raw.(map[string]any)
		goChanges[c["path"].(string)] = c
	}
	count := 0
	for _, raw := range pyBody.(map[string]any)["changes"].([]any) {
		c := raw.(map[string]any)
		path := c["path"].(string)
		unsafe, found := expectedPy[path]
		if !found {
			continue
		}
		if !reflect.DeepEqual(displayValue(t, c["before"]), unsafe) {
			t.Fatal("Python credential leak changed", path, c["before"])
		}
		publicPath := path
		if path == "OxiDNS 草稿.synthetic-new-password" {
			publicPath = "[REDACTED]"
		}
		goChange := goChanges[publicPath]
		if goChange == nil || !reflect.DeepEqual(displayValue(t, goChange["before"]), expectedGo[path]) {
			t.Fatal("Go did not mask exact credential reference", path, goChange)
		}
		replacement, err := json.Marshal(expectedGo[path])
		if err != nil {
			t.Fatal(err)
		}
		c["before"] = string(replacement)
		c["path"] = publicPath
		count++
	}
	if count != 3 {
		t.Fatalf("expected three explicit Python privacy differences, got %d", count)
	}
}

func TestA20DraftPythonCredentialLeaksAreFixed(t *testing.T) {
	w := newWorld(t)
	root := "/api/profiles/home/custom-config/linux-gateway"
	w.call("EDIT", "home", `{"oxidns":{"native_config":"plugins: []\n","password":"synthetic-old-password","note":"original"}}`, true)
	w.call("POST", root, `{"enabled":false,"rule_set_ids":[]}`, true)
	w.call("EDIT", "home", `{"oxidns":{"native_config":"plugins: []\n","password":"synthetic-new-password","note":"previous synthetic-old-password; current synthetic-new-password; encoded synthetic%2Dnew%2Dpassword","future":{"password":"synthetic-nested-password","note":"reference synthetic-nested-password","synthetic-old-password":"keyed metadata"},"synthetic-new-password":"root metadata"}}`, true)
	rev, _ := w.srv.Repo.ConfigRevision(w.srv.Context(), "home")
	w.privacyCorrection = true
	w.call("POST", root+"/restore/preview", fmt.Sprintf(`{"expected_config_revision":%d}`, rev), true)
}

// maskSystemRevision hides the system _revision (document and row): since
// O17 an edit of Agent state only (the AGENT steps change versions) leaves
// it, where Python counted every registry write.
func maskSystemRevision(state any) {
	root, _ := state.(map[string]any)
	if document, ok := root["document"].(map[string]any); ok {
		if system, ok := document["system"].(map[string]any); ok {
			if _, present := system["_revision"]; present {
				system["_revision"] = "@system-revision"
			}
		}
	}
	fields, _ := root["fields"].([]any)
	for _, field := range fields {
		if row, ok := field.([]any); ok && len(row) >= 4 && row[0] == "system" && row[2] == "_revision" {
			row[len(row)-1] = "@system-revision"
		}
	}
}
