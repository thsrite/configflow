package substore

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	schedclock "configflow/internal/clock"
	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/repo"
)

// fakeSubStore emulates the Sub-Store endpoints the client and the runtime
// call: POST /api/subs, GET /download/<name>/<target>, DELETE
// /api/sub/<name>, GET /api/utils/env. It never contacts anything.
type fakeSubStore struct {
	srv *httptest.Server

	mu           sync.Mutex
	version      string // "" = not running (the env endpoint answers 503)
	events       []string
	created      []map[string]any
	downloads    []url.Values
	envRequests  int
	createStatus int
	// download answers a download (nil: one converted proxy per request).
	download func(name, target string, query url.Values, sub map[string]any) (int, string, http.Header)
	subs     map[string]map[string]any
	onDelete func()
}

const fakeProxyYAML = "proxies:\n  - {name: from-sub-store, type: ss, server: example.test, port: 443, cipher: aes-128-gcm, password: p}\n"

func newFakeSubStore(t *testing.T) *fakeSubStore {
	f := &fakeSubStore{createStatus: 201, subs: map[string]map[string]any{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSubStore) port() string { return strconv.Itoa(f.srv.Listener.Addr().(*net.TCPAddr).Port) }

func (f *fakeSubStore) setVersion(v string) {
	f.mu.Lock()
	f.version = v
	f.mu.Unlock()
}

func (f *fakeSubStore) running() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.version != ""
}

func (f *fakeSubStore) eventList() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.events...)
}

func (f *fakeSubStore) createdPayloads() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.created...)
}

func segment(escaped string) string {
	s, err := url.PathUnescape(escaped)
	if err != nil {
		return escaped
	}
	return s
}

func (f *fakeSubStore) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.EscapedPath()
	f.mu.Lock()
	switch {
	case r.Method == "GET" && path == "/api/utils/env":
		f.envRequests++
		version := f.version
		f.mu.Unlock()
		if version == "" {
			http.Error(w, "down", 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"status":"success","data":{"backend":"Node","version":"`+version+`"}}`)
	case r.Method == "POST" && path == "/api/subs":
		body, _ := io.ReadAll(r.Body)
		var payload map[string]any
		_ = json.Unmarshal(body, &payload)
		f.created = append(f.created, payload)
		name, _ := payload["name"].(string)
		f.subs[name] = payload
		f.events = append(f.events, "create:"+name)
		status := f.createStatus
		f.mu.Unlock()
		w.WriteHeader(status)
		io.WriteString(w, `{"status":"success"}`)
	case r.Method == "GET" && strings.HasPrefix(path, "/download/"):
		parts := strings.Split(strings.TrimPrefix(path, "/download/"), "/")
		name, target := segment(parts[0]), ""
		if len(parts) > 1 {
			target = segment(parts[1])
		}
		query := r.URL.Query()
		f.downloads = append(f.downloads, query)
		f.events = append(f.events, "download:"+name)
		sub := f.subs[name]
		download := f.download
		f.mu.Unlock()
		status, text, header := 200, fakeProxyYAML, http.Header{}
		if download != nil {
			status, text, header = download(name, target, query, sub)
		}
		if status < 0 {
			// A transport failure: close the connection without answering.
			hijack(w)
			return
		}
		for k, v := range header {
			w.Header()[k] = v
		}
		w.WriteHeader(status)
		io.WriteString(w, text)
	case r.Method == "DELETE" && strings.HasPrefix(path, "/api/sub/"):
		name := segment(strings.TrimPrefix(path, "/api/sub/"))
		delete(f.subs, name)
		f.events = append(f.events, "delete:"+name)
		onDelete := f.onDelete
		f.mu.Unlock()
		if onDelete != nil {
			onDelete()
		}
		io.WriteString(w, `{"status":"success"}`)
	default:
		f.mu.Unlock()
		http.NotFound(w, r)
	}
}

func hijack(w http.ResponseWriter) {
	conn, _, err := w.(http.Hijacker).Hijack()
	if err == nil {
		conn.Close()
	}
}

// fakeAirport is an upstream subscription server on loopback.
type fakeAirport struct {
	srv     *httptest.Server
	mu      sync.Mutex
	hits    int
	agents  []string
	handler func(w http.ResponseWriter, r *http.Request)
}

func newFakeAirport(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) *fakeAirport {
	a := &fakeAirport{handler: handler}
	a.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a.mu.Lock()
		a.hits++
		a.agents = append(a.agents, r.Header.Get("User-Agent"))
		a.mu.Unlock()
		a.handler(w, r)
	}))
	t.Cleanup(a.srv.Close)
	return a
}

func (a *fakeAirport) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.hits
}

// url is the subscription URL of the airport (a path and a token-like query).
func (a *fakeAirport) url() string { return a.srv.URL + "/sub?token=private" }

func serveText(text string, header map[string]string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		for k, v := range header {
			w.Header().Set(k, v)
		}
		io.WriteString(w, text)
	}
}

// fakeCommands emulates supervisorctl (driving the fake Sub-Store) and node.
type fakeCommands struct {
	mu         sync.Mutex
	store      *fakeSubStore
	calls      [][]string
	starts     int
	stops      int
	restarts   int
	runVersion string // version a started process reports
	// onRestart decides what a restart does (nil: run runVersion).
	onRestart func(n int) (CommandResult, error)
	// fail makes every supervisorctl call fail like an OSError.
	fail      error
	checkCode int
}

func (c *fakeCommands) Run(_ context.Context, argv []string, _ time.Duration) (CommandResult, error) {
	c.mu.Lock()
	c.calls = append(c.calls, append([]string(nil), argv...))
	c.mu.Unlock()
	switch argv[0] {
	case "supervisorctl":
		c.mu.Lock()
		fail := c.fail
		c.mu.Unlock()
		if fail != nil {
			return CommandResult{}, fail
		}
		switch argv[3] {
		case "status":
			if c.store.running() {
				return CommandResult{Stdout: "sub-store                        RUNNING   pid 7, uptime 0:00:05\n"}, nil
			}
			return CommandResult{Stdout: "sub-store                        STOPPED   Not started\n", ReturnCode: 3}, nil
		case "start":
			c.mu.Lock()
			c.starts++
			c.mu.Unlock()
			c.store.setVersion(c.runVersion)
			return CommandResult{Stdout: "sub-store: started\n"}, nil
		case "stop":
			c.mu.Lock()
			c.stops++
			c.mu.Unlock()
			c.store.setVersion("")
			return CommandResult{Stdout: "sub-store: stopped\n"}, nil
		case "restart":
			c.mu.Lock()
			c.restarts++
			n := c.restarts
			onRestart := c.onRestart
			c.mu.Unlock()
			if onRestart != nil {
				return onRestart(n)
			}
			c.store.setVersion(c.runVersion)
			return CommandResult{Stdout: "sub-store: stopped\nsub-store: started\n"}, nil
		}
	case "node":
		if argv[1] == "--version" {
			return CommandResult{Stdout: "v26.0.0\n"}, nil
		}
		c.mu.Lock()
		code := c.checkCode
		c.mu.Unlock()
		if code != 0 {
			return CommandResult{Stderr: "SyntaxError: Unexpected token\n", ReturnCode: code}, nil
		}
		return CommandResult{}, nil
	}
	return CommandResult{}, &PyError{Class: "FileNotFoundError", Message: "[Errno 2] No such file or directory: '" + argv[0] + "'"}
}

func (c *fakeCommands) counts() (int, int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.starts, c.stops
}

// fakeClock is a settable clock; Sleep advances it.
type fakeClock struct {
	mu   sync.Mutex
	now  time.Time
	mono float64
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 11, 15, 8, 0, 0, 123456000, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Monotonic() float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.mono
}

func (c *fakeClock) Sleep(d time.Duration) { c.advance(d.Seconds()) }

func (c *fakeClock) set(mono float64) {
	c.mu.Lock()
	c.mono = mono
	c.mu.Unlock()
}

func (c *fakeClock) advance(seconds float64) {
	c.mu.Lock()
	c.mono += seconds
	c.now = c.now.Add(time.Duration(seconds * float64(time.Second)))
	c.mu.Unlock()
}

// harness is one test's Sub-Store world.
type harness struct {
	t        *testing.T
	store    *fakeSubStore
	commands *fakeCommands
	clock    *fakeClock
	vars     map[string]string
	builtin  string // the image's bundle directory
	data     string // DATA_DIR
	service  *Service
	envMu    sync.Mutex
	nextID   int
}

type harnessOptions struct {
	builtin   bool   // the bundled Sub-Store of the image exists
	version   string // its VERSION
	realClock bool
}

func newHarness(t *testing.T, opts harnessOptions) *harness {
	base := t.TempDir()
	h := &harness{t: t, store: newFakeSubStore(t), clock: newFakeClock(), vars: map[string]string{},
		builtin: filepath.Join(base, "opt"), data: filepath.Join(base, "data")}
	// Standalone helpers that call GetMode without a bound context must use
	// this fixture's native repository, never the package directory or another test.
	repository, err := repo.Open(h.data, repo.Options{})
	if err != nil {
		t.Fatal(err)
	}
	repo.SetRepository(repository)
	t.Cleanup(func() { repo.SetRepository(nil) })
	h.commands = &fakeCommands{store: h.store, runVersion: opts.version}
	if h.commands.runVersion == "" {
		h.commands.runVersion = "2.40.0"
	}
	os.MkdirAll(h.builtin, 0o755)
	os.MkdirAll(h.data, 0o755)
	if opts.builtin {
		os.WriteFile(filepath.Join(h.builtin, SubStoreAsset), []byte("builtin"), 0o644)
		version := opts.version
		if version == "" {
			version = "2.40.0"
		}
		os.WriteFile(filepath.Join(h.builtin, "VERSION"), []byte(version+"\n"), 0o644)
	}
	h.vars["SUB_STORE_BUILTIN_DIR"] = h.builtin
	h.vars["SUB_STORE_BACKEND_API_PORT"] = h.store.port()
	h.vars["SUB_STORE_URL"] = h.store.srv.URL
	var clock Clock = h.clock
	if opts.realClock {
		clock = SystemClock
	}
	env := &Env{
		LookupEnv: func(key string) (string, bool) {
			h.envMu.Lock()
			defer h.envMu.Unlock()
			v, ok := h.vars[key]
			return v, ok
		},
		DataDir:  func() string { return h.data },
		HTTP:     RequestsClient{},
		Commands: h.commands,
		Clock:    clock,
		RandomHex: func() string {
			h.envMu.Lock()
			defer h.envMu.Unlock()
			h.nextID++
			return strconv.FormatInt(int64(0xa0000000+h.nextID), 16)
		},
	}
	h.service = NewService(env)
	h.service.Runtime.Directory = filepath.Join(base, "runtime")
	if !opts.realClock {
		h.service.Runtime.clock = advancingScheduler{h.clock}
	}
	runner := lifecycle.NewRuntime(lifecycle.Options{Registry: lifecycle.NewRegistry(), Grace: 20 * time.Millisecond})
	runner.Start(context.Background())
	t.Cleanup(func() { runner.Shutdown(context.Background()) })
	h.service.Dependencies.Spawner = completingUpdateSpawner{runner}
	return h
}

func (h *harness) setenv(key, value string) {
	h.envMu.Lock()
	h.vars[key] = value
	h.envMu.Unlock()
}

func (h *harness) unsetenv(key string) {
	h.envMu.Lock()
	delete(h.vars, key)
	h.envMu.Unlock()
}

// logCapture records every log line while a test runs.
type logCapture struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *logCapture) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *logCapture) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.String()
}

func captureLogs(t *testing.T) *logCapture {
	capture := &logCapture{}
	root, err := logx.Setup(logx.Config{Level: logx.Debug, Console: capture})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close(); logx.Setup(logx.Config{Level: logx.Info, Console: io.Discard}) })
	return capture
}

func must[T any](t *testing.T) func(T, error) T {
	return func(v T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
}

// completingUpdateSpawner preserves the old harness's synchronous observations,
// but actual update work still runs independently under a tracked Runtime.
type completingUpdateSpawner struct{ runner *lifecycle.Runtime }

func (s completingUpdateSpawner) Spawn(ctx context.Context, name string, fn func(context.Context)) error {
	done := make(chan struct{})
	if err := s.runner.Spawn(ctx, name, func(worker context.Context) { defer close(done); fn(worker) }); err != nil {
		return err
	}
	<-done
	return nil
}

type advancingScheduler struct{ *fakeClock }
type immediateTimer struct{ ch chan time.Time }

func (c advancingScheduler) NewTimer(d time.Duration) schedclock.Timer {
	c.advance(d.Seconds())
	ch := make(chan time.Time, 1)
	ch <- c.Now()
	return immediateTimer{ch}
}
func (t immediateTimer) C() <-chan time.Time { return t.ch }
func (t immediateTimer) Stop() bool          { return false }
