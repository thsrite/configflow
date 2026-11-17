// Package difftest compares C31 HTTP responses and persisted state with Flask.
package difftest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	generation "configflow/internal/aggregations"
	"configflow/internal/api/aggregations"
	"configflow/internal/auth"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/jsonx"
	"configflow/internal/notifications"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"configflow/internal/substore"
	"configflow/internal/taskstatus"
	"configflow/internal/yamlx"
)

const secret = "c31-synthetic-jwt-secret"

type control struct {
	Sources      map[string]string `json:"sources"`
	CorruptCache bool              `json:"corrupt_cache"`
	Offline      bool              `json:"offline"`
	URIChain     bool              `json:"uri_chain"`
	Mutate       bool              `json:"mutate"`
}
type fakeHTTP struct {
	control *control
	before  func()
}

func (f fakeHTTP) Do(_ context.Context, req *substore.Request) (*substore.Response, error) {
	text := "proxies: []"
	source := false
	if strings.HasPrefix(req.URL, "https://feed.example/") {
		id := strings.TrimPrefix(req.URL, "https://feed.example/")
		text = f.control.Sources[id]
		source = true
	}
	for _, param := range req.Params {
		if param.Key == "content" {
			if param.Value == "demo" || param.Value == "opaque://exit" {
				text = "proxies: [{name: Converted, type: http, server: example.com, port: 80"
				if param.Value == "opaque://exit" && f.control.URIChain {
					text += ", dialer-proxy: relay"
				}
				text += "}]"
			}
		}
	}
	if source && f.before != nil {
		f.before()
	}
	if f.control.Offline && (source || strings.Contains(req.URL, "/download/_cf_tmp_s")) {
		return nil, fmt.Errorf("synthetic offline")
	}
	return &substore.Response{StatusCode: 200, URL: req.URL, Header: http.Header{"Content-Type": []string{"text/yaml"}, "X-Powered-By": []string{"sub-store"}}, Body: []byte(text)}, nil
}

type fixedClock struct{}

func (fixedClock) Now() time.Time      { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
func (fixedClock) Monotonic() float64  { return 0 }
func (fixedClock) Sleep(time.Duration) {}

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
	o.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+dir, "ADMIN_USERNAME=diff-admin", "ADMIN_PASSWORD=diff-password", "JWT_SECRET_KEY="+secret, "PYTHONDONTWRITEBYTECODE=1", "SUB_STORE_BUILTIN_DIR=/nonexistent", "LOG_FILE="+filepath.Join(dir, "app.log"))
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
	o.scanner.Buffer(make([]byte, 65536), 8<<20)
	o.cmd.Stderr = &o.output
	if err := o.cmd.Start(); err != nil {
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

type request struct {
	Method  string            `json:"method"`
	Path    string            `json:"path"`
	Body    *string           `json:"body"`
	Headers map[string]string `json:"headers"`
	Control control           `json:"control"`
}
type answer struct {
	Comments []string          `json:"comments"`
	Status   int               `json:"status"`
	Headers  map[string]string `json:"headers"`
	Body     any               `json:"body"`
	State    any               `json:"state"`
	Error    string            `json:"oracle_error"`
}
type world struct {
	srv          *httpxtest.Server
	py           *oracle
	control      *control
	token        string
	t            *testing.T
	extraHeaders map[string]string
}

func node(id string) *jsonx.Object {
	return jsonx.Obj("id", id, "name", id, "type", "http", "server", "example.com", "port", 80, "enabled", true)
}
func newWorld(t *testing.T, setup func(context.Context, *repo.Repository), sources map[string]string) *world {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	t.Cleanup(provider.SetLocations(deploy.Locations{}))
	generation.Wire()
	mihomoenv.Wire()
	previousClock := notifications.Clock
	notifications.Clock = fixedClock{}.Now
	t.Cleanup(func() { notifications.Clock = previousClock })
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret, Register: httpxtest.Routes(aggregations.Register)})
	ctx := srv.Context()
	// The isolated app projects only the task families seeded by this fixture.
	// Other modules have no resources in this synthetic repository.
	notifications.SetSources(notifications.Sources{
		Agents: func(context.Context) ([]*jsonx.Object, error) { return []*jsonx.Object{}, nil },
		ListTasks: func(ctx context.Context) ([]*jsonx.Object, error) {
			status, err := taskstatus.New(srv.DataDir).Load()
			if err != nil {
				return nil, err
			}
			subs, err := refresh.SubscriptionRows(ctx, status)
			if err != nil {
				return nil, err
			}
			rules, err := refresh.RuleRows(ctx, status)
			return append(subs, rules...), err
		},
	})
	t.Cleanup(notifications.ResetSources)
	_, err := srv.Repo.UpdateSystemTransaction(ctx, func(_ context.Context, s *jsonx.Object) (*jsonx.Object, error) {
		settings := s.GetObject("system_config")
		settings.Set("config_token", "c31-pull")
		settings.Set("rule_proxy_token", "c31-internal")
		settings.Set("retired_rule_proxy_tokens", []any{"c31-retired"})
		settings.Set("server_domain", "http://127.0.0.1:5001")
		return nil, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if setup != nil {
		setup(ctx, srv.Repo)
	}
	pyDir := t.TempDir()
	if out, err := exec.Command("cp", "-a", srv.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, out)
	}
	c := &control{Sources: sources}
	env := substore.NewEnv()
	mutated := false
	env.HTTP = fakeHTTP{control: c, before: func() {
		if !c.Mutate || mutated {
			return
		}
		mutated = true
		_, _, err := srv.Repo.UpdateProfileTransaction(ctx, "default", func(_ context.Context, p *jsonx.Object) (*jsonx.Object, error) {
			g := p.GetList("proxy_groups")[0].(*jsonx.Object)
			g.Set("name", "Changed")
			g.Set("manual_nodes", []any{"exit"})
			return nil, nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}}
	env.Clock = fixedClock{}
	env.LookupEnv = func(key string) (string, bool) {
		if key == "SUB_STORE_URL" {
			return "http://127.0.0.1:9", true
		}
		if key == "SUB_STORE_BUILTIN_DIR" {
			return "/nonexistent", true
		}
		return "", false
	}
	env.RandomHex = func() string { return "00000001" }
	service := substore.NewService(env)
	t.Cleanup(substore.SetDefault(service))
	re := refresh.DefaultEnv()
	re.Clock = fixedClock{}
	re.HTTP = env.HTTP
	re.RandomHex = env.RandomHex
	t.Cleanup(refresh.SetEnv(re))
	t.Cleanup(provider.SetEnv(&provider.Env{NodeID: func() string { return "node_00000001" }, AggregationFetch: func(_ context.Context, id, url string) (*substore.SubscriptionResult, error) {
		if c.Offline {
			return nil, fmt.Errorf("synthetic offline")
		}
		return &substore.SubscriptionResult{Text: c.Sources[id], Source: "rendered_yaml", FetchedAt: "2026-01-01T00:00:00+00:00"}, nil
	}}))
	token, err := auth.EncodeHS256(jsonx.Obj("username", "diff-admin", "exp", time.Now().Add(time.Hour).Unix()), secret)
	if err != nil {
		t.Fatal(err)
	}
	return &world{srv, startOracle(t, pyDir), c, "Bearer " + token, t, nil}
}
func native(v any) any {
	data, err := jsonx.Compact(v)
	if err != nil {
		panic(err)
	}
	var out any
	if err := json.Unmarshal(data, &out); err != nil {
		panic(err)
	}
	return out
}
func snapshot(t *testing.T, srv *httpxtest.Server) any {
	t.Helper()
	doc, err := srv.Repo.ExportAll(srv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]any{}
	err = filepath.WalkDir(srv.DataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".json" && ext != ".yaml" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any
		if ext == ".json" {
			err = json.Unmarshal(data, &value)
		} else {
			err = yamlx.Unmarshal(data, &value)
		}
		if err != nil {
			return err
		}
		relative, _ := filepath.Rel(srv.DataDir, path)
		files[filepath.ToSlash(relative)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"document": native(doc), "files": files}
}

var generatedID = regexp.MustCompile(`^(?:node|agg)_[0-9a-f]{8}$`)
var dispositionTime = regexp.MustCompile(`_[0-9]{8}_[0-9]{6}\.yaml`)

func normalize(value any) any {
	switch v := value.(type) {
	case string:
		// Retain the exact Chinese validation prefix; regexp diagnostics are native.
		if strings.HasPrefix(v, "节点筛选正则无效: ") {
			return "节点筛选正则无效: <native-regex-error>"
		}
		return v
	case map[string]any:
		for k, item := range v {
			switch k {
			case "created_at", "updated_at", "cached_updated_at", "fetched_at", "traffic_at", "timestamp", "last_run", "last_success", "next_run", "at", "last_finished_at", "last_success_at", "last_run_at":
				if text, ok := item.(string); ok {
					_, absoluteErr := time.Parse(time.RFC3339Nano, text)
					_, localErr := time.Parse("2006-01-02T15:04:05.999999999", text)
					if absoluteErr == nil || localErr == nil {
						v[k] = "<time>"
					}
				}
			case "order":
				if ids, ok := item.([]any); ok {
					for i, id := range ids {
						if text, ok := id.(string); ok && generatedID.MatchString(text) {
							ids[i] = "<generated-id>"
						}
					}
				}
			case "id":
				if s, ok := item.(string); ok && generatedID.MatchString(s) {
					v[k] = "<generated-id>"
				}
			default:
				v[k] = normalize(item)
			}
		}
	case []any:
		for i, item := range v {
			v[i] = normalize(item)
		}
	}
	return value
}
func (w *world) call(method, path, body string, authenticated bool) answer {
	return w.callComparing(method, path, body, authenticated, false)
}

// callNativeProviderSyntax is used only by the five approved N7-3c diagnostics.
func (w *world) callNativeProviderSyntax(method, path, body string, authenticated bool) answer {
	return w.callComparing(method, path, body, authenticated, true)
}

func (w *world) callComparing(method, path, body string, authenticated, nativeSyntax bool) answer {
	t := w.t
	t.Helper()
	headers := map[string]string{}
	for key, value := range w.extraHeaders {
		headers[key] = value
	}
	if authenticated {
		headers["Authorization"] = w.token
	}
	var payload *string
	if body != "" {
		payload = &body
	}
	req := request{method, path, payload, headers, *w.control}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.py.stdin.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	if !w.py.scanner.Scan() {
		t.Fatalf("oracle ended: %v %s", w.py.scanner.Err(), w.py.output.String())
	}
	var py answer
	if err := json.Unmarshal(w.py.scanner.Bytes(), &py); err != nil {
		t.Fatalf("oracle JSON: %v %s", err, w.py.scanner.Text())
	}
	if py.Error != "" {
		t.Fatalf("oracle: %s", py.Error)
	}
	options := []httpxtest.Option{}
	for key, value := range w.extraHeaders {
		options = append(options, httpxtest.Header(key, value))
	}
	if body != "" {
		options = append(options, httpxtest.Body([]byte(body), "application/json"))
	}
	if authenticated {
		options = append(options, httpxtest.Bearer(strings.TrimPrefix(w.token, "Bearer ")))
	}
	var got *httpxtest.Result
	if method == "GENERATE" {
		got = w.generate(body)
	} else {
		got = w.srv.Do(method, path, options...)
	}
	var gb any
	if method == "HEAD" {
		gb = ""
	} else if strings.Contains(got.Header.Get("Content-Type"), "json") {
		err = json.Unmarshal(got.Body, &gb)
	} else if strings.Contains(got.Header.Get("Content-Type"), "yaml") {
		err = yamlx.Unmarshal(got.Body, &gb)
	} else {
		gb = string(got.Body)
	}
	if err != nil {
		t.Fatalf("decode Go: %v %s", err, got.Body)
	}
	// Named Python bugs: scalar selectors are iterated as character IDs; a
	// corrupt non-array cache can become a successful partial provider. Require
	// exact native rejection and keep comparing every stored effect.
	expectedRejection := false
	if method == "GENERATE" {
		var args struct {
			Aggregation struct{ Subscriptions json.RawMessage }
		}
		if json.Unmarshal([]byte(body), &args) == nil && (string(args.Aggregation.Subscriptions) == `"s"` || w.control.CorruptCache) && py.Status == 200 {
			expectedRejection = true
			if got.Status != 500 || string(got.Body) != `{"message":"聚合操作失败","success":false}` {
				t.Errorf("scalar selector rejection: %d %s", got.Status, got.Body)
			}
			t.Log("EXPECTED C31 Python bug: scalar subscription selector rejected; storage still compared")
		}
	}
	if got.Status != py.Status && !expectedRejection {
		t.Errorf("%s %s status Python=%d Go=%d body=%s", method, path, py.Status, got.Status, got.Body)
	}
	compare := func(label string, a, b any) {
		t.Helper()
		a = normalize(jsonValue(a))
		b = normalize(jsonValue(b))

		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s %s %s: %s", method, path, label, firstDifference(a, b, ""))
		}
	}
	if !expectedRejection {
		expectedBody := py.Body
		if nativeSyntax {
			expectedBody, err = nativeProviderSyntaxBody(method, path, w.control.Sources["s"], py, got.Status)
			if err != nil {
				t.Fatal(err)
			}
		}
		compare("body", expectedBody, gb)
	}
	comments := []string{}
	if strings.Contains(got.Header.Get("Content-Type"), "yaml") {
		for _, line := range strings.Split(string(got.Body), "\n") {
			if strings.HasPrefix(line, "#") {
				comments = append(comments, line)
			}
		}
	}
	stamp := regexp.MustCompile(`[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?Z?`)
	for _, side := range [][]string{py.Comments, comments} {
		for i, line := range side {
			side[i] = stamp.ReplaceAllString(line, "<timestamp>")
		}
	}
	compare("provider comments", py.Comments, comments)
	for _, key := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Profile", "X-ConfigFlow-Provider", "Content-Disposition"} {

		if method == "HEAD" && py.Status == 500 && key == "Content-Type" {
			// Step B #3: API HTML errors migrate to JSON, including bodyless HEAD.
			if !strings.HasPrefix(got.Header.Get(key), "application/json") {
				t.Error("HEAD API error must use JSON media type")
			}
			continue
		}
		a, b := py.Headers[key], got.Header.Get(key)
		if key == "Content-Disposition" {
			a = dispositionTime.ReplaceAllString(a, "_<time>.yaml")
			b = dispositionTime.ReplaceAllString(b, "_<time>.yaml")
		}
		if a != b {
			t.Errorf("%s %s header %s: Python=%q Go=%q", method, path, key, a, b)
		}
	}
	compare("state", py.State, snapshot(t, w.srv))
	t.Logf("compared %s %s: Python=%d Go=%d", method, path, py.Status, got.Status)
	return py
}

// jsonValue makes YAML integer/float values comparable as JSON numbers.
func jsonValue(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if json.Unmarshal(b, &out) != nil {
		return v
	}
	return out
}

func firstDifference(a, b any, path string) string {
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return fmt.Sprintf("%s type %T != %T", path, a, b)
	}
	switch v := a.(type) {
	case map[string]any:
		w := b.(map[string]any)
		for k, x := range v {
			y, ok := w[k]
			if !ok {
				return path + "." + k + " missing in Go"
			}
			if !reflect.DeepEqual(x, y) {
				return firstDifference(x, y, path+"."+k)
			}
		}
		for k := range w {
			if _, ok := v[k]; !ok {
				return path + "." + k + " extra in Go"
			}
		}
	case []any:
		w := b.([]any)
		if len(v) != len(w) {
			return fmt.Sprintf("%s length %d != %d", path, len(v), len(w))
		}
		for i, x := range v {
			if !reflect.DeepEqual(x, w[i]) {
				return firstDifference(x, w[i], fmt.Sprintf("%s[%d]", path, i))
			}
		}
	default:
		return fmt.Sprintf("%s Python=%v Go=%v", path, a, b)
	}
	return path
}

// generate exercises the same exported dry generator as converter/Agent bundles.
func (w *world) generate(body string) *httpxtest.Result {
	t := w.t
	t.Helper()
	var args struct {
		Aggregation, Config, Main json.RawMessage
		Prepared                  bool
	}
	if err := json.Unmarshal([]byte(body), &args); err != nil {
		t.Fatal(err)
	}
	out, err := generation.Generate(w.srv.Context(), args.Aggregation, args.Config, generation.Options{Main: args.Main, SourcePrepared: args.Prepared, Fetch: func(_ context.Context, id, url string) (*substore.SubscriptionResult, error) {
		if w.control.Offline {
			return nil, fmt.Errorf("synthetic offline")
		}
		return &substore.SubscriptionResult{Text: w.control.Sources[id], Source: "rendered_yaml", FetchedAt: "2026-01-01T00:00:00+00:00"}, nil
	}})
	status := 200
	var result any
	if err != nil {
		status = 500
		message := "聚合操作失败"
		var reference *rules.StrategyReferenceError
		if errors.As(err, &reference) {
			status = 400
			message = err.Error()
		}
		result = map[string]any{"success": false, "message": message}
	} else {
		var content any
		if err := yamlx.Unmarshal([]byte(out.Content), &content); err != nil {
			t.Fatal(err)
		}
		result = map[string]any{"name": out.Name, "file_path": out.FilePath, "content": content, "subscription_node_counts": out.SubscriptionNodeCounts, "total_count": out.TotalCount}
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	return &httpxtest.Result{T: t, Status: status, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: raw}
}
