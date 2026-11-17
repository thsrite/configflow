// Package difftest exercises C07's real Flask HTTP and complete stored state.
package difftest

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"configflow/internal/agents/metrics"
	apilogs "configflow/internal/api/logs"
	apinotifications "configflow/internal/api/notifications"
	apistats "configflow/internal/api/stats"
	apitasks "configflow/internal/api/tasks"
	"configflow/internal/auth"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/refresh"
	"configflow/internal/tasks"
)

const secret = "c07-synthetic-jwt-key"

var fixed = time.Date(2026, 11, 18, 12, 0, 0, 123456000, time.UTC)

type command struct {
	Method  string                     `json:"method"`
	Path    string                     `json:"path"`
	Body    string                     `json:"body,omitempty"`
	Headers map[string]string          `json:"headers"`
	Write   map[string]json.RawMessage `json:"write,omitempty"`
}
type answer struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers"`
	Body    json.RawMessage   `json:"body"`
	State   json.RawMessage   `json:"state"`
}
type world struct {
	s            *httpxtest.Server
	pyDir, token string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	for _, slot := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources, tasks.SlotRouters, tasks.SlotAgents, tasks.SlotDomains, tasks.SlotMetrics} {
		t.Cleanup(tasks.SetRows(slot, func(context.Context, *jsonx.Object) ([]*jsonx.Object, error) { return nil, nil }))
	}
	refresh.Wire()
	t.Cleanup(tasks.SetRows(tasks.SlotMetrics, metrics.Rows))
	t.Cleanup(tasks.SetIntervalSetter(tasks.MetricsTaskKey, metrics.SetInterval))
	for _, category := range []string{tasks.SlotSubscriptions, tasks.SlotRuleSources} {
		t.Cleanup(tasks.SetRunner(category, func(context.Context, string, string) error { return nil }))
	}
	t.Cleanup(notifications.SetSources(notifications.Sources{ListTasks: tasks.AvailableTasks, MissingTasks: tasks.Missing, Agents: func(context.Context) ([]*jsonx.Object, error) { return nil, nil }}))
	previousNow, previousClock := apistats.Now, notifications.Clock
	apistats.Now = func() time.Time { return fixed }
	notifications.Clock = func() time.Time { return fixed }
	t.Cleanup(func() { apistats.Now = previousNow; notifications.Clock = previousClock })
	s := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret, Register: httpxtest.Routes(apilogs.Register, apistats.Register, apitasks.Register, apinotifications.Register)})
	_, err := s.Repo.UpdateSharedTransaction(s.Context(), func(_ context.Context, shared *jsonx.Object) (*jsonx.Object, error) {
		shared.Set("subscriptions", []any{jsonx.Obj("id", "s", "name", "Synthetic feed", "url", "https://feed.example.com/sub", "interval", 3600, "enabled", true)})
		shared.Set("rule_library", []any{jsonx.Obj("id", "r", "name", "Synthetic rules", "source_type", "url", "url", "https://rules.example.com/list", "update_interval", 3600, "enabled", true)})
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Repo.UpdateSystemTransaction(s.Context(), func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		system.GetObject("system_config").Set("config_token", "fixture-capability")
		system.GetObject("system_config").Set("rule_proxy_token", "fixture-local-secret")
		system.Set("backup", jsonx.Obj("webdav_username", "fixture-user", "webdav_password", "fixture-password", "webdav_url", ""))
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, "task-status.json"), []byte(`{"subscription:s":{"last_status":"success","last_run_at":"2026-11-18T08:00:00Z","run_count":"3","failure_count":"1"},"rule_source:r":{"last_status":"success","last_run_at":"2026-11-18T08:00:00Z"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.DataDir, "app.log"), []byte("INFO: ready\nERROR: synthetic failure\nWARNING: last\n"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DATA_DIR", s.DataDir)
	t.Setenv("LOG_FILE", filepath.Join(s.DataDir, "app.log"))
	pyDir := t.TempDir()
	if out, err := exec.Command("cp", "-a", s.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
		t.Fatalf("copy fixture: %v %s", err, out)
	}
	token, err := auth.EncodeHS256(jsonx.Obj("username", "diff-admin", "exp", time.Now().Add(time.Hour).Unix()), secret)
	if err != nil {
		t.Fatal(err)
	}
	return &world{s, pyDir, "Bearer " + token}
}

func (w *world) commands(commands []command) []command {
	for i := range commands {
		if commands[i].Headers == nil {
			commands[i].Headers = map[string]string{"Authorization": w.token, "Content-Type": "application/json"}
		}
	}
	return commands
}

func python(t *testing.T, w *world, commands []command) []answer {
	t.Helper()
	root, _ := filepath.Abs("../../../..")
	script, _ := filepath.Abs("testdata/reference.py")
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(os.Getenv("HOME"), "cf-test/.venv/bin/python"), script)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+w.pyDir, "LOG_FILE="+filepath.Join(w.pyDir, "app.log"), "ADMIN_USERNAME=diff-admin", "ADMIN_PASSWORD=diff-password", "JWT_SECRET_KEY="+secret, "PYTHONDONTWRITEBYTECODE=1")
	data, _ := json.Marshal(commands)
	cmd.Stdin = bytes.NewReader(data)
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("Flask oracle: %v %s %s", err, diagnostics.String(), out)
	}
	var answers []answer
	if err := json.Unmarshal(out, &answers); err != nil {
		t.Fatalf("oracle output: %v %s", err, out)
	}
	if len(answers) != len(commands) {
		t.Fatal("oracle answer count", len(answers))
	}
	return answers
}

func state(t *testing.T, w *world) json.RawMessage {
	t.Helper()
	doc, err := w.s.Repo.ExportAll(w.s.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]json.RawMessage{}
	if err := filepath.WalkDir(w.s.DataDir, func(path string, e os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if e.IsDir() {
			return nil
		}
		if filepath.Ext(path) != ".json" && e.Name() != "app.log" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if e.Name() == "app.log" {
			data, err = json.Marshal(string(data))
			if err != nil {
				return err
			}
		}
		rel, _ := filepath.Rel(w.s.DataDir, path)
		files[filepath.ToSlash(rel)] = data
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(struct {
		Document *jsonx.Object              `json:"document"`
		Files    map[string]json.RawMessage `json:"files"`
	}{doc, files})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func send(t *testing.T, w *world, c command) answer {
	t.Helper()
	for name, data := range c.Write {
		if err := os.WriteFile(filepath.Join(w.s.DataDir, name), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	options := []httpxtest.Option{}
	for key, value := range c.Headers {
		options = append(options, httpxtest.Header(key, value))
	}
	if c.Body != "" {
		options = append(options, httpxtest.Body([]byte(c.Body), c.Headers["Content-Type"]))
	}
	r := w.s.Do(c.Method, c.Path, options...)
	body := json.RawMessage(r.Body)
	if c.Method == "HEAD" {
		body = []byte(`""`)
	} else if !json.Valid(body) {
		body, _ = json.Marshal(string(r.Body))
	}
	headers := map[string]string{}
	for key, values := range r.Header {
		headers[key] = strings.Join(values, ", ")
	}
	return answer{r.Status, headers, body, state(t, w)}
}

func canonical(t *testing.T, raw json.RawMessage, dir string) any {
	t.Helper()
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		t.Fatal(err, string(raw))
	}
	var normalize func(any, string) any
	normalize = func(v any, key string) any {
		switch x := v.(type) {
		case map[string]any:
			for k, item := range x {
				x[k] = normalize(item, k)
			}
		case []any:
			for i, item := range x {
				x[i] = normalize(item, key)
			}
		case string:
			if key == "updated_at" || key == "modified" || key == "created" {
				if _, err := time.Parse(time.RFC3339Nano, x); err == nil {
					return "<valid-time>"
				}
				if _, err := time.Parse("2006-01-02T15:04:05.999999", x); err == nil {
					return "<valid-local-time>"
				}
			}
			return strings.ReplaceAll(x, dir, "<DATA>")
		}
		return v
	}
	return normalize(value, "")
}

func compare(t *testing.T, w *world, c command, got, want answer, body bool) {
	t.Helper()
	if got.Status != want.Status {
		t.Fatalf("%s %s status Go=%d Python=%d; Go %s Python %s", c.Method, c.Path, got.Status, want.Status, got.Body, want.Body)
	}
	for _, key := range []string{"Content-Type", "Cache-Control"} {
		if got.Headers[key] != want.Headers[key] {
			t.Errorf("%s %s: %s Go=%q Python=%q", c.Method, c.Path, key, got.Headers[key], want.Headers[key])
		}
	}
	if body && !reflect.DeepEqual(canonical(t, got.Body, w.s.DataDir), canonical(t, want.Body, w.pyDir)) {
		t.Errorf("%s %s body\nGo %s\nPython %s", c.Method, c.Path, got.Body, want.Body)
	}
	if !reflect.DeepEqual(canonical(t, got.State, w.s.DataDir), canonical(t, want.State, w.pyDir)) {
		t.Errorf("%s %s state\nGo %s\nPython %s", c.Method, c.Path, got.State, want.State)
	}
}

func TestC07FlaskRoutesAndStoredState(t *testing.T) {
	w := newWorld(t)
	commands := []command{
		{Method: "GET", Path: "/api/tasks"},
		{Method: "GET", Path: "/api/notifications"},
		{Method: "GET", Path: "/api/stats/overview"},
		{Method: "GET", Path: "/api/stats/overview", Headers: map[string]string{"Authorization": w.token, "X-ConfigFlow-Profile": "missing"}},
		{Method: "GET", Path: "/api/logs/tail?lines=2&search=error"},
		{Method: "GET", Path: "/api/logs/info"},
		{Method: "PUT", Path: "/api/tasks/interval", Body: `{"key":"subscription:s","interval_seconds":60}`},
		{Method: "PUT", Path: "/api/tasks/interval", Body: `{"key":"rule_source:r","interval_seconds":600}`},
		{Method: "PUT", Path: "/api/tasks/interval", Body: `{"key":"metrics_history_flush","interval_seconds":600}`},
		{Method: "PUT", Path: "/api/tasks/interval", Body: `{"key":"subscription:s","interval_seconds":30}`},
		{Method: "PUT", Path: "/api/tasks/interval", Body: `{"key":"subscription:missing","interval_seconds":600}`},
		{Method: "POST", Path: "/api/tasks/run", Body: `{"key":"subscription:s"}`},
		{Method: "POST", Path: "/api/tasks/run", Body: `{"key":"rule_source:r"}`},
		{Method: "POST", Path: "/api/tasks/run", Body: `{"key":"unknown:s"}`},
		{Method: "POST", Path: "/api/notifications/read", Body: `{"versions":{}}`},
		{Method: "POST", Path: "/api/logs/clear"},
		{Method: "GET", Path: "/api/logs/tail"},
	}
	for _, path := range []string{"/api/tasks/run", "/api/notifications/read"} {
		for _, body := range []string{"{", "[]", "null", "true", `"x"`, `{"key":2}`, `{"versions":{"x":null}}`, `{"ignored":NaN}`} {
			commands = append(commands, command{Method: "POST", Path: path, Body: body})
		}
	}
	for _, path := range []string{"/api/tasks", "/api/notifications", "/api/stats/overview", "/api/logs/tail", "/api/logs/info", "/api/logs/clear", "/api/tasks/run", "/api/tasks/interval", "/api/notifications/read"} {
		method := "GET"
		if path == "/api/tasks/run" || path == "/api/notifications/read" || path == "/api/logs/clear" {
			method = "POST"
		}
		if path == "/api/tasks/interval" {
			method = "PUT"
		}
		commands = append(commands, command{Method: method, Path: path, Headers: map[string]string{}})
	}
	commands = w.commands(commands)
	answers := python(t, w, commands)
	for i, c := range commands {
		t.Run(c.Method+" "+c.Path+"/"+c.Body, func(t *testing.T) { compare(t, w, c, send(t, w, c), answers[i], true) })
	}
	t.Logf("%d real Flask requests with complete DB/JSON/log state comparisons", len(commands))
}

func TestC07ExpectedHEADReadOnlyDifference(t *testing.T) {
	for _, path := range []string{"/api/stats/overview", "/api/notifications"} {
		t.Run(path, func(t *testing.T) {
			w := newWorld(t)
			write := map[string]json.RawMessage{"task-status.json": []byte(`{"subscription:s":{"last_status":"failed","last_error":"synthetic failure","run_count":"3","failure_count":"1"}}`)}
			commands := w.commands([]command{{Method: "HEAD", Path: path, Body: `{"versions":{}}`, Write: write}})
			want := python(t, w, commands)[0]
			before := state(t, w)
			got := send(t, w, commands[0])
			if got.Status != want.Status || got.Status != 200 || string(got.Body) != `""` {
				t.Fatal(got.Status, want.Status, string(got.Body))
			}
			for _, key := range []string{"Content-Type", "Cache-Control"} {
				if got.Headers[key] != want.Headers[key] {
					t.Fatal(key)
				}
			}
			file := "notifications.json"
			if path == "/api/stats/overview" {
				file = "profiles/default/stats_history.json"
			}
			if _, err := os.Stat(filepath.Join(w.pyDir, file)); err != nil {
				t.Fatal("Python HEAD no longer demonstrates the expected write", err)
			}
			if _, err := os.Stat(filepath.Join(w.s.DataDir, file)); !os.IsNotExist(err) {
				t.Fatal("Go HEAD wrote a projection", err)
			}
			var original, after struct {
				Document json.RawMessage `json:"document"`
			}
			json.Unmarshal(before, &original)
			json.Unmarshal(got.State, &after)
			if !reflect.DeepEqual(canonical(t, original.Document, w.s.DataDir), canonical(t, after.Document, w.s.DataDir)) {
				t.Fatal("HEAD changed DB or revision")
			}
		})
	}
}

func TestC07ExpectedNonFiniteBindingDifference(t *testing.T) {
	w := newWorld(t)
	commands := w.commands([]command{
		{Method: "POST", Path: "/api/tasks/run", Body: `{"key":"subscription:s","ignored":NaN}`},
		{Method: "POST", Path: "/api/notifications/read", Body: `{"versions":{},"ignored":Infinity}`},
	})
	answers := python(t, w, commands)
	for i, c := range commands {
		got := send(t, w, c)
		expected := 202
		if i == 1 {
			expected = 200
		}
		if answers[i].Status != expected || got.Status != 400 {
			t.Fatal("native JSON finite-only difference changed", answers[i].Status, got.Status)
		}
	}
}

func TestC07NotificationReadAndCombinedRedaction(t *testing.T) {
	w := newWorld(t)
	message := "请求失败 https://fixture-user:fixture-password@download.example.com/feed?token=fixture-capability password=fixture-local-secret"
	status, err := json.Marshal(map[string]map[string]string{"subscription:s": {"last_status": "failed", "last_run_at": "2026-11-18T10:00:00Z", "run_count": "3", "failure_count": "1", "last_error": message}, "rule_source:r": {"last_status": "success", "last_run_at": "2026-11-18T08:00:00Z"}})
	if err != nil {
		t.Fatal(err)
	}
	commands := w.commands([]command{
		{Method: "GET", Path: "/api/tasks", Write: map[string]json.RawMessage{"task-status.json": status}},
		{Method: "GET", Path: "/api/notifications"},
		{Method: "GET", Path: "/api/logs/tail", Write: map[string]json.RawMessage{"app.log": []byte(`"INFO: fixture-capability fixture-local-secret fixture-password"`)}},
		{Method: "POST", Path: "/api/notifications/read", Body: `{"versions":{"task:subscription:s":"2026-11-18T12:00:00.123456+00:00"}}`},
		{Method: "GET", Path: "/api/notifications"},
		{Method: "POST", Path: "/api/notifications/read", Body: `{"versions":{"task:subscription:s":"stale"}}`},
	})
	answers := python(t, w, commands)
	for i, c := range commands {
		got := send(t, w, c)
		compare(t, w, c, got, answers[i], true)
		for _, secret := range []string{"fixture-password", "fixture-capability", "fixture-local-secret"} {
			if bytes.Contains(got.Body, []byte(secret)) {
				t.Fatal("public response leaked stored credential", c.Path, secret)
			}
		}
		if i == 1 {
			for _, secret := range []string{"fixture-user", "fixture-password", "fixture-capability", "fixture-local-secret"} {
				if bytes.Contains(got.Body, []byte(secret)) {
					t.Fatal("notification response leaked credential", secret)
				}
				data, err := os.ReadFile(filepath.Join(w.s.DataDir, "notifications.json"))
				if err != nil || bytes.Contains(data, []byte(secret)) {
					t.Fatal("notification receipt leaked credential", secret, err)
				}
			}
		}
	}
}

func TestC07ExpectedReadinessDifferenceKeepsUnseenFailures(t *testing.T) {
	w := newWorld(t)
	for _, slot := range []string{tasks.SlotRouters, tasks.SlotAgents} {
		t.Cleanup(tasks.SetRows(slot, nil))
	}
	old := []byte(`{"items":{"task:router_rule_sync:default:r":{"state":"pending","source":"task","version":"old","read":false},"task:agent_rule_sync:a":{"state":"pending","source":"task","version":"old","read":false}}}`)
	commands := w.commands([]command{
		{Method: "GET", Path: "/api/tasks", Write: map[string]json.RawMessage{"notifications.json": old}},
		{Method: "GET", Path: "/api/notifications"},
	})
	answers := python(t, w, commands)
	for i, c := range commands {
		got := send(t, w, c)
		if answers[i].Status != 200 || got.Status != 503 {
			t.Fatal("readiness difference changed", answers[i].Status, got.Status)
		}
		var body struct {
			Missing []string `json:"missing_slots"`
		}
		if err := json.Unmarshal(got.Body, &body); err != nil || !reflect.DeepEqual(body.Missing, []string{tasks.SlotRouters, tasks.SlotAgents}) {
			t.Fatal(string(got.Body), err)
		}
	}
	if err := notifications.RefreshTasks(w.s.Context(), "subscription:s"); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(w.s.DataDir, "notifications.json"))
	if err != nil || !bytes.Equal(old, after) {
		t.Fatal("partial observations deactivated unseen failures", err, string(after))
	}
}

func TestC07MalformedHistoryRetainsPythonUnavailableStatus(t *testing.T) {
	w := newWorld(t)
	commands := w.commands([]command{{Method: "GET", Path: "/api/notifications", Write: map[string]json.RawMessage{"task-status.json": []byte(`{"subscription:s":{"run_count":"bad"}}`)}}})
	want := python(t, w, commands)[0]
	got := send(t, w, commands[0])
	if want.Status != 503 {
		t.Fatal("Python malformed-history contract changed", want.Status)
	}
	compare(t, w, commands[0], got, want, true)
}
