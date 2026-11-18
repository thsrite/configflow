// Package difftest checks C29 against real Flask with complete stored-state comparison.
package difftest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	api "configflow/internal/api/oxidns"
	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

const secret = "c29-synthetic-jwt-key"

type request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    *string           `json:"body"`
	Headers map[string]string `json:"headers"`
}
type answer struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    any               `json:"body"`
	State   any               `json:"state"`
	Error   string            `json:"oracle_error"`
}
type oracle struct {
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	cmd     *exec.Cmd
	output  bytes.Buffer
}

func startOracle(t *testing.T, dir string) *oracle {
	t.Helper()
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, _ := filepath.Abs("../../../../..")
	script, _ := filepath.Abs("testdata/reference.py")
	o := &oracle{cmd: exec.Command(python, script)}
	o.cmd.Dir = root
	o.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+dir, "ADMIN_USERNAME=diff-admin", "ADMIN_PASSWORD=diff-password", "JWT_SECRET_KEY="+secret, "PYTHONDONTWRITEBYTECODE=1", "LOG_FILE="+filepath.Join(dir, "app.log"))
	var err error
	o.stdin, err = o.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := o.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	o.scanner = bufio.NewScanner(stdout)
	o.scanner.Buffer(make([]byte, 65536), 16<<20)
	o.cmd.Stderr = &o.output
	if err = o.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		o.stdin.Close()
		o.cmd.Process.Kill()
		o.cmd.Wait()
		if t.Failed() {
			t.Log(o.output.String())
		}
	})
	return o
}
func (o *oracle) send(t *testing.T, r request) answer {
	t.Helper()
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fmt.Fprintln(o.stdin, string(data)); err != nil {
		t.Fatal(err)
	}
	if !o.scanner.Scan() {
		t.Fatalf("oracle ended: %v %s", o.scanner.Err(), o.output.String())
	}
	var a answer
	if err = json.Unmarshal(o.scanner.Bytes(), &a); err != nil {
		t.Fatalf("oracle: %v %s", err, o.scanner.Text())
	}
	if a.Error != "" {
		t.Fatal(a.Error)
	}
	return a
}

type world struct {
	srv   *httpxtest.Server
	py    *oracle
	token string
	t     *testing.T
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret, Register: httpxtest.Routes(api.Register)})
	ctx := srv.Context()
	if _, err := srv.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.GetObject("system_config").Set("config_token", "c29-config-secret")
		s.GetObject("system_config").Set("rule_proxy_token", "c29-rule-secret")
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.UpdateSharedTransaction(ctx, func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("rule_library", []any{jsonx.Obj("id", "library", "name", "Synthetic", "source_type", "content", "content", "DOMAIN-SUFFIX,example.com", "behavior", "domain", "format", "text")})
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
		p.Set("rule_configs", []any{jsonx.Obj("id", "rs", "itemType", "ruleset", "library_rule_id", "library", "policy", "DIRECT", "enabled", true)})
		p.Set("oxidns", jsonx.Obj("local_dns", "192.0.2.53", "remote_dns", "192.0.2.54", "fallback_dns", "192.0.2.55", "unrelated", jsonx.Obj("empty", []any{}, "null", nil)))
		return nil, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.Repo.CloneProfile(ctx, "default", jsonx.Obj("id", "office", "name", "Office")); err != nil {
		t.Fatal(err)
	}
	// Preserve unrelated binary/JSON/YAML files as well as all database members.
	if err := os.WriteFile(filepath.Join(srv.DataDir, "unrelated.bin"), []byte{0, 1, 255}, 0600); err != nil {
		t.Fatal(err)
	}
	pyDir := t.TempDir()
	if out, err := exec.Command("cp", "-a", srv.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, out)
	}
	token, err := auth.EncodeHS256(jsonx.Obj("username", "diff-admin", "exp", time.Now().Add(time.Hour).Unix()), secret)
	if err != nil {
		t.Fatal(err)
	}
	return &world{srv, startOracle(t, pyDir), "Bearer " + token, t}
}
func native(t *testing.T, v any) any {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	var out any
	if err = json.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func snapshot(t *testing.T, s *httpxtest.Server) any {
	t.Helper()
	doc, err := s.Repo.ExportAll(s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]any{}
	err = filepath.WalkDir(s.DataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || entry.Name() == "app.log" || entry.Name() == ".config.lock" || strings.HasPrefix(entry.Name(), "configflow.sqlite3") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any
		switch filepath.Ext(path) {
		case ".json":
			err = json.Unmarshal(data, &value)
		case ".yaml", ".yml":
			var node *yamlx.Node
			node, err = yamlx.Parse(data)
			if err == nil {
				data, err = yamlx.ToJSON(node)
			}
			if err == nil {
				err = json.Unmarshal(data, &value)
			}
		default:
			values := make([]int, len(data))
			for i, b := range data {
				values[i] = int(b)
			}
			value = values
		}
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(s.DataDir, path)
		files[filepath.ToSlash(rel)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Round-trip to unify integer types in the file projection.
	return native(t, map[string]any{"document": doc, "files": files})
}
func sendGo(t *testing.T, s *httpxtest.Server, r request) answer {
	t.Helper()
	body := ""
	if r.Body != nil {
		body = *r.Body
	}
	req := httptest.NewRequest(r.Method, "http://localhost"+r.Path, strings.NewReader(body))
	for k, v := range r.Headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	resp := rec.Result()
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var value any = string(data)
	if r.Method != "HEAD" && strings.HasPrefix(resp.Header.Get("Content-Type"), "application/json") {
		if err := json.Unmarshal(data, &value); err != nil {
			t.Fatal(err)
		}
	}
	headers := map[string]string{}
	for k, v := range resp.Header {
		headers[http.CanonicalHeaderKey(k)] = strings.Join(v, ", ")
	}
	return answer{Status: rec.Code, Body: value, Headers: headers, State: snapshot(t, s)}
}
func normalize(value any, key string) any {
	switch v := value.(type) {
	case map[string]any:
		for k, item := range v {
			if k == "updated_at" {
				if text, ok := item.(string); ok {
					if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
						v[k] = "<valid-update-time>"
						continue
					}
				}
			}
			v[k] = normalize(item, k)
		}
	case []any:
		for i, item := range v {
			v[i] = normalize(item, key)
		}
	case string:
		if key == "native_config" || key == "config" || key == "custom_config" || key == "template_base_config" || key == "legacy_native_config" || key == "local_dns" || key == "remote_dns" || key == "fallback_dns" {
			if node, err := yamlx.Parse([]byte(v)); err == nil {
				if data, err := yamlx.ToJSON(node); err == nil {
					var out any
					if json.Unmarshal(data, &out) == nil {
						return out
					}
				}
			}
		}
	}
	return value
}
func compare(t *testing.T, label string, got, want any) {
	t.Helper()
	canonical := func(value any) any {
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var copy any
		if err := json.Unmarshal(data, &copy); err != nil {
			t.Fatal(err)
		}
		return normalize(copy, "")
	}
	if !reflect.DeepEqual(canonical(got), canonical(want)) {
		a, _ := json.MarshalIndent(got, "", "  ")
		b, _ := json.MarshalIndent(want, "", "  ")
		t.Errorf("%s\nGo: %s\nPython: %s", label, a, b)
	}
}
func headers(a answer) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"Content-Type", "Cache-Control", "ETag", "Allow"} {
		for name, value := range a.Headers {
			if strings.EqualFold(name, key) {
				out[key] = value
			}
		}
		if _, ok := out[key]; !ok {
			out[key] = ""
		}
	}
	if out["Allow"] != "" {
		items := strings.Split(out["Allow"], ",")
		for i := range items {
			items[i] = strings.TrimSpace(items[i])
		}
		sort.Strings(items)
		out["Allow"] = strings.Join(items, ", ")
	}
	return out
}
func (w *world) check(r request) answer {
	w.t.Helper()
	if r.Headers == nil {
		r.Headers = map[string]string{}
	}
	if _, ok := r.Headers["Authorization"]; !ok {
		r.Headers["Authorization"] = w.token
	}
	if r.Body != nil {
		if _, ok := r.Headers["Content-Type"]; !ok {
			r.Headers["Content-Type"] = "application/json"
		}
	}
	py := w.py.send(w.t, r)
	goAnswer := sendGo(w.t, w.srv, r)
	if r.Method == "GET" && r.Path == "/api/profiles/missing/oxidns/settings" && r.Headers["Authorization"] == "" {
		// O9-L6: pin both complete boundary responses, without changing the oracle.
		compare(w.t, "Python original missing profile status", py.Status, 404)
		compare(w.t, "Python original missing profile body", py.Body, map[string]any{"success": false, "message": "Profile not found: missing"})
		py.Status, py.Body = 401, map[string]any{"success": false, "message": "请先登录后再操作。"}
	}
	compare(w.t, "status", goAnswer.Status, py.Status)
	compare(w.t, "headers", headers(goAnswer), headers(py))
	compare(w.t, "body", goAnswer.Body, py.Body)
	compare(w.t, "stored state", goAnswer.State, py.State)
	return goAnswer
}
func body(value any) *string  { data, _ := json.Marshal(value); text := string(data); return &text }
func raw(text string) *string { return &text }
func post(path string, value any) request {
	return request{Method: "POST", Path: path, Body: body(value)}
}
func draft(text string) map[string]any {
	return map[string]any{"config_mode": "native", "native_config": text, "additional_files": []any{}}
}

func TestC29FlaskRoutes(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/office/oxidns"} {
		t.Run(prefix, func(t *testing.T) {
			w.t = t
			w.check(request{Method: "GET", Path: prefix + "/settings"})
			saved := w.check(post(prefix+"/settings", draft("# retained draft\nruntime: {worker_threads: 0}\nplugins: []\n")))
			for _, action := range []string{"render", "validate", "editor", "template-preview", "common-settings", "common-settings/apply", "native-settings/apply"} {
				t.Run(action, func(t *testing.T) {
					w.t = t
					settings := draft("runtime: {worker_threads: 4}\nplugins: []\nfuture: {empty: [], nil_value: null, flag: false}\n")
					value := map[string]any{"settings": settings, "base_url": "http://config.example", "template_settings": map[string]any{"local_dns": "192.0.2.53", "remote_dns": "192.0.2.54"}}
					if action == "native-settings/apply" {
						value["native_config"] = "runtime: {worker_threads: 6}\nplugins: []\nfuture: {empty: [], nil_value: null, flag: false}\n"
					}
					if action == "common-settings/apply" {
						value["changes"] = map[string]any{"worker_threads": 6}
					}
					if result := w.check(post(prefix+"/"+action, value)); result.Status != 200 {
						t.Fatalf("successful route rejected: %#v", result.Body)
					}
					w.check(post(prefix+"/"+action, map[string]any{}))
					w.check(post(prefix+"/"+action, map[string]any{"settings": settings, "base_url": false}))
					w.check(request{Method: "POST", Path: prefix + "/" + action, Body: raw(`{"settings":`)})
					w.check(request{Method: "POST", Path: prefix + "/" + action, Body: raw(`{}`), Headers: map[string]string{"Authorization": ""}})
				})
			}
			w.t = t
			w.check(request{Method: "POST", Path: prefix + "/settings", Body: raw(`{}`), Headers: map[string]string{"If-Match": `"stale"`}})
			w.check(request{Method: "POST", Path: prefix + "/settings", Body: raw(`{}`), Headers: map[string]string{"If-Match": "W/" + headers(saved)["ETag"]}})
			w.check(request{Method: "POST", Path: prefix + "/settings", Body: raw(`{}`), Headers: map[string]string{"If-Match": "*"}})
			for _, value := range []string{`null`, `[]`, `true`, `"text"`, `{`} {
				w.check(request{Method: "POST", Path: prefix + "/settings", Body: raw(value)})
			}
			w.check(request{Method: "POST", Path: prefix + "/settings", Body: raw(`{}`), Headers: map[string]string{"Content-Type": "text/plain"}})
			w.check(request{Method: "POST", Path: prefix + "/settings", Body: raw(`{}`), Headers: map[string]string{"Authorization": ""}})
			w.check(request{Method: "GET", Path: prefix + "/settings", Headers: map[string]string{"Authorization": ""}})
			w.check(request{Method: "OPTIONS", Path: prefix + "/settings"})
		})
	}
	w.t = t
	w.check(request{Method: "GET", Path: "/api/profiles/missing/oxidns/settings"})
	w.check(post("/api/profiles/missing/oxidns/settings", map[string]any{}))
	w.check(post("/api/oxidns/native-settings/apply", map[string]any{"settings": draft("plugins: []"), "native_config": "plugins: []", "renames": []any{}}))
	w.check(post("/api/oxidns/native-settings/apply", map[string]any{"settings": draft("plugins: []"), "native_config": "plugins: []", "standard_add": false}))
}

// This is an explicit approved difference: Flask HEAD erroneously enters its
// settings save branch. Compare Go HEAD to Flask GET, then prove DB/files/revision
// are unchanged by a JSON-bearing HEAD. The Python bug runs in its private copy.
func TestC29ExpectedDifferenceHEADReadOnly(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/default/oxidns"} {
		get := w.check(request{Method: "GET", Path: prefix + "/settings"})
		r := request{Method: "HEAD", Path: prefix + "/settings", Body: raw(`{"worker_threads": 9}`), Headers: map[string]string{"Authorization": w.token, "Content-Type": "application/json"}}
		got := sendGo(t, w.srv, r)
		compare(t, "HEAD follows GET status", got.Status, get.Status)
		compare(t, "HEAD follows GET headers", headers(got), headers(get))
		compare(t, "HEAD never writes", got.State, get.State)
		if got.Body != "" {
			t.Fatalf("HEAD body: %#v", got.Body)
		}
	}
	r := request{Method: "HEAD", Path: "/api/oxidns/settings", Body: raw(`{"worker_threads": 9}`), Headers: map[string]string{"Authorization": w.token, "Content-Type": "application/json"}}
	before := w.py.send(t, request{Method: "GET", Path: r.Path, Headers: r.Headers})
	bug := w.py.send(t, r)
	if bug.Status != 200 || reflect.DeepEqual(before.State, bug.State) {
		t.Fatal("expected Python HEAD save bug was not exercised")
	}
	t.Log("expected_difference=head_read_only: Python HEAD saves; Go matches GET and preserves complete DB/files/revision")
}

func TestC29EditorWorkflowAndValidation(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/oxidns?unused=1", "/api/profiles/office/oxidns"} {
		// Both global profile selection and explicit wrappers address Office.
		path := func(action string) string {
			if strings.HasPrefix(prefix, "/api/oxidns?") {
				return "/api/oxidns/" + action + "?profile=office"
			}
			return prefix + "/" + action
		}
		template := map[string]any{"direct_rulesets": []string{"rs"}, "local_dns": "192.0.2.53", "remote_dns": "192.0.2.54", "fallback_dns": "192.0.2.55", "api": map[string]any{"enabled": true, "username": "editor", "password": "c29-rule-secret"}, "rule_update": map[string]any{"enabled": true}}
		prepared := w.check(post(path("editor"), map[string]any{"settings": template, "base_url": "http://config.example"}))
		if prepared.Status != 200 {
			t.Fatalf("editor did not prepare a draft: %#v", prepared.Body)
		}
		settings := prepared.Body.(map[string]any)["settings"]
		read := w.check(post(path("common-settings"), map[string]any{"settings": settings, "base_url": "http://config.example"}))
		if read.Status != 200 {
			t.Fatalf("common read failed: %#v", read.Body)
		}
		if read.Body.(map[string]any)["values"].(map[string]any)["api"].(map[string]any)["password"] != "c29-rule-secret" {
			t.Fatal("draft credential was redacted")
		}
		for _, change := range []map[string]any{{"worker_threads": 6}, {"api.enabled": false}, {"api.enabled": true}, {"rule_update.enabled": false}, {"rule_update.enabled": true}} {
			patched := w.check(post(path("common-settings/apply"), map[string]any{"settings": settings, "changes": change, "base_url": "http://config.example"}))
			if patched.Status != 200 {
				t.Fatal("patch failed")
			}
			settings = patched.Body.(map[string]any)["settings"]
		}
		rendered := w.check(post(path("render"), map[string]any{"settings": settings, "base_url": "http://config.example"}))
		if rendered.Status != 200 || !strings.Contains(rendered.Body.(map[string]any)["config"].(string), "c29-rule-secret") {
			t.Fatal("render redacted draft")
		}
		settings.(map[string]any)["rule_dns_follow"] = []string{"rs"}
		saved := w.check(post(path("settings"), settings))
		if saved.Status != 200 {
			t.Fatal("save failed")
		}
		w.check(request{Method: "GET", Path: path("settings")})
		moved := saved.Body.(map[string]any)["settings"].(map[string]any)
		moved["direct_rulesets"] = []string{}
		moved["proxy_rulesets"] = []string{"rs"}
		w.check(post(path("settings"), moved))
		bad := draft("# incomplete plugins remain editable\nplugins: [{tag: forward, type: forward, args: {}}]\n")
		checked := w.check(post(path("validate"), map[string]any{"settings": bad}))
		if checked.Body.(map[string]any)["valid"] != false {
			t.Fatal("incomplete draft passed publication validation")
		}
		w.check(post(path("settings"), bad))
		w.check(request{Method: "GET", Path: path("settings")})
		w.check(post(path("render"), map[string]any{"settings": bad}))
		refs := draft("plugins:\n- {tag: target, type: reverse_lookup, args: {}}\n- {tag: main, type: sequence, args: [{exec: target}]}\n")
		rejected := w.check(post(path("native-settings/apply"), map[string]any{"settings": refs, "native_config": "plugins: [{tag: main, type: sequence, args: [{exec: target}]}]"}))
		if rejected.Status != 400 || rejected.Body.(map[string]any)["references"] == nil {
			t.Fatal("native deletion lost reference locations")
		}
	}
	for _, action := range []string{"settings", "render", "validate", "editor", "template-preview", "common-settings", "common-settings/apply", "native-settings/apply"} {
		for _, rawBody := range []string{`[]`, `null`, `true`, `{"settings":false}`, `{"settings":null}`, `{"settings":{}} trailing`} {
			w.check(request{Method: "POST", Path: "/api/oxidns/" + action, Body: raw(rawBody)})
		}
	}
	// DTO fields retain whole-value last-member semantics at the binding boundary.
	w.check(request{Method: "POST", Path: "/api/oxidns/validate", Body: raw(`{"settings":{"config_mode":"invalid"},"settings":{"config_mode":"native","native_config":"plugins: []"}}`)})
}

// N8-6/§5.7 deliberately reject non-finite JSON at binding. These are named
// expected differences, derived from real Flask malformed-body controls;
// unrelated fields, headers and all persisted state remain strict.
func TestC29ExpectedDifferenceNonFinite(t *testing.T) {
	w := newWorld(t)
	for _, prefix := range []string{"/api/oxidns", "/api/profiles/office/oxidns"} {
		for _, action := range []string{"settings", "render", "validate", "editor", "template-preview", "common-settings", "common-settings/apply", "native-settings/apply"} {
			t.Run(prefix+"/"+action, func(t *testing.T) {
				path := prefix + "/" + action
				requestHeaders := map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
				malformed := w.py.send(t, request{Method: "POST", Path: path, Body: raw("{"), Headers: requestHeaders})
				if malformed.Status != 400 {
					t.Fatal("missing malformed-body control")
				}
				for _, token := range []string{"NaN", "Infinity", "-Infinity"} {
					t.Run(token, func(t *testing.T) {
						text := `{"settings":{"config_mode":"native","native_config":"plugins: []","additional_files":[]},"native_config":"plugins: []","changes":{"worker_threads":6},"template_settings":{"local_dns":"192.0.2.53","remote_dns":"192.0.2.54"},"future":` + token + `}`
						if action == "settings" {
							text = `{"worker_threads":` + token + `}`
						}
						r := request{Method: "POST", Path: path, Body: raw(text), Headers: requestHeaders}
						before := snapshot(t, w.srv)
						py := w.py.send(t, r)
						got := sendGo(t, w.srv, r)
						if action == "settings" {
							control := w.py.send(t, request{Method: "POST", Path: path, Body: raw(`{"worker_threads":1.5}`), Headers: requestHeaders})
							compare(t, "Python non-finite reaches existing integer validator", py.Status, control.Status)
							compare(t, "Python integer failure", py.Body, control.Body)
						} else if py.Status != 200 {
							t.Fatalf("Python ignored-member control rejected: %#v", py)
						}
						compare(t, "native malformed-body status", got.Status, malformed.Status)
						compare(t, "native malformed-body fields", got.Body, malformed.Body)
						compare(t, "native malformed-body headers", headers(got), headers(malformed))
						compare(t, "non-finite Go request cannot write", got.State, before)
						compare(t, "Python control cannot write", py.State, malformed.State)
					})
				}
			})
		}
	}
	t.Log("expected_difference=nonfinite_json: all 16 POST method/path pairs reject NaN/Infinity/-Infinity before validation; complete DB/files unchanged")
}
