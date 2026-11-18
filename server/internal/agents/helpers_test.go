package agents

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// fakeAgent is the Client of the tests: handle answers every request.
type fakeAgent struct {
	mu       sync.Mutex
	handle   func(req Request) (*Response, error)
	requests []Request
}

func (f *fakeAgent) Do(_ context.Context, req Request) (*Response, error) {
	f.mu.Lock()
	f.requests = append(f.requests, req)
	handle := f.handle
	f.mu.Unlock()
	if handle == nil {
		panic("unexpected Agent request " + req.Method + " " + req.URL)
	}
	return handle(req)
}

func (f *fakeAgent) set(handle func(req Request) (*Response, error)) {
	f.mu.Lock()
	f.handle = handle
	f.mu.Unlock()
}

func (f *fakeAgent) urls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		out = append(out, r.URL)
	}
	return out
}

// reply is the test's response(payload, status).
func reply(payload any, status int) *Response {
	body, err := jsonx.Default(payload)
	if err != nil {
		panic(err)
	}
	return &Response{StatusCode: status, Header: http.Header{}, Body: body}
}

func always(payload any, status int) func(Request) (*Response, error) {
	return func(Request) (*Response, error) { return reply(payload, status), nil }
}

func transportFailure(text string) func(Request) (*Response, error) {
	return func(Request) (*Response, error) { return nil, &TransportError{Err: errString(text)} }
}

type errString string

func (e errString) Error() string { return string(e) }

type env struct {
	t     *testing.T
	ctx   context.Context
	repo  *repo.Repository
	m     *Manager
	agent *jsonx.Object
	fake  *fakeAgent
	dir   string
}

func openRepo(t *testing.T, dir string) *repo.Repository {
	t.Helper()
	r, err := repo.OpenDefault(dir)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func newManager(t *testing.T, r *repo.Repository) *Manager {
	t.Helper()
	m, err := New(context.Background(), r)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// newEnv is test_agent_deployments.env: one mihomo Agent at agent.test:8080.
func newEnv(t *testing.T) *env {
	t.Helper()
	ResetRuntime()
	ResetHooks()
	t.Cleanup(func() { ResetRuntime(); ResetHooks() })
	dir := seededAgentDirectory(t)
	r := openRepo(t, dir)
	m := newManager(t, r)
	fake := &fakeAgent{}
	m.SetClient(fake)
	m.sleep = func(time.Duration) {}
	e := &env{t: t, ctx: context.Background(), repo: r, m: m, fake: fake, dir: dir}
	registered := e.register(jsonx.Obj("name", "test", "host", "agent.test", "port", 8080, "service_type", "mihomo"), nil)
	e.agent = e.get(registered.GetString("id"))
	return e
}

func (e *env) register(data *jsonx.Object, token any) *jsonx.Object {
	e.t.Helper()
	result, err := e.m.RegisterAgent(e.ctx, data.DeepCopy(), token, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return result
}

func (e *env) get(id string) *jsonx.Object {
	e.t.Helper()
	agent, err := e.m.GetAgentByID(e.ctx, id, true)
	if err != nil {
		e.t.Fatal(err)
	}
	return agent
}

func (e *env) id() string { return e.agent.GetString("id") }

func (e *env) system() *jsonx.Object {
	e.t.Helper()
	system, err := e.repo.GetSystem(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return system
}

func (e *env) stored(id string) *jsonx.Object {
	agent, _ := findAgent(e.system().GetList("agents"), id)
	return agent
}

func (e *env) revision() int64 { return e.system().GetInt("_revision") }

// editSystem is repository.update_system_transaction(fn).
func (e *env) editSystem(fn func(system *jsonx.Object)) {
	e.t.Helper()
	_, err := e.repo.UpdateSystemTransaction(e.ctx, func(_ context.Context, system *jsonx.Object) (*jsonx.Object, error) {
		fn(system)
		return nil, nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) firstAgent(system *jsonx.Object) *jsonx.Object {
	return system.GetList("agents")[0].(*jsonx.Object)
}

func (e *env) createProfile(id string) {
	e.t.Helper()
	if _, err := e.repo.CreateProfile(e.ctx, jsonx.Obj("id", id, "name", id), ""); err != nil {
		e.t.Fatal(err)
	}
}

func must[T any](t *testing.T) func(T, error) T {
	return func(value T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
}
