// Package httpxtest runs route packages through the full request pipeline
// in tests: a temporary data directory with its own repository, the global
// hooks (profile context, authentication gate, redaction, CORS), the auth
// routes, and helpers to log in, send requests and decode JSON. It is the Go
// counterpart of the `authenticated_app` fixture of
// backend/test_agent_upgrade_auth.py and of make_app in test_qa_integrity.py.
//
//	srv := httpxtest.New(t, httpxtest.Options{Register: settings.Register, Auth: true})
//	admin := srv.Login()                                   // Authorization header
//	res := srv.Get("/api/server-domain", admin)
//	res.WantStatus(200)
//	if res.Object().GetString("server_domain") != "" { ... }
//	res = srv.Post("/api/config-token", httpxtest.JSON(map[string]any{"generate": true}), admin)
//
// Use it from an external test package (package foo_test) when foo is one of
// the packages wired here. Authentication settings are process-wide
// (internal/auth); tests that enable auth must not run in parallel with tests
// expecting it disabled.
package httpxtest

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	apiauth "configflow/internal/api/auth"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonresp"
	"configflow/internal/repo"
	"configflow/internal/testjson"
)

// Default credentials of an authenticated test server (the Python fixture's).
const (
	Username = "upgrade-admin"
	Password = "upgrade-test-password"
)

// Options configure a test server.
type Options struct {
	// Register adds route packages (their Register functions); the auth
	// routes are always present.
	Register []func(*httpx.Router)
	// Auth enables login with Username/Password (or the defaults above).
	Auth               bool
	Username, Password string
	// SecretKey for login tokens; empty: random.
	SecretKey string
	// DataDir; empty: t.TempDir().
	DataDir string
	// DefaultFactories opens the repository like production
	// (repo.OpenDefault); otherwise like ProfileRepository(tmp_path).
	DefaultFactories bool
	// Setup runs after the routes are registered (add hooks, error handlers).
	Setup func(app *httpx.App)
}

// Server is a running test application.
type Server struct {
	T       testing.TB
	App     *httpx.App
	Repo    *repo.Repository
	DataDir string
	handler http.Handler
	http    *httptest.Server
}

// Routes is a convenience for Options.Register.
func Routes(register ...func(*httpx.Router)) []func(*httpx.Router) { return register }

// New builds the application on a fresh data directory.
func New(t testing.TB, options Options) *Server {
	t.Helper()
	dataDir := options.DataDir
	if dataDir == "" {
		dataDir = t.TempDir()
	}
	var repository *repo.Repository
	var err error
	if options.DefaultFactories {
		repository, err = repo.OpenDefault(dataDir)
	} else {
		repository, err = repo.Open(dataDir, repo.Options{})
	}
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	// Borrowed snapshots the guard remembers are re-checked when the test ends.
	t.Cleanup(func() {
		if err := repository.CheckBorrowed(); err != nil {
			t.Error(err)
		}
	})
	config := auth.Config{SecretKey: options.SecretKey}
	if options.Auth {
		config.AdminUsername, config.AdminPassword = options.Username, options.Password
		if config.AdminUsername == "" {
			config.AdminUsername = Username
		}
		if config.AdminPassword == "" {
			config.AdminPassword = Password
		}
	}
	previous := auth.Configure(config)
	t.Cleanup(func() { auth.Configure(previous) })

	app := httpx.New()
	apiauth.Register(app.Router)
	for _, register := range options.Register {
		register(app.Router)
	}
	auth.Install(app)
	if options.Setup != nil {
		options.Setup(app)
	}
	s := &Server{T: t, App: app, Repo: repository, DataDir: dataDir}
	s.handler = http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		app.ServeHTTP(w, req.WithContext(repo.WithRepository(req.Context(), repository)))
	})
	return s
}

// Context returns a context bound to the test repository (for direct
// package calls next to HTTP requests).
func (s *Server) Context() context.Context {
	return repo.WithScope(repo.WithRepository(context.Background(), s.Repo))
}

// Handler is the application bound to the test repository.
func (s *Server) Handler() http.Handler { return s.handler }

// URL starts a real HTTP server on loopback (once) and returns its base URL.
func (s *Server) URL() string {
	if s.http == nil {
		s.http = httptest.NewServer(s.handler)
		s.T.Cleanup(s.http.Close)
	}
	return s.http.URL
}

// Option modifies a request.
type Option func(*http.Request)

// Header sets a request header.
func Header(key, value string) Option {
	return func(r *http.Request) { r.Header.Set(key, value) }
}

// Headers adds every header of h (e.g. the result of Login).
func Headers(h http.Header) Option {
	return func(r *http.Request) {
		for key, values := range h {
			r.Header[key] = values
		}
	}
}

// Bearer sets Authorization: Bearer token.
func Bearer(token string) Option { return Header("Authorization", "Bearer "+token) }

// JSON sends v as an application/json body (Flask test client's json=).
func JSON(v any) Option {
	return func(r *http.Request) {
		data, err := jsonresp.Encode(v)
		if err != nil {
			panic(err)
		}
		setBody(r, data, "application/json")
	}
}

// Body sends raw bytes with a content type ("" for none).
func Body(data []byte, contentType string) Option {
	return func(r *http.Request) { setBody(r, data, contentType) }
}

// Query appends query parameters (key, value, key, value ...).
func Query(pairs ...string) Option {
	return func(r *http.Request) {
		q := r.URL.Query()
		for i := 0; i+1 < len(pairs); i += 2 {
			q.Add(pairs[i], pairs[i+1])
		}
		r.URL.RawQuery = q.Encode()
	}
}

// RemoteAddr sets the peer address ("198.51.100.7:1234").
func RemoteAddr(addr string) Option { return func(r *http.Request) { r.RemoteAddr = addr } }

func setBody(r *http.Request, data []byte, contentType string) {
	r.Body = io.NopCloser(bytes.NewReader(data))
	r.ContentLength = int64(len(data))
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
}

// Object is a test-only view of a decoded JSON response object.
type Object struct {
	values map[string]any
}

// Len returns the number of keys in the object.
func (o *Object) Len() int {
	if o == nil {
		return 0
	}
	return len(o.values)
}

// Has reports whether key is present in the object.
func (o *Object) Has(key string) bool {
	if o == nil {
		return false
	}
	_, ok := o.values[key]
	return ok
}

// Value returns the raw decoded value for key, or nil when absent.
func (o *Object) Value(key string) any {
	if o == nil {
		return nil
	}
	return o.values[key]
}

// GetString returns the string value for key, or "".
func (o *Object) GetString(key string) string {
	s, _ := o.Value(key).(string)
	return s
}

// GetBool returns the bool value for key, or false.
func (o *Object) GetBool(key string) bool {
	b, _ := o.Value(key).(bool)
	return b
}

// GetInt returns the integer value for key, or 0.
func (o *Object) GetInt(key string) int64 {
	switch n := o.Value(key).(type) {
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			return int64(f)
		}
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

// GetObject returns the nested object for key, or nil.
func (o *Object) GetObject(key string) *Object {
	m, ok := o.Value(key).(map[string]any)
	if !ok {
		return nil
	}
	return &Object{values: m}
}

// MarshalJSON encodes the underlying map for test inspection helpers.
func (o *Object) MarshalJSON() ([]byte, error) {
	if o == nil {
		return []byte("null"), nil
	}
	return json.Marshal(o.values)
}

// Result is a recorded response.
type Result struct {
	T      testing.TB
	Status int
	Header http.Header
	Body   []byte
}

// Do sends a request through the application in-process. path may carry a
// query string.
func (s *Server) Do(method, path string, options ...Option) *Result {
	s.T.Helper()
	target := "http://localhost" + path
	if _, err := url.Parse(target); err != nil {
		s.T.Fatalf("bad path %q: %v", path, err)
	}
	req := httptest.NewRequest(method, target, http.NoBody)
	req.RemoteAddr = "127.0.0.1:40000"
	for _, option := range options {
		option(req)
	}
	recorder := httptest.NewRecorder()
	s.handler.ServeHTTP(recorder, req)
	return &Result{T: s.T, Status: recorder.Code, Header: recorder.Header(), Body: recorder.Body.Bytes()}
}

// Get, Post, Put and Delete are Do with that method.
func (s *Server) Get(path string, options ...Option) *Result { return s.Do("GET", path, options...) }
func (s *Server) Post(path string, options ...Option) *Result {
	return s.Do("POST", path, options...)
}
func (s *Server) Put(path string, options ...Option) *Result { return s.Do("PUT", path, options...) }
func (s *Server) Delete(path string, options ...Option) *Result {
	return s.Do("DELETE", path, options...)
}

// Login posts the configured credentials and returns the Authorization
// header to pass with Headers(...).
func (s *Server) Login() Option {
	s.T.Helper()
	config := auth.Current()
	res := s.Post("/api/auth/login", JSON(map[string]any{"username": config.AdminUsername, "password": config.AdminPassword}))
	res.WantStatus(200)
	token := res.Object().GetString("token")
	if token == "" {
		s.T.Fatalf("login returned no token: %s", res.Body)
	}
	return Bearer(token)
}

// WantStatus fails the test unless the status matches.
func (r *Result) WantStatus(status int) *Result {
	r.T.Helper()
	if r.Status != status {
		r.T.Fatalf("status %d, want %d; body: %s", r.Status, status, r.Body)
	}
	return r
}

// JSON decodes the body (fails the test when it is not JSON).
func (r *Result) JSON() any {
	r.T.Helper()
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	var value any
	if err := dec.Decode(&value); err != nil {
		r.T.Fatalf("response is not JSON (%v): %s", err, r.Body)
	}
	return value
}

// Object decodes the body as a JSON object.
func (r *Result) Object() *Object {
	r.T.Helper()
	m, ok := r.JSON().(map[string]any)
	if !ok {
		r.T.Fatalf("response is not a JSON object: %s", r.Body)
	}
	return &Object{values: m}
}

// Text is the body as a string.
func (r *Result) Text() string { return string(r.Body) }

// WantJSON fails unless the body equals want as JSON values.
func (r *Result) WantJSON(want any) *Result {
	r.T.Helper()
	if diff := testjson.Diff(json.RawMessage(r.Body), want); diff != "" {
		r.T.Fatalf("body %s\ndiff: %s", r.Body, diff)
	}
	return r
}

// Contains reports whether the raw body contains s.
func (r *Result) Contains(s string) bool { return strings.Contains(string(r.Body), s) }
