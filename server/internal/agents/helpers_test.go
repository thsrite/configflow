package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"sync"
	"testing"
	"time"

	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/repo"
	"configflow/internal/testjson"
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
	body, err := jsonresp.EncodeUTF8(payload)
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
	agent *model.Agent
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
	registered := e.register(map[string]any{"name": "test", "host": "agent.test", "port": 8080, "service_type": "mihomo"}, "")
	e.agent = e.get(registered.ID)
	return e
}

func (e *env) register(data map[string]any, token string) RegistrationResult {
	e.t.Helper()
	raw, err := json.Marshal(data)
	if err != nil {
		e.t.Fatal(err)
	}
	input, err := DecodeRegistrationInput(raw)
	if err != nil {
		e.t.Fatal(err)
	}
	result, err := e.m.RegisterIdentity(e.ctx, input, RegistrationOptions{ExistingToken: token})
	if err != nil {
		e.t.Fatal(err)
	}
	return result
}

func (e *env) get(id string) *model.Agent {
	e.t.Helper()
	agent, err := e.m.ReadAgent(e.ctx, id, true)
	if err != nil {
		e.t.Fatal(err)
	}
	return agent
}

func (e *env) id() string { return e.agent.ID }

func (e *env) system() *model.System {
	e.t.Helper()
	system, err := e.repo.System(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	return system
}

func (e *env) stored(id string) *model.Agent {
	sys := e.system()
	for i := range sys.Agents {
		if text, ok := storedText(model.EncodeMember(&sys.Agents[i], "id")); ok && text == id {
			return &sys.Agents[i]
		}
	}
	return nil
}

// revision counts registry writes: every committed transaction of this
// process, configuration or Agent state (design/o17-runtime-state.md: the
// system revision no longer advances for Agent state edits).
func (e *env) revision() int64 { return e.repo.CommitCount() }

// editSystem updates the system section atomically.
func (e *env) editSystem(fn func(system *model.System)) {
	e.t.Helper()
	err := e.repo.UpdateSystem(e.ctx, func(_ context.Context, system *model.System) error {
		fn(system)
		return nil
	})
	if err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) firstAgent(system *model.System) *model.Agent {
	return &system.Agents[0]
}

func (e *env) createProfile(id string) {
	e.t.Helper()
	if _, err := e.repo.CreateProfileRecord(e.ctx, repo.ProfileCreation{ID: id, Name: id}, ""); err != nil {
		e.t.Fatal(err)
	}
}

func must[T any](t testing.TB) func(T, error) T {
	return func(value T, err error) T {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	raw, err := jsonresp.EncodeUTF8(v)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func parseJSONVal(t testing.TB, raw []byte) any {
	t.Helper()
	val, err := testjson.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return val
}

func diffJSONValues(t testing.TB, want, got any) string {
	t.Helper()
	wVal := parseJSONVal(t, mustJSON(t, want))
	gVal := parseJSONVal(t, mustJSON(t, got))
	return testjson.Diff(wVal, gVal)
}

func compactRawJSON(t testing.TB, raw json.RawMessage) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var buf bytes.Buffer
	writeCompactToken(t, dec, &buf)
	return buf.Bytes()
}

func writeCompactToken(t testing.TB, dec *json.Decoder, buf *bytes.Buffer) {
	t.Helper()
	tok, err := dec.Token()
	if err != nil {
		t.Fatal(err)
	}
	switch v := tok.(type) {
	case json.Delim:
		switch v {
		case '{':
			buf.WriteByte('{')
			first := true
			for dec.More() {
				if !first {
					buf.WriteByte(',')
				}
				first = false
				key, err := dec.Token()
				if err != nil {
					t.Fatal(err)
				}
				buf.Write(mustJSON(t, key))
				buf.WriteByte(':')
				writeCompactToken(t, dec, buf)
			}
			if _, err := dec.Token(); err != nil {
				t.Fatal(err)
			}
			buf.WriteByte('}')
		case '[':
			buf.WriteByte('[')
			first := true
			for dec.More() {
				if !first {
					buf.WriteByte(',')
				}
				first = false
				writeCompactToken(t, dec, buf)
			}
			if _, err := dec.Token(); err != nil {
				t.Fatal(err)
			}
			buf.WriteByte(']')
		default:
			t.Fatalf("unexpected delim %v", v)
		}
	case json.Number:
		buf.WriteString(v.String())
	default:
		buf.Write(mustJSON(t, v))
	}
}

func orderedJSONKeys(t testing.TB, raw []byte) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil || tok != json.Delim('{') {
		t.Fatalf("expected JSON object: %v %v", tok, err)
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, k.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

func cloneRuntimeFields(f runtimeFields) runtimeFields {
	var out runtimeFields
	for i := range f {
		out[i] = bytes.Clone(f[i])
	}
	return out
}

// memberJSON reads one member of a record as written: a DeploymentReply
// member as received, a plain record member as encoded.
func memberJSON(t testing.TB, a any, name string) (json.RawMessage, bool) {
	if reply, ok := a.(interface {
		MemberJSON(string) (json.RawMessage, bool)
	}); ok {
		return reply.MemberJSON(name)
	}
	raw, present, err := model.EncodeMember(a, name)
	if err != nil && t != nil {
		t.Fatal(err)
	}
	return raw, present
}

func memberPresent(a any, name string) bool {
	_, present := memberJSON(nil, a, name)
	return present
}

func memberVal(t testing.TB, a any, name string) (any, bool) {
	t.Helper()
	raw, present := memberJSON(t, a, name)
	if !present {
		return nil, false
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v, true
}

// storedText reads a JSON string member.
func storedText(raw json.RawMessage, present bool, err error) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if err != nil || !present || len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var text string
	return text, json.Unmarshal(raw, &text) == nil
}

// deploymentFlagTrue reports a journal flag member that is true.
func deploymentFlagTrue(d any, name string) bool {
	raw, present := memberJSON(nil, d, name)
	var flag *bool
	return present && json.Unmarshal(raw, &flag) == nil && flag != nil && *flag
}

// originalDeploymentText reads a journal text member.
func originalDeploymentText(d any, name string) string {
	raw, present := memberJSON(nil, d, name)
	text, _ := storedText(raw, present, nil)
	return text
}

func runtimeEntriesCount(s *RuntimeState) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

func runtimeEntryCopy(s *RuntimeState, id string) (heartbeatEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[id]
	if !ok {
		return heartbeatEntry{}, false
	}
	return heartbeatEntry{
		HeartbeatAt:   entry.HeartbeatAt,
		LastHeartbeat: entry.LastHeartbeat,
		Fields:        cloneRuntimeFields(entry.Fields),
	}, true
}

func setRuntimeEntry(s *RuntimeState, id string, entry heartbeatEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries[id] = heartbeatEntry{
		HeartbeatAt:   entry.HeartbeatAt,
		LastHeartbeat: entry.LastHeartbeat,
		Fields:        cloneRuntimeFields(entry.Fields),
	}
}

// replyJournal is the typed journal view of a deployment response.
func replyJournal(t testing.TB) func(*DeploymentReply, error) *model.Deployment {
	return func(reply *DeploymentReply, err error) *model.Deployment {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		if reply == nil {
			return nil
		}
		return &reply.Deployment
	}
}

// nonTextMember reports whether raw holds, at any depth, one of the named
// members with a value that is neither text nor null. Python reference rows
// with such values are an accepted O18 T1b difference: Agent records read a
// number as its text and other types as the zero value.
func nonTextMember(raw json.RawMessage, names ...string) bool {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return false
	}
	var walk func(any) bool
	walk = func(v any) bool {
		switch v := v.(type) {
		case map[string]any:
			for key, member := range v {
				if slices.Contains(names, key) {
					if _, text := member.(string); !text && member != nil {
						return true
					}
				}
				if walk(member) {
					return true
				}
			}
		case []any:
			for _, item := range v {
				if walk(item) {
					return true
				}
			}
		}
		return false
	}
	return walk(value)
}
