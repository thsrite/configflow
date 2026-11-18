// Package difftest checks C34 against real Flask with complete stored-state comparison.
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
	"unicode"

	"archive/zip"
	api "configflow/internal/api/config"
	apigenerate "configflow/internal/api/generate"
	apiprofiles "configflow/internal/api/profiles"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/server"
	"configflow/internal/yamlx"
)

const secret = "c34-synthetic-jwt-key"

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
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret, Register: httpxtest.Routes(api.Register, apigenerate.Register, apiprofiles.Register), Setup: func(_ *httpx.App) { server.NewApp() }})
	ctx := srv.Context()
	if _, err := srv.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		s.GetObject("system_config").Set("config_token", "c34-config-secret")
		s.GetObject("system_config").Set("rule_proxy_token", "c34-rule-secret")
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
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "application/zip") {
		archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			t.Fatal(err)
		}
		entries := map[string]any{}
		for _, file := range archive.File {
			in, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			content, err := io.ReadAll(in)
			in.Close()
			if err != nil {
				t.Fatal(err)
			}
			entries[file.Name] = string(content)
		}
		value = entries
	}
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
			if k == "updated_at" || k == "created_at" {
				if text, ok := item.(string); ok {
					if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
						v[k] = "<valid-update-time>"
						continue
					}
				}
			}
			if strings.HasSuffix(k, ".lock") {
				delete(v, k)
				continue
			}
			if strings.HasPrefix(k, "migrations/deleted-") {
				parts := strings.Split(k, "/")
				if len(parts) > 2 {
					end := strings.LastIndex(parts[1], "-")
					if end >= 0 {
						parts[1] = parts[1][:end] + "-<id>"
						normalized := strings.Join(parts, "/")
						delete(v, k)
						v[normalized] = normalize(item, normalized)
						continue
					}
				}
			}
			if strings.HasSuffix(k, ".yaml") {
				if text, ok := item.(string); ok {
					if node, err := yamlx.Parse([]byte(text)); err == nil {
						if raw, err := yamlx.ToJSON(node); err == nil {
							var tree any
							if json.Unmarshal(raw, &tree) == nil {
								v[k] = tree
								continue
							}
						}
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
		if key == "default_config" || key == "content" || key == "body" || key == "native_config" || key == "config" || key == "custom_config" || key == "template_base_config" || key == "legacy_native_config" || key == "local_dns" || key == "remote_dns" || key == "fallback_dns" {
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
		if text, ok := copy.(string); ok {
			return normalize(text, "body")
		}
		return normalize(copy, "")
	}
	a, b := canonical(got), canonical(want)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("%s: %s", label, firstDifference(a, b, "$"))
	}

}
func headers(a answer) map[string]string {
	out := map[string]string{}
	for _, key := range []string{"Content-Type", "Content-Disposition", "Cache-Control", "Allow"} {
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
	if goAnswer.Status != py.Status {
		w.t.Logf("%s %s Go status=%d body=%v Python status=%d body=%v", r.Method, r.Path, goAnswer.Status, goAnswer.Body, py.Status, py.Body)
	}
	compare(w.t, "status", goAnswer.Status, py.Status)
	compare(w.t, "headers", headers(goAnswer), headers(py))
	internalDiagnostic := false
	if py.Status == 500 && goAnswer.Status == 500 {
		left, lok := goAnswer.Body.(map[string]any)
		right, rok := py.Body.(map[string]any)
		if lok && rok {
			message, ok := right["message"].(string)
			plain := ok
			for _, r := range message {
				if unicode.Is(unicode.Han, r) {
					plain = false
				}
			}
			if plain && left["success"] == false && right["success"] == false && len(left) == 2 && len(right) == 2 {
				if text, ok := left["message"].(string); ok && text != "" {
					internalDiagnostic = true
				}
			}
		}
	}
	if !internalDiagnostic {
		compare(w.t, "body", goAnswer.Body, py.Body)
	}
	compare(w.t, "stored state", goAnswer.State, py.State)
	return goAnswer
}
func body(value any) *string  { data, _ := json.Marshal(value); text := string(data); return &text }
func raw(text string) *string { return &text }
func post(path string, value any) request {
	return request{Method: "POST", Path: path, Body: body(value)}
}

func firstDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if left, ok := a.(map[string]any); ok {
		if right, ok := b.(map[string]any); ok {
			keys := make([]string, 0, len(left)+len(right))
			for k := range left {
				keys = append(keys, k)
			}
			for k := range right {
				if _, ok := left[k]; !ok {
					keys = append(keys, k)
				}
			}
			sort.Strings(keys)
			for _, k := range keys {
				l, lok := left[k]
				r, rok := right[k]
				if lok != rok {
					return fmt.Sprintf("%s.%s present Go=%v Python=%v", path, k, lok, rok)
				}
				if diff := firstDifference(l, r, path+"."+k); diff != "" {
					return diff
				}
			}
		}
	}
	if left, ok := a.([]any); ok {
		if right, ok := b.([]any); ok {
			if len(left) != len(right) {
				return fmt.Sprintf("%s lengths %d != %d", path, len(left), len(right))
			}
			for i := range left {
				if diff := firstDifference(left[i], right[i], fmt.Sprintf("%s[%d]", path, i)); diff != "" {
					return diff
				}
			}
		}
	}
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	if len(left) > 220 {
		left = left[:220]
	}
	if len(right) > 220 {
		right = right[:220]
	}
	return fmt.Sprintf("%s Go=%s Python=%s", path, left, right)
}
