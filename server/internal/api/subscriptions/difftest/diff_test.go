// Package difftest compares C30 HTTP responses and persisted state with Flask.
package difftest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
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

	"configflow/internal/api/nodes"
	"configflow/internal/api/subscriptions"
	"configflow/internal/auth"
	"configflow/internal/convert/mihomoenv"
	"configflow/internal/deploy"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/provider"
	"configflow/internal/refresh"
	"configflow/internal/repo"
	"configflow/internal/substore"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/yamlx"
)

const secret = "c30-synthetic-jwt-secret"

type control struct {
	Sources  map[string]string `json:"sources"`
	Offline  bool              `json:"offline"`
	URIChain bool              `json:"uri_chain"`
	Mutate   bool              `json:"mutate"`
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
	srv     *httpxtest.Server
	py      *oracle
	control *control
	token   string
	t       *testing.T
}

func node(id string) map[string]any {
	return map[string]any{"id": id, "name": id, "type": "http", "server": "example.com", "port": 80, "enabled": true}
}
func newWorld(t *testing.T, setup func(context.Context, *repo.Repository), sources map[string]string) *world {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	t.Cleanup(provider.SetLocations(deploy.Locations{}))
	nodes.Wire()
	mihomoenv.Wire()
	previousClock := notifications.Clock
	notifications.Clock = fixedClock{}.Now
	t.Cleanup(func() { notifications.Clock = previousClock })
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: secret, Register: httpxtest.Routes(nodes.Register, subscriptions.Register)})
	ctx := srv.Context()
	// The isolated app projects only the task families seeded by this fixture.
	// Other modules have no resources in this synthetic repository.
	notifications.SetInputs(notifications.Inputs{
		Agents: func(context.Context) ([]model.Agent, error) { return []model.Agent{}, nil },
		Rows: func(ctx context.Context) ([]*tasks.Task, error) {
			status, err := taskstatus.New(srv.DataDir).Read()
			if err != nil {
				return nil, err
			}
			subs, err := refresh.SubscriptionTasks(ctx, status)
			if err != nil {
				return nil, err
			}
			rules, err := refresh.RuleTasks(ctx, status)
			return append(subs, rules...), err
		},
	})
	t.Cleanup(notifications.ResetInputs)
	err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.SystemConfig.ConfigToken = "c30-pull"
		s.SystemConfig.RuleProxyToken = "c30-internal"
		s.SystemConfig.RetiredRuleProxyTokens = []string{"c30-retired"}
		s.SystemConfig.ServerDomain = "http://127.0.0.1:5001"
		return nil
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
		_, err := srv.Repo.UpdateProfile(ctx, "default", func(_ context.Context, p *model.Profile) error {
			p.ProxyGroups[0].Name = "Changed"
			manual := []string{"exit"}
			p.ProxyGroups[0].ManualNodes = &manual
			return nil
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
	re := refresh.DefaultRefreshEnv()
	re.Clock = fixedClock{}
	re.HTTP = env.HTTP
	re.RandomHex = env.RandomHex
	t.Cleanup(refresh.SetRefreshEnv(re))
	t.Cleanup(provider.SetDeliveryEnv(&provider.DeliveryEnv{NodeID: func() string { return "node_00000001" }}))
	token, err := auth.EncodeHS256(map[string]any{"username": "diff-admin", "exp": time.Now().Add(time.Hour).Unix()}, secret)
	if err != nil {
		t.Fatal(err)
	}
	return &world{srv, startOracle(t, pyDir), c, "Bearer " + token, t}
}
func native(v any) any {
	data, err := json.Marshal(v)
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
	doc, err := srv.Repo.ExportAllJSON(srv.Context(), false)
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

var generatedID = regexp.MustCompile(`^node_[0-9a-f]{8}$`)
var dispositionTime = regexp.MustCompile(`_[0-9]{8}_[0-9]{6}\.yaml`)

func normalize(value any) any {
	switch v := value.(type) {
	case string:
		// Malformed optional request bodies retain status and rejection, while
		// Go reports its own diagnostic instead of Python attribute errors.
		if v == "request body must be a JSON object" || regexp.MustCompile(`^'(?:list|str|bool|int|float)' object has no attribute 'get'$`).MatchString(v) {
			return "<invalid-object-body>"
		}
		// Only English/native exception identity is unstable; Chinese text stays exact.
		v = regexp.MustCompile(`request_failed \([^)]*\)`).ReplaceAllString(v, "request_failed (<native-transport-error>)")
		if strings.HasPrefix(v, "更新失败（") && strings.HasSuffix(v, "）") {
			return "更新失败（<native-error-type>）"
		}
		if strings.HasPrefix(v, "节点筛选正则无效: ") {
			return "节点筛选正则无效: <native-regex-error>"
		}
		return v
	case map[string]any:
		for k, item := range v {
			switch k {
			case "created_at", "updated_at", "cached_updated_at", "fetched_at", "traffic_at", "timestamp", "last_run", "last_success", "next_run", "at", "last_finished_at", "last_success_at", "last_run_at", "last_duration_ms", "elapsed_ms", "duration_ms", "fetched_at_unix":
				if item != nil {
					v[k] = "<time>"
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
	return w.callWithNativeBody(method, path, body, authenticated, nil)
}

type nativeBodyExpectation struct {
	Python map[string]any
	Native map[string]any
}

// callWithNativeBody pins both complete bodies for a single approved boundary;
// status, headers, comments and stored effects keep the ordinary comparison.
func (w *world) callWithNativeBody(method, path, body string, authenticated bool, expected *nativeBodyExpectation) answer {
	t := w.t
	t.Helper()
	headers := map[string]string{}
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
	if body != "" {
		options = append(options, httpxtest.Body([]byte(body), "application/json"))
	}
	if authenticated {
		options = append(options, httpxtest.Bearer(strings.TrimPrefix(w.token, "Bearer ")))
	}
	got := w.srv.Do(method, path, options...)
	var gb any
	if strings.Contains(got.Header.Get("Content-Type"), "json") {
		err = json.Unmarshal(got.Body, &gb)
	} else if strings.Contains(got.Header.Get("Content-Type"), "yaml") {
		err = yamlx.Unmarshal(got.Body, &gb)
	} else {
		gb = string(got.Body)
	}
	if err != nil {
		t.Fatalf("decode Go: %v %s", err, got.Body)
	}
	approvedShape := approvedShapeError(path, body, py, got)
	if got.Status != py.Status && !approvedShape {
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
	if expected != nil {
		compare("Python boundary body", expected.Python, py.Body)
		compare("native boundary body", expected.Native, gb)
	} else if approvedShape {
		t.Log("EXPECTED Step B #1: invalid input returns accurate 400 JSON; stored effects still compared")
	} else {
		compare("body", py.Body, gb)
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
	pyBytes, _ := json.Marshal(py.Body)
	for _, key := range []string{"Content-Type", "Cache-Control", "X-ConfigFlow-Profile", "X-ConfigFlow-Provider", "Content-Disposition"} {
		if approvedShape && key == "Content-Type" {
			if !strings.HasPrefix(got.Header.Get(key), "application/json") {
				t.Error("expected JSON shape error")
			}
			continue
		}
		a, b := py.Headers[key], got.Header.Get(key)
		if key == "X-ConfigFlow-Profile" {
			a = httpxtest.O9ExpectedProfileHeader(a, py.Status, got.Status, pyBytes, got.Body)
		}
		if key == "Content-Disposition" {
			a = dispositionTime.ReplaceAllString(a, "_<time>.yaml")
			b = dispositionTime.ReplaceAllString(b, "_<time>.yaml")
		}
		if a != b {
			t.Errorf("%s %s header %s: Python=%q Go=%q", method, path, key, a, b)
		}
	}
	compare("state", py.State, snapshot(t, w.srv))
	t.Logf("same %s %s → %d", method, path, got.Status)
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
