// Package difftest compares backup APIs, uploaded documents and stored effects
// against the real Flask application using independent loopback DAV servers.
package difftest

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
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

	"configflow/internal/api/settings"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/httpx/httpxtest"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/notifications"
	"configflow/internal/repo"
	"configflow/internal/repo/repotest"
	"configflow/internal/tasks"
	"configflow/internal/taskstatus"
	"configflow/internal/webdav"
)

const jwtSecret = "c35-synthetic-jwt-secret"

var fixed = time.Date(2026, 10, 22, 12, 0, 0, 123456000, time.UTC)

type dav struct {
	mu      sync.Mutex
	dirs    map[string]bool
	files   map[string]json.RawMessage
	fail    string
	code    int
	uploads int
	events  []string
	server  *httptest.Server
}

func newDAV(t *testing.T) *dav {
	t.Helper()
	d := &dav{dirs: map[string]bool{"/": true}, files: map[string]json.RawMessage{}}
	d.server = httptest.NewServer(http.HandlerFunc(d.serve))
	t.Cleanup(d.server.Close)
	return d
}
func (d *dav) serve(w http.ResponseWriter, r *http.Request) {
	d.mu.Lock()
	defer d.mu.Unlock()
	user, password, ok := r.BasicAuth()
	if !ok || user != "fixture-user" || password != "fixture-password" {
		w.WriteHeader(401)
		return
	}
	remote := r.URL.Path
	d.events = append(d.events, r.Method+" "+remote)
	if d.fail == r.Method {
		w.WriteHeader(d.code)
		fmt.Fprint(w, "fixture-user fixture-password private-response-body https://credential:secret@example.com/")
		return
	}
	switch r.Method {
	case "HEAD":
		if !d.dirs[remote] && d.files[remote] == nil {
			w.WriteHeader(404)
		}
	case "MKCOL":
		d.dirs[remote] = true
		w.WriteHeader(201)
	case "PUT":
		data, _ := io.ReadAll(r.Body)
		d.files[remote] = data
		d.uploads++
		w.WriteHeader(201)
	case "PROPFIND":
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(207)
		fmt.Fprint(w, `<?xml version="1.0"?><d:multistatus xmlns:d="DAV:">`)
		names := []string{remote}
		for name := range d.files {
			if strings.HasPrefix(name, remote) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		for _, name := range names {
			fmt.Fprint(w, `<d:response><d:href>`)
			_ = xml.EscapeText(w, []byte(name))
			fmt.Fprint(w, `</d:href><d:propstat><d:prop><d:resourcetype/></d:prop><d:status>HTTP/1.1 200 OK</d:status></d:propstat></d:response>`)
		}
		fmt.Fprint(w, `</d:multistatus>`)
	case "DELETE":
		delete(d.files, remote)
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}
func (d *dav) state() any {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, _ := json.Marshal(struct {
		Dirs    map[string]bool            `json:"dirs"`
		Files   map[string]json.RawMessage `json:"files"`
		Uploads int                        `json:"uploads"`
	}{d.dirs, d.files, d.uploads})
	var result any
	_ = json.Unmarshal(data, &result)
	return result
}
func (d *dav) setFailure(method string, status int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.fail, d.code = method, status
}

type loopbackOnly struct {
	client *http.Client
	host   string
}

func (c loopbackOnly) Do(req *http.Request) (*http.Response, error) {
	if req.URL.Host != c.host {
		return nil, fmt.Errorf("non-fixture network request refused")
	}
	return c.client.Do(req)
}

type oracle struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	scanner *bufio.Scanner
	log     bytes.Buffer
}

func startOracle(t *testing.T, dir, url string) *oracle {
	t.Helper()
	python := os.Getenv("CONFIGFLOW_DIFF_PYTHON")
	if python == "" {
		home, _ := os.UserHomeDir()
		python = filepath.Join(home, "cf-test/.venv/bin/python")
	}
	root, _ := filepath.Abs("../../../..")
	script, _ := filepath.Abs("testdata/reference.py")
	o := &oracle{cmd: exec.Command(python, script)}
	o.cmd.Dir = root
	repotest.PythonLayout(t, dir) // Python reads the version-1 layout (O17)
	o.cmd.Env = append(os.Environ(), "PYTHONPATH="+root, "DATA_DIR="+dir, "ADMIN_USERNAME=diff-admin", "ADMIN_PASSWORD=diff-password", "JWT_SECRET_KEY="+jwtSecret, "FAKE_DAV_URL="+url, "PYTHONDONTWRITEBYTECODE=1", "SUB_STORE_BUILTIN_DIR=/nonexistent", "LOG_FILE="+filepath.Join(dir, "app.log"), "NO_PROXY=127.0.0.1,localhost")
	var err error
	o.stdin, err = o.cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := o.cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	o.cmd.Stderr = &o.log
	o.scanner = bufio.NewScanner(stdout)
	o.scanner.Buffer(make([]byte, 65536), 8<<20)
	if err := o.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	// Wait for the real blueprints to finish importing before any commits start
	// an automatic-backup debounce timer on either side.
	if _, err := o.stdin.Write([]byte("{\"method\":\"GET\",\"path\":\"/api/auth/status\",\"headers\":{}}\n")); err != nil {
		t.Fatal(err)
	}
	if !o.scanner.Scan() {
		t.Fatalf("oracle startup: %s", o.log.String())
	}
	t.Cleanup(func() {
		o.stdin.Close()
		o.cmd.Process.Kill()
		o.cmd.Wait()
		if t.Failed() {
			t.Log(o.log.String())
		}
	})
	return o
}

type request struct {
	Method      string            `json:"method"`
	Path        string            `json:"path"`
	Body        *string           `json:"body"`
	Headers     map[string]string `json:"headers"`
	ContentType string            `json:"content_type"`
	WaitAuto    bool              `json:"wait_auto"`
}
type answer struct {
	Status int    `json:"status"`
	Body   any    `json:"body"`
	State  any    `json:"state"`
	Error  string `json:"oracle_error"`
}
type world struct {
	t                *testing.T
	srv              *httpxtest.Server
	py               *oracle
	pythonDAV, goDAV *dav
	service          *webdav.Service
	token            string
	pyDir            string
}

func newWorld(t *testing.T) *world { return newWorldSaved(t, nil) }
func newWorldSaved(t *testing.T, retention map[string]any) *world {
	t.Helper()
	if os.Getenv("CONFIGFLOW_DIFF") != "1" {
		t.Skip("set CONFIGFLOW_DIFF=1 on test server")
	}
	pd, gd := newDAV(t), newDAV(t)
	service := webdav.NewService(loopbackOnly{gd.server.Client(), strings.TrimPrefix(gd.server.URL, "http://")})
	service.Now = func() time.Time { return fixed }
	srv := httpxtest.New(t, httpxtest.Options{Auth: true, Username: "diff-admin", Password: "diff-password", SecretKey: jwtSecret, Setup: func(app *httpx.App) {
		settings.NewHandler(app.Repo, nil, service).Register(app.Router)
	}})
	ctx := srv.Context()
	backup := map[string]any{"webdav_url": "<DAV>", "webdav_username": "fixture-user", "webdav_password": "fixture-password", "webdav_path": "/backups/", "auto_backup": false}
	err := srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		raw, err := json.Marshal(backup)
		if err != nil {
			return err
		}
		if err := s.SetJSON("backup", raw); err != nil {
			return err
		}
		s.SystemConfig.ServerDomain = "http://127.0.0.1:5001"
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(retention) > 0 {
		for k, v := range retention {
			backup[k] = v
		}
		raw, err := json.Marshal(backup)
		if err != nil {
			t.Fatal(err)
		}
		// Saved retention may hold hand-edited values of the wrong type,
		// which the plain record refuses as input: the row is written directly.
		repotest.WriteRow(t, srv.Repo.DatabasePath(), "system", "", "backup", raw)
	}
	pyDir := t.TempDir()
	if out, err := exec.Command("cp", "-a", srv.DataDir+"/.", pyDir).CombinedOutput(); err != nil {
		t.Fatalf("copy %v %s", err, out)
	}
	// Each process has its own endpoint but the same credentials/catalog.
	err = srv.Repo.UpdateSystem(ctx, func(_ context.Context, s *model.System) error {
		s.Backup.WebDAVURL = gd.server.URL
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Update the independent Python copy using its repository format, not file edits.
	pr, err := repo.Open(pyDir, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	err = pr.UpdateSystem(repo.WithRepository(context.Background(), pr), func(_ context.Context, s *model.System) error {
		s.Backup.WebDAVURL = pd.server.URL
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := lifecycle.NewRuntime(lifecycle.Options{})
	tk := tasks.NewRegistry(srv.Repo, rt)
	service.Repo = srv.Repo
	service.Tasks = tk
	service.Subscribe(srv.Repo)
	service.Register(rt, tk)
	for _, slot := range tk.Missing() {
		tk.SetRowBuilder(slot, func(context.Context, taskstatus.Snapshot) ([]*tasks.Task, error) { return []*tasks.Task{}, nil })
	}
	service.Notifications = notifications.New(notifications.Options{
		Repo:   srv.Repo,
		Tasks:  tk,
		Agents: func(context.Context) ([]model.Agent, error) { return []model.Agent{}, nil },
		Rows:   tk.List,
	})
	previousClock := notifications.Clock
	notifications.Clock = func() time.Time { return fixed }
	t.Cleanup(func() { notifications.Clock = previousClock })
	token, err := auth.EncodeHS256(map[string]any{"username": "diff-admin", "exp": time.Now().Add(time.Hour).Unix()}, jwtSecret)
	if err != nil {
		t.Fatal(err)
	}
	oracle := startOracle(t, pyDir, pd.server.URL)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.WaitIdle(ctx); err != nil {
			t.Error(err)
		}
	})
	return &world{t, srv, oracle, pd, gd, service, "Bearer " + token, pyDir}
}
func storeState(t *testing.T, srv *httpxtest.Server) any {
	t.Helper()
	data, err := srv.Repo.ExportAllJSON(srv.Context(), false)
	if err != nil {
		t.Fatal(err)
	}
	var document any
	_ = json.Unmarshal(data, &document)
	files := map[string]any{}
	err = filepath.WalkDir(srv.DataDir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var value any
		if err := json.Unmarshal(data, &value); err != nil {
			return err
		}
		name, _ := filepath.Rel(srv.DataDir, path)
		files[filepath.ToSlash(name)] = value
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{"document": document, "files": files}
}

var nativeStatus = regexp.MustCompile(`\b(?:ResponseErrorCode|RemoteError) (HTTP [0-9]{3})`)

func (w *world) normalize(value any) any {
	switch v := value.(type) {
	case string:
		v = strings.ReplaceAll(strings.ReplaceAll(v, w.pythonDAV.server.URL, "<DAV>"), w.goDAV.server.URL, "<DAV>")
		v = nativeStatus.ReplaceAllString(v, "$1")
		return v
	case map[string]any:
		for key, item := range v {
			switch key {
			case "created_at", "updated_at", "last_finished_at", "last_duration_ms":
				if item != nil {
					v[key] = "<time>"
				}
			default:
				v[key] = w.normalize(item)
			}
		}
	case []any:
		for i, item := range v {
			v[i] = w.normalize(item)
		}
	}
	return value
}
func firstDifference(a, b any, path string) string {
	if reflect.TypeOf(a) != reflect.TypeOf(b) {
		return fmt.Sprintf("%s type %T != %T", path, a, b)
	}
	switch left := a.(type) {
	case map[string]any:
		right := b.(map[string]any)
		for k, x := range left {
			y, ok := right[k]
			if !ok {
				return path + "." + k + " missing in Go"
			}
			if !reflect.DeepEqual(x, y) {
				return firstDifference(x, y, path+"."+k)
			}
		}
		for k := range right {
			if _, ok := left[k]; !ok {
				return path + "." + k + " extra in Go"
			}
		}
	case []any:
		right := b.([]any)
		if len(left) != len(right) {
			return fmt.Sprintf("%s length %d != %d", path, len(left), len(right))
		}
		for i, x := range left {
			if !reflect.DeepEqual(x, right[i]) {
				return firstDifference(x, right[i], fmt.Sprintf("%s[%d]", path, i))
			}
		}
	default:
		return fmt.Sprintf("%s Python=%v Go=%v", path, a, b)
	}
	return path
}
func (w *world) call(method, path, body string, admin, wait bool) answer {
	return w.callApproved(method, path, body, admin, wait, nil)
}

// callApproved compares all effects while spelling out one approved Python bug
// correction. The callback must assert the original Python value before editing it.
func (w *world) callApproved(method, path, body string, admin, wait bool, approved func(*answer)) answer {
	t := w.t
	t.Helper()
	headers := map[string]string{}
	if admin {
		headers["Authorization"] = w.token
	}
	var pyBody *string
	if body != "" {
		data := strings.ReplaceAll(body, "<DAV>", w.pythonDAV.server.URL)
		pyBody = &data
	}
	command := request{Method: method, Path: path, Body: pyBody, Headers: headers, ContentType: "application/json", WaitAuto: wait}
	data, _ := json.Marshal(command)
	if _, err := w.py.stdin.Write(append(data, '\n')); err != nil {
		t.Fatal(err)
	}
	options := []httpxtest.Option{}
	if body != "" {
		options = append(options, httpxtest.Body([]byte(strings.ReplaceAll(body, "<DAV>", w.goDAV.server.URL)), "application/json"))
	}
	if admin {
		options = append(options, httpxtest.Bearer(strings.TrimPrefix(w.token, "Bearer ")))
	}
	got := w.srv.Do(method, path, options...)
	if !w.py.scanner.Scan() {
		t.Fatalf("oracle stopped: %v %s", w.py.scanner.Err(), w.py.log.String())
	}
	var py answer
	if err := json.Unmarshal(w.py.scanner.Bytes(), &py); err != nil {
		t.Fatal(err)
	}
	if py.Error != "" {
		t.Fatal(py.Error)
	}
	if approved != nil {
		approved(&py)
	}

	var value any
	if json.Unmarshal(got.Body, &value) != nil {
		value = string(got.Body)
	}
	if wait {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := w.service.WaitIdle(ctx); err != nil {
			cancel()
			t.Fatal(err)
		}
		cancel()
	}
	compare := func(label string, left, right any) {
		left = w.normalize(left)
		right = w.normalize(right)
		if !reflect.DeepEqual(left, right) {
			t.Errorf("%s %s %s: %s", method, path, label, firstDifference(left, right, ""))
		}
	}
	if py.Status != got.Status {
		t.Errorf("%s %s status Python=%d Go=%d", method, path, py.Status, got.Status)
	}
	compare("body", py.Body, value)
	compare("stored state", py.State, storeState(t, w.srv))
	compare("remote DAV state", w.pythonDAV.state(), w.goDAV.state())
	t.Logf("same %s %s → %d", method, path, got.Status)
	return py
}
