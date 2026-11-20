// Package difftest exercises C33 through Flask and the complete Go HTTP pipeline.
package difftest

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"configflow/internal/api/proxygroups"
	"configflow/internal/api/rulelibrary"
	apirules "configflow/internal/api/rules"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lockx"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/oxidns"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/routers"
	shared "configflow/internal/rules"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/testjson"
	"configflow/internal/yamlx"
)

const secret = "c33-synthetic-secret"

type answer struct {
	Status     int               `json:"status"`
	Headers    map[string]string `json:"headers"`
	Body       any               `json:"body"`
	State      any               `json:"state"`
	Error      string            `json:"oracle_error"`
	CachePaths map[string]string `json:"cache_paths"`
}
type world struct {
	t       *testing.T
	srv     *httpxtest.Server
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	stderr  bytes.Buffer
	cmd     *exec.Cmd
	token   string
	pyDir   string
	ids     map[string]string
}

func newWorld(t *testing.T, seed func(context.Context, *repo.Repository)) *world {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	w := &world{t: t, ids: map[string]string{}}
	t.Cleanup(func() {
		if count := lockx.Violations(); count != 0 {
			t.Errorf("lock order violations: %d", count)
		}
	})
	w.srv = httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret, Register: httpxtest.Routes(apirules.Register, rulelibrary.Register, proxygroups.Register)})
	notifications.SetInputs(notifications.Inputs{Agents: func(context.Context) ([]model.Agent, error) { return []model.Agent{}, nil }, Rows: func(ctx context.Context) ([]*tasks.Task, error) {
		status, err := taskstatus.New(w.srv.DataDir).Read()
		if err != nil {
			return nil, err
		}
		return refresh.RuleTasks(ctx, status)
	}})
	t.Cleanup(notifications.ResetInputs)
	oldClock := notifications.Clock
	notifications.Clock = fixedClock{}.Now
	t.Cleanup(func() { notifications.Clock = oldClock })
	env := refresh.DefaultRefreshEnv()
	env.Clock = fixedClock{}
	t.Cleanup(refresh.SetRefreshEnv(env))
	if seed != nil {
		seed(w.srv.Context(), w.srv.Repo)
	}
	pyDir := t.TempDir()
	w.pyDir = pyDir
	if out, err := exec.Command("cp", "-a", w.srv.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, out)
	}
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, _ := filepath.Abs("../../../../..")
	script, _ := filepath.Abs("testdata/reference.py")
	w.cmd = exec.Command(python, script)
	w.cmd.Dir = root
	w.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+pyDir, "ADMIN_USERNAME=diff-admin", "ADMIN_PASSWORD=diff-password", "JWT_SECRET_KEY="+secret, "LOG_FILE="+filepath.Join(pyDir, "app.log"), "PYTHONDONTWRITEBYTECODE=1")
	var err error
	w.stdin, err = w.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := w.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	w.scanner = bufio.NewScanner(stdout)
	w.scanner.Buffer(make([]byte, 65536), 64<<20)
	w.cmd.Stderr = &w.stderr
	if err = w.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		w.stdin.Close()
		w.cmd.Process.Kill()
		w.cmd.Wait()
		if t.Failed() {
			t.Log(w.stderr.String())
		}
	})
	token, err := auth.EncodeHS256(map[string]any{"username": "diff-admin", "exp": time.Now().Add(time.Hour).Unix()}, secret)
	if err != nil {
		t.Fatal(err)
	}
	w.token = token
	return w
}
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
	doc, err := s.Repo.ExportAllJSON(s.Context(), false)
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
		if ext != ".json" && ext != ".yaml" && ext != ".list" {
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
		case ".yaml":
			err = yamlx.Unmarshal(data, &value)
		default:
			value = string(data)
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

var generatedID = regexp.MustCompile(`^(?:rule|ruleset|lib)_[0-9a-f]{12}$`)
var draftRevision = regexp.MustCompile(`^[0-9a-f]{64}$`)

func validDraftRevision(value any) bool {
	token, ok := value.(string)
	return ok && draftRevision.MatchString(token)
}

func (w *world) normalize(v any) any {
	switch value := v.(type) {
	case string:
		return w.normalizeString(value)
	case []any:
		if len(value) == 5 && value[2] == "mihomo" {
			value[4] = normalizeMihomo(w.t, value[4])
		}
		if len(value) == 5 && (value[2] == "updated_at" || value[2] == "created_at") {
			if text, ok := value[4].(string); ok {
				if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
					value[4] = "<time>"
				}
			}
		}
		for i, item := range value {
			value[i] = w.normalize(item)
		}
	case map[string]any:
		for key, item := range value {
			if key == "mihomo" {
				item = normalizeMihomo(w.t, item)
				value[key] = item
			}
			if replacement := w.normalizeString(key); replacement != key {
				delete(value, key)
				key = replacement
				value[key] = item
			}
			if key == "native_config" {
				if text, ok := item.(string); ok && strings.TrimSpace(text) != "" {
					var document any
					if yamlx.Unmarshal([]byte(text), &document) == nil {
						value[key] = w.normalize(jsonValue(w.t, document))
						continue
					}
				}
			}
			if key == "elapsed_time" {
				if _, ok := item.(float64); !ok {
					w.t.Errorf("elapsed_time is not numeric: %T", item)
				}
				value[key] = "<elapsed>"
				continue
			}
			if key == "updated_at" || key == "created_at" || key == "last_run" || key == "last_success" || key == "next_run" {
				if text, ok := item.(string); ok {
					if _, err := time.Parse(time.RFC3339Nano, text); err == nil {
						value[key] = "<time>"
						continue
					}
				}
			}
			value[key] = w.normalize(item)
		}
	}
	return v
}

// normalizeMihomo compares the user YAML stored in a known Mihomo setting.
// This is D1 formatting only; comment survival has separate route regressions.
func normalizeMihomo(t *testing.T, value any) any {
	t.Helper()
	settings, ok := value.(map[string]any)
	if !ok {
		return value
	}
	if content, ok := settings["custom_config"].(string); ok && content != "" {
		var document any
		if yamlx.Unmarshal([]byte(content), &document) == nil {
			settings["custom_config"] = jsonValue(t, document)
		}
	}
	return value
}
func (w *world) pairIDs(a, b any) {
	// Pair generated IDs by their response positions, preserving one-to-one
	// identity for every later field, reference, key and persisted document.
	aa, aok := a.(map[string]any)
	bb, bok := b.(map[string]any)
	if aok && bok {
		if x, xok := aa["id"].(string); xok && generatedID.MatchString(x) {
			y, yok := bb["id"].(string)
			if !yok || !generatedID.MatchString(y) {
				w.t.Errorf("generated ID mismatch %q %v", x, bb["id"])
			} else {
				replacement := w.ids[x]
				if replacement == "" {
					replacement = fmt.Sprintf("<id-%d>", len(w.ids))
					w.ids[x] = replacement
				}
				if old, ok := w.ids[y]; ok && old != replacement {
					w.t.Error("generated IDs are not bijective")
				}
				w.ids[y] = replacement
				if aa["source_type"] == "url" {
					for _, row := range []map[string]any{aa, bb} {
						w.ids["shared/rules/"+shared.CacheFilename(testjson.Decode[model.RuleConfig](w.t, row))] = replacement + ".list"
					}
				}

				w.ids[oxidns.TagFor("rule", oxidns.S(x))] = replacement + "-tag"
				w.ids[oxidns.TagFor("rule", oxidns.S(y))] = replacement + "-tag"
				w.ids[fmt.Sprintf("shared/rules/by-id/%x.list", sha256.Sum256([]byte(x)))] = replacement + ".list"
				w.ids[fmt.Sprintf("shared/rules/by-id/%x.list", sha256.Sum256([]byte(y)))] = replacement + ".list"
			}
		}
		for k, x := range aa {
			w.pairIDs(x, bb[k])
		}
	}
	ax, aok := a.([]any)
	bx, bok := b.([]any)
	if aok && bok {
		for i := 0; i < len(ax) && i < len(bx); i++ {
			w.pairIDs(ax[i], bx[i])
		}
	}
}
func (w *world) call(method, path, body string, authenticated bool) answer {
	return w.callWithBody(method, path, body, body, authenticated)
}

func (w *world) callWithBody(method, path, body, goBody string, authenticated bool) answer {
	headers := map[string]string{}
	if authenticated {
		headers["Authorization"] = "Bearer " + w.token
	}
	return w.callHeaders(method, path, body, goBody, headers)
}

func (w *world) callHeaders(method, path, body, goBody string, headers map[string]string) answer {
	t := w.t
	t.Helper()
	options := []httpxtest.Option{}
	for key, value := range headers {
		options = append(options, httpxtest.Header(key, value))
	}
	var payload *string
	if body != "" {
		payload = &body
		contentType := headers["Content-Type"]
		if contentType == "" {
			contentType = "application/json"
		}
		options = append(options, httpxtest.Body([]byte(goBody), contentType))
	}
	request := struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Body    *string           `json:"body"`
		Headers map[string]string `json:"headers"`
	}{method, path, payload, headers}
	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = w.stdin.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if !w.scanner.Scan() {
		t.Fatalf("oracle ended: %v %s", w.scanner.Err(), w.stderr.String())
	}
	var py answer
	if err = json.Unmarshal(w.scanner.Bytes(), &py); err != nil {
		t.Fatalf("oracle JSON %v %s", err, w.scanner.Text())
	}
	if py.Error != "" {
		t.Fatal(py.Error)
	}
	var got *httpxtest.Result
	if method == "TWIN_DRAFT" {
		statuses := make([]int, 2)
		var workers sync.WaitGroup
		for i := range statuses {
			workers.Go(func() { statuses[i] = w.srv.Do("PUT", path, options...).Status })
		}
		workers.Wait()
		sort.Ints(statuses)
		data, err := json.Marshal(map[string]any{"statuses": statuses})
		if err != nil {
			t.Fatal(err)
		}
		got = &httpxtest.Result{Status: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: data}
	} else if method == "DESIRED" {
		var request struct {
			Gateway string   `json:"gateway"`
			IDs     []string `json:"rule_ids"`
		}
		if err := json.Unmarshal([]byte(body), &request); err != nil {
			t.Fatal(err)
		}
		config, err := w.srv.Repo.Config(w.srv.Context(), path, repo.ResourceRoots{})
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(config)
		if err != nil {
			t.Fatal(err)
		}
		result, err := routers.ComputeDesired(w.srv.Context(), raw, request.Gateway, request.IDs, nil)
		if err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		got = &httpxtest.Result{Status: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: data}
	} else {
		got = w.srv.Do(method, path, options...)
	}
	var actual any
	if method == "HEAD" {
		actual = ""
	} else if strings.Contains(got.Header.Get("Content-Type"), "json") {
		if err = json.Unmarshal(got.Body, &actual); err != nil {
			t.Fatalf("Go JSON %v %s", err, got.Body)
		}
	} else {
		actual = string(got.Body)
	}
	if got.Status != py.Status {
		t.Errorf("%s %s status Python=%d Go=%d Python=%v Go=%s", method, path, py.Status, got.Status, py.Body, got.Body)
	}
	w.pairIDs(py.Body, actual)
	compare := func(label string, a, b any) {
		t.Helper()
		a = w.normalize(jsonValue(t, a))
		b = w.normalize(jsonValue(t, b))
		if label == "body" && (method == "GET" || method == "PUT") && strings.HasSuffix(path, "/draft-content") && py.Status == 200 && got.Status == 200 {
			// D2 changes only the ephemeral revision. Separate route tests prove
			// each side is stable, invalidates old tokens, and rejects races.
			for _, v := range []any{a, b} {
				obj, ok := v.(map[string]any)
				if !ok {
					t.Fatal("draft returned no object")
				}
				if !validDraftRevision(obj["revision"]) {
					t.Fatal("invalid draft revision", obj["revision"])
				}
				obj["revision"] = "<native editor revision>"
			}
		}
		if label == "body" && (path == "/api/rule-library/test" || path == "/api/rule-library/cache") {
			// Python returns futures in completion order. Only this result array
			// has no ordering contract; every other array stays ordered.
			for _, v := range []any{a, b} {
				if obj, ok := v.(map[string]any); ok {
					if results, ok := obj["results"].([]any); ok {
						sort.Slice(results, func(i, j int) bool {
							left, _ := json.Marshal(results[i].(map[string]any)["id"])
							right, _ := json.Marshal(results[j].(map[string]any)["id"])
							return string(left) < string(right)
						})
					}
				}
			}
		}
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s %s %s: %s", method, path, label, firstDifference(a, b, ""))
		}
	}
	pyBytes, _ := json.Marshal(py.Body)
	if text, ok := py.Body.(string); ok {
		pyBytes = []byte(text)
	}
	migrated := httpxtest.MigratedAPIError(method, py.Status, got.Status, http.Header{"Content-Type": []string{py.Headers["Content-Type"]}}, got.Header, pyBytes, got.Body)
	if !migrated {
		if text, ok := py.Body.(string); ok && strings.Contains(path, "target=mihomo") && strings.HasPrefix(strings.TrimSpace(text), "payload:") {
			var a, b any
			left := yamlx.Unmarshal([]byte(text), &a)
			right := yamlx.Unmarshal([]byte(actual.(string)), &b)
			if left != nil || right != nil {
				t.Fatalf("marked YAML delivery invalid: Python=%v Go=%v", left, right)
			}
			compare("marked YAML body", a, b)
		} else {
			// D1 permits YAML spelling changes in the checked editor text.
			// Compare only that documented YAML field structurally; the rest of
			// the response and every stored row/file remain exact comparisons.
			if path == "/api/rule-library/check-content" {
				var request map[string]any
				_ = json.Unmarshal([]byte(body), &request)
				if request["format"] == "yaml" {
					for _, response := range []any{py.Body, actual} {
						if obj, ok := response.(map[string]any); ok {
							if content, ok := obj["content"].(string); ok {
								var parsed any
								if err := yamlx.Unmarshal([]byte(content), &parsed); err != nil {
									t.Fatal(err)
								}
								obj["content"] = jsonValue(t, parsed)
							}
						}
					}
				}
			}
			// PORTING excludes internal English exception texts. Keep this
			// exception narrow to the malformed proxy-domain scalar case.
			if path == "/api/rule-library/proxy-domains" && method == "POST" && py.Status == 500 && got.Status == 500 {
				a, aok := py.Body.(map[string]any)
				b, bok := actual.(map[string]any)
				if aok && bok && a["message"] == "exception_type=AttributeError" && b["message"] == "proxy_domains must be a string" {
					a["message"], b["message"] = "<native diagnostic>", "<native diagnostic>"
				}
			}
			if path == "/api/rule-library/cache" && py.Status == 500 && got.Status == 500 {
				a, aok := py.Body.(map[string]any)
				b, bok := actual.(map[string]any)
				if aok && bok && a["message"] == "exception_type=AttributeError" && b["message"] == "invalid rule cache request object" {
					a["message"], b["message"] = "<native diagnostic>", "<native diagnostic>"
				}
			}
			if (path == "/api/rule-library/reorder" || path == "/api/proxy-groups/reorder") && py.Status == 500 && got.Status == 500 {
				a, aok := py.Body.(map[string]any)
				b, bok := actual.(map[string]any)
				var request any
				decodeErr := json.Unmarshal([]byte(body), &request)
				_, object := request.(map[string]any)
				// The shared response hook hides native English 500 diagnostics.
				// Only valid non-object reorder inputs have a different message.
				if aok && bok && decodeErr == nil && !object && b["message"] == httpx.DescInternal {
					switch a["message"] {
					case "exception_type=AttributeError", "'list' object has no attribute 'get'", "'str' object has no attribute 'get'", "'int' object has no attribute 'get'", "'bool' object has no attribute 'get'":
						a["message"], b["message"] = "<native non-object diagnostic>", "<native non-object diagnostic>"
					}
				}
			}
			if py.Status == 500 && got.Status == 500 && (path == "/api/proxy-groups/preview-regex" || path == "/api/rule-library/cache" || path == "/api/rule-library/reorder" || path == "/api/rule-library/proxy-domains") {
				a, aok := py.Body.(map[string]any)
				b, bok := actual.(map[string]any)
				if aok && bok && (b["message"] == "invalid JSON request" || b["message"] == httpx.DescInternal) {
					message, _ := a["message"].(string)
					if message == "exception_type=BadRequest" || strings.HasPrefix(message, "400 Bad Request:") ||
						message == "exception_type=UnsupportedMediaType" || message == "415 Unsupported Media Type: Did not attempt to load JSON data because the request Content-Type was not 'application/json'." {
						a["message"], b["message"] = "<native JSON diagnostic>", "<native JSON diagnostic>"
					}
				}
			}
			compare("body", py.Body, actual)
		}
	}
	for _, key := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Cache", "Warning", "X-ConfigFlow-Profile"} {
		if key == "Content-Type" && migrated {
			continue
		}
		expected := py.Headers[key]
		if key == "X-ConfigFlow-Profile" {
			expected = httpxtest.O9ExpectedProfileHeader(expected, py.Status, got.Status, pyBytes, got.Body)
		}
		if expected != got.Header.Get(key) {
			t.Errorf("header %s Python=%q Go=%q", key, expected, got.Header.Get(key))
		}
	}
	compare("state", py.State, snapshot(t, w.srv))
	t.Logf("%s %s Python=%d Go=%d", method, path, py.Status, got.Status)
	return py
}

func firstDifference(a, b any, path string) string {
	if reflect.DeepEqual(a, b) {
		return ""
	}
	if aa, ok := a.(map[string]any); ok {
		if bb, ok := b.(map[string]any); ok {
			keys := make([]string, 0, len(aa)+len(bb))
			seen := map[string]bool{}
			for key := range aa {
				seen[key] = true
				keys = append(keys, key)
			}
			for key := range bb {
				if !seen[key] {
					keys = append(keys, key)
				}
			}
			sort.Strings(keys)
			for _, key := range keys {
				av, aok := aa[key]
				bv, bok := bb[key]
				if aok != bok {
					return fmt.Sprintf("%s.%s present Python=%v Go=%v", path, key, aok, bok)
				}
				if d := firstDifference(av, bv, path+"."+key); d != "" {
					return d
				}
			}
		}
	}
	if aa, ok := a.([]any); ok {
		if bb, ok := b.([]any); ok {
			if len(aa) != len(bb) {
				return fmt.Sprintf("%s length Python=%d Go=%d", path, len(aa), len(bb))
			}
			for i := range aa {
				if d := firstDifference(aa[i], bb[i], fmt.Sprintf("%s[%d]", path, i)); d != "" {
					return d
				}
			}
		}
	}
	return fmt.Sprintf("%s Python=%v Go=%v", path, a, b)
}

type fixedClock struct{}

func (fixedClock) Now() time.Time      { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
func (fixedClock) Monotonic() float64  { return 0 }
func (fixedClock) Sleep(time.Duration) {}

func (w *world) normalizeString(value string) string {
	keys := make([]string, 0, len(w.ids))
	for key := range w.ids {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	pairs := make([]string, 0, len(keys)*2)
	for _, key := range keys {
		pairs = append(pairs, key, w.ids[key])
	}
	if len(pairs) == 0 {
		return value
	}
	return strings.NewReplacer(pairs...).Replace(value)
}
