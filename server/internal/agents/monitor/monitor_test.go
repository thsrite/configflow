package monitor

// Port of backend/test_oxidns_monitor.py: monitoring authenticates both hops
// and never exposes core configuration. The Python tests replace
// requests.Session; here a fake agents.Client answers. The two route tests
// are in internal/api/agents/oxidns_test.go.

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

func baseAgent() *jsonx.Object {
	return jsonx.Obj("id", "oxi", "name", "DNS", "host", "127.0.0.1", "port", 8082,
		"profile_id", "default", "service_type", "oxidns", "version", "1.7.0-go",
		"status", "online", "token", "agent-private-token")
}

func baseRow() *jsonx.Object {
	return jsonx.Obj("id", 42, "created_at_ms", 1792100000123, "elapsed_ms", 0,
		"client_ip", "192.0.2.1", "questions_json", []any{jsonx.Obj("name", "example.test.", "qtype", "A", "qclass", "IN")},
		"error", nil, "has_response", true, "rcode", "NOERROR", "answer_count", 1,
		"answers_json", []any{jsonx.Obj("name", "example.test.", "rr_type", "A", "ttl", 60, "payload_text", "192.0.2.2")})
}

func baseOverview() *jsonx.Object {
	return jsonx.Obj("success", true, "health", jsonx.Obj("version", "1.6.1", "status", "ok", "uptime_ms", 1234,
		"instance_id", "instance"), "capabilities", jsonx.Obj("query_recorders", []any{"recorder"}, "caches", []any{"cache"}),
		"metrics", "server_request_total{plugin_tag=\"udp\",protocol=\"udp\"} 5\n",
		"config", "private configuration must not be forwarded")
}

// fakeManager is the tests' Manager: one or more agents, a data directory
// and a client.
type fakeManager struct {
	mu      sync.Mutex
	agents  map[string]*jsonx.Object
	dataDir string
	client  agents.Client
}

func (m *fakeManager) GetAgentByID(ctx context.Context, id string, _ bool) (*jsonx.Object, error) {
	if snapshot, ok := ctx.Value(fakeSnapshotKey{}).(map[string]*jsonx.Object); ok {
		if agent := snapshot[id]; agent != nil {
			return agent.DeepCopy(), nil
		}
		return nil, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if a, ok := m.agents[id]; ok {
		return a.DeepCopy(), nil
	}
	return nil, nil
}

func (m *fakeManager) DataDir() string       { return m.dataDir }
func (m *fakeManager) Client() agents.Client { return m.client }

func (m *fakeManager) agent(id string) *jsonx.Object {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.agents[id]
}

func newManager(t *testing.T, agent *jsonx.Object) *fakeManager {
	if agent == nil {
		agent = baseAgent()
	}
	return &fakeManager{agents: map[string]*jsonx.Object{"oxi": agent}, dataDir: t.TempDir()}
}

// call is one request seen by the fake client.
type call struct {
	url    string
	query  url.Values
	rawQ   string
	header map[string]string
	req    agents.Request
}

type fakeClient struct {
	mu       sync.Mutex
	response *agents.Response
	err      error
	calls    []call
}

func (c *fakeClient) Do(_ context.Context, r agents.Request) (*agents.Response, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	u, _ := url.Parse(r.URL)
	header := map[string]string{}
	for k := range r.Header {
		header[k] = r.Header.Get(k)
	}
	c.calls = append(c.calls, call{url: u.Scheme + "://" + u.Host + u.Path, query: u.Query(), rawQ: u.RawQuery, header: header, req: r})
	if c.err != nil {
		return nil, c.err
	}
	resp := *c.response
	if r.MaxBody > 0 && int64(len(resp.Body)) > r.MaxBody+1 {
		resp.Body = resp.Body[:r.MaxBody+1]
	}
	return &resp, nil
}

func jsonResponse(data any, status int) *agents.Response {
	body, _ := jsonx.Default(data)
	return &agents.Response{StatusCode: status, Body: body}
}

func install(m *fakeManager, response *agents.Response) *fakeClient {
	c := &fakeClient{response: response}
	m.client = c
	return c
}

func wantMonitorError(t *testing.T, err error, code string, status int) *Error {
	t.Helper()
	var e *Error
	if !errors.As(err, &e) || e.Code != code || (status != 0 && e.Status != status) {
		t.Fatalf("got %v (%#v), want %s %d", err, e, code, status)
	}
	return e
}

func args(pairs ...string) [][2]string {
	out := [][2]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, [2]string{pairs[i], pairs[i+1]})
	}
	return out
}

var ctx = context.Background()

func TestOverviewUsesRegisteredAgentAndExcludesCredentials(t *testing.T) {
	m := newManager(t, nil)
	c := install(m, jsonResponse(baseOverview(), 200))
	result, err := Overview(ctx, m, "oxi", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, metricsOf(result).Value("requests_total"), 5)
	wantJSON(t, metricsOf(result).Value("avg_latency_ms"), nil)
	wantJSON(t, result.Value("recorders"), []any{"recorder"})
	wantJSON(t, result.GetObject("core").Value("instance_id"), "instance")
	serialized, _ := jsonx.Default(result)
	if strings.Contains(string(serialized), "agent-private-token") || strings.Contains(string(serialized), "private configuration") {
		t.Fatal(string(serialized))
	}
	got := c.calls[0]
	if got.url != "http://127.0.0.1:8082/api/oxidns/overview" || got.header["Authorization"] != "Bearer agent-private-token" ||
		!got.req.NoRedirects || got.req.MaxBody != MaxResponseBytes || !got.req.NoProxy {
		t.Fatalf("%+v", got)
	}
}

func TestUpstreamErrorsAreBoundedAndSanitized(t *testing.T) {
	cases := []struct {
		data   any
		status int
		raw    []byte
		code   string
	}{
		{jsonx.Obj("message", "core-password"), 302, nil, "INVALID_RESPONSE"},
		{jsonx.Obj("message", "core-password"), 401, nil, "AGENT_AUTH_FAILED"},
		{jsonx.Obj("message", "core-password"), 403, nil, "AGENT_AUTH_FAILED"},
		{nil, 404, []byte("404 page not found"), "UPGRADE_REQUIRED"},
		{jsonx.Obj("success", false, "code", "CORE_AUTH_FAILED", "message", "core-password"), 502, nil, "CORE_AUTH_FAILED"},
		{jsonx.Obj("success", false, "code", "RECORD_NOT_FOUND", "message", "private trace"), 404, nil, "RECORD_NOT_FOUND"},
		{jsonx.Obj("success", false, "code", "TLS_UNSUPPORTED", "message", "private config"), 409, nil, "TLS_UNSUPPORTED"},
		{nil, 200, []byte("not json with password"), "INVALID_RESPONSE"},
		{[]any{}, 200, nil, "INVALID_RESPONSE"},
		{nil, 200, []byte(strings.Repeat("x", MaxResponseBytes+1)), "INVALID_RESPONSE"},
	}
	for _, tc := range cases {
		m := newManager(t, nil)
		response := jsonResponse(tc.data, tc.status)
		if tc.raw != nil {
			response = &agents.Response{StatusCode: tc.status, Body: tc.raw}
		}
		install(m, response)
		_, err := Overview(ctx, m, "oxi", nil)
		e := wantMonitorError(t, err, tc.code, 0)
		for _, secret := range []string{"core-password", "private trace", "private config"} {
			if strings.Contains(e.Error(), secret) {
				t.Fatal(e)
			}
		}
	}
}

func TestTransportExceptionDoesNotLeakCredentials(t *testing.T) {
	m := newManager(t, nil)
	c := install(m, nil)
	c.err = &agents.TransportError{Err: errors.New("http://user:secret@example.test")}
	_, err := Overview(ctx, m, "oxi", nil)
	e := wantMonitorError(t, err, "AGENT_UNREACHABLE", 0)
	if strings.Contains(e.Error(), "secret") {
		t.Fatal(e)
	}
}

func TestAgentGuardsPrecedeNetwork(t *testing.T) {
	cases := []struct {
		key   string
		value any
		code  string
	}{
		{"version", "1.6.1-go", "UPGRADE_REQUIRED"}, {"service_type", "mihomo", "WRONG_SERVICE"},
		{"host", "localhost/private", "AGENT_UNREACHABLE"}, {"host", "user@localhost", "AGENT_UNREACHABLE"},
		{"port", 0, "AGENT_UNREACHABLE"},
	}
	for _, tc := range cases {
		m := newManager(t, nil)
		c := install(m, jsonResponse(baseOverview(), 200))
		m.agent("oxi").Set(tc.key, tc.value)
		_, err := Overview(ctx, m, "oxi", nil)
		wantMonitorError(t, err, tc.code, 0)
		if len(c.calls) != 0 {
			t.Fatal(tc)
		}
	}
}

func TestQueryValidationRejectsInjectionBeforeAgentCall(t *testing.T) {
	for _, params := range [][][2]string{
		args("url", "http://elsewhere"), args("recorder", "../config"), args("limit", "0"), args("limit", "101"),
		args("qname", "ok", "qname", "different"), args("cursor", "page2"), args("since_ms", "-1"),
		args("since_ms", "100", "until_ms", "99"), args("status", "delete"), args("qtype", "A&flush=true"),
		args("qname", "a\nb"), args("qname", strings.Repeat("x", 1025)), args("cursor", "9999999999999999:1"),
	} {
		m := newManager(t, nil)
		c := install(m, jsonResponse(jsonx.NewObject(), 200))
		_, err := Queries(ctx, m, "oxi", params)
		wantMonitorError(t, err, "INVALID_QUERY", 0)
		if len(c.calls) != 0 {
			t.Fatal(params)
		}
	}
}

func TestQueriesPreserveCursorFilterAndZeroLatency(t *testing.T) {
	m := newManager(t, nil)
	c := install(m, jsonResponse(jsonx.Obj("ok", true, "records", []any{baseRow()}, "next_cursor", "1792100000123:42"), 200))
	result, err := Queries(ctx, m, "oxi", args("recorder", "recorder", "qname", "example", "qtype", "aaaa", "limit", "50"))
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, result.GetList("records")[0].(*jsonx.Object).Value("elapsed_ms"), 0)
	wantJSON(t, result.Value("next_cursor"), "1792100000123:42")
	if result.Has("total") || c.calls[0].query.Get("qtype") != "AAAA" {
		t.Fatal(jsonx.Repr(result), c.calls[0].query)
	}
}

func TestQueryDetailIsFlatAndReadOnly(t *testing.T) {
	r := baseRow()
	r.Set("steps", []any{jsonx.Obj("event_index", 0, "sequence_tag", "main", "node_index", 0, "kind", "executor",
		"tag", "cache", "outcome", "continued")})
	r.Set("private_field", "omit")
	m := newManager(t, nil)
	c := install(m, jsonResponse(jsonx.Obj("ok", true, "record", r), 200))
	result, err := Query(ctx, m, "oxi", "42", args("recorder", "recorder"))
	if err != nil {
		t.Fatal(err)
	}
	record := result.GetObject("record")
	wantJSON(t, record.Value("id"), 42)
	wantJSON(t, record.GetList("steps")[0].(*jsonx.Object).Value("tag"), "cache")
	if record.Has("private_field") || !strings.HasSuffix(c.calls[0].url, "/queries/42") {
		t.Fatal(jsonx.Repr(record), c.calls[0].url)
	}
	if _, ok := c.calls[0].header["X-Configflow-Query-Sources"]; ok {
		t.Fatal("detail sent the sources header")
	}
}

func TestListPreservesAgentSourceSummaryWithoutTraceRequests(t *testing.T) {
	for _, source := range []string{"local", "remote", "fallback", "pinned", "cache", "hosts", "blocked",
		"rejected", "unknown", "unrecorded", "error", "no_response", "ipv4_preferred"} {
		r := baseRow()
		r.Set("resolution_source", source)
		r.Set("source_credentials", "never forward")
		m := newManager(t, nil)
		c := install(m, jsonResponse(jsonx.Obj("ok", true, "records", []any{r}, "next_cursor", nil), 200))
		result, err := Queries(ctx, m, "oxi", args("recorder", "recorder"))
		if err != nil {
			t.Fatal(err)
		}
		first := result.GetList("records")[0].(*jsonx.Object)
		if first.Value("resolution_source") != source || first.Has("source_credentials") {
			t.Fatal(jsonx.Repr(first))
		}
		if len(c.calls) != 1 || !strings.HasSuffix(c.calls[0].url, "/queries") || c.calls[0].header["X-Configflow-Query-Sources"] != "1" {
			t.Fatal(c.calls)
		}
	}
}

func TestPinnedResolutionSourceAccepted(t *testing.T) {
	r := baseRow()
	r.Set("resolution_source", "pinned")
	out, err := record(r, false)
	if err != nil || out.Value("resolution_source") != "pinned" {
		t.Fatal(err, jsonx.Repr(out))
	}
}

func TestInvalidSourceSummaryIsIgnoredForLegacyDetailFallback(t *testing.T) {
	for _, source := range []any{"private://credentials", "constructor", jsonx.NewObject(), []any{}, nil} {
		r := baseRow()
		r.Set("resolution_source", source)
		out, err := record(r, false)
		if err != nil || out.Has("resolution_source") {
			t.Fatal(source, err)
		}
	}
}

func TestOverviewTreatsUnrepresentableUptimeAsUnknown(t *testing.T) {
	huge, _ := jsonx.ParseNumber("1" + strings.Repeat("0", 400))
	for _, uptime := range []any{huge, jsonx.Int(MaxInteger + 1)} {
		data := baseOverview()
		data.GetObject("health").Set("uptime_ms", uptime)
		m := newManager(t, nil)
		install(m, jsonResponse(data, 200))
		result, err := Overview(ctx, m, "oxi", nil)
		if err != nil || result.GetObject("core").Value("uptime_ms") != nil {
			t.Fatal(err, jsonx.Repr(result))
		}
	}
}

func TestRecordRejectsUnrepresentableTimesWithoutOverflow(t *testing.T) {
	huge, _ := jsonx.ParseNumber("1" + strings.Repeat("0", 400))
	for _, field := range []string{"created_at_ms", "elapsed_ms"} {
		for _, value := range []any{huge, jsonx.Int(MaxInteger + 1)} {
			r := baseRow()
			r.Set(field, value)
			m := newManager(t, nil)
			install(m, jsonResponse(jsonx.Obj("ok", true, "records", []any{r}, "next_cursor", nil), 200))
			_, err := Queries(ctx, m, "oxi", args("recorder", "recorder"))
			wantMonitorError(t, err, "INVALID_RESPONSE", 502)
		}
	}
}

func TestRcodeFilterMapsMnemonicsAndPreservesOfficialText(t *testing.T) {
	for _, tc := range [][2]string{
		{"NOERROR", "No Error"}, {"noerror", "No Error"}, {"FormErr", "Form Error"},
		{"SERVFAIL", "Server Failure"}, {"NXDOMAIN", "Non-Existent Domain"},
		{"NOTIMP", "Not Implemented"}, {"REFUSED", "Query Refused"},
		{"YXDOMAIN", "Name should not exist"}, {"YXRRSET", "RR Set should not exist"},
		{"NXRRSET", "RR Set does not exist"}, {"NOTAUTH", "Not authorized"},
		{"NOTZONE", "Name not in zone"}, {"BADVERS", "Bad OPT Version"},
		{"BADSIG", "TSIG Signature Failure"}, {"BADKEY", "Key not recognized"},
		{"BADTIME", "Signature out of time window"}, {"BADMODE", "Bad TKEY mode"},
		{"BADNAME", "Duplicate key name"}, {"BADALG", "Algorithm not supported"},
		{"BADTRUNC", "Bad truncation"}, {"BADCOOKIE", "Bad server cookie"},
		{"No Error", "No Error"}, {"  Non-Existent Domain  ", "Non-Existent Domain"},
		{"server failure", "server failure"}, {"RCODE15", "RCODE15"},
		{"Future Response", "Future Response"},
	} {
		r := baseRow()
		r.Set("rcode", "No Error")
		m := newManager(t, nil)
		c := install(m, jsonResponse(jsonx.Obj("ok", true, "records", []any{r}, "next_cursor", nil), 200))
		result, err := Queries(ctx, m, "oxi", args("recorder", "recorder", "rcode", tc[0]))
		if err != nil {
			t.Fatal(err)
		}
		if c.calls[0].query.Get("rcode") != tc[1] || result.GetList("records")[0].(*jsonx.Object).Value("rcode") != "No Error" {
			t.Fatal(tc, c.calls[0].query)
		}
	}
}

func TestRcodeFilterRejectsInvalidCharactersBeforeNetwork(t *testing.T) {
	for _, rcode := range []string{"No Error&flush=true", "../config", "No Error?", "No Error#", "No Error\x7f",
		"No Error\n", "No\tError", "拒绝", strings.Repeat("x", 65)} {
		m := newManager(t, nil)
		c := install(m, jsonResponse(jsonx.NewObject(), 200))
		_, err := Queries(ctx, m, "oxi", args("rcode", rcode))
		wantMonitorError(t, err, "INVALID_QUERY", 400)
		if len(c.calls) != 0 {
			t.Fatal(rcode)
		}
	}
}

func clientsManager(t *testing.T) *fakeManager {
	m := newManager(t, nil)
	m.agent("oxi").Set("version", "1.7.1-go")
	return m
}

func baseClients() *jsonx.Object {
	return jsonx.Obj("ok", true, "recorder", "recorder", "sample_size", 3,
		"rows", []any{jsonx.Obj("key", "192.168.0.2", "count", 2, "share", 2.0/3), jsonx.Obj("key", "2001:db8::2", "count", 1, "share", 1.0/3)},
		"config", "private configuration", "token", "core-private-token")
}

func TestClientsUseIndependentSearchAndReturnOnlySafeDTO(t *testing.T) {
	m := clientsManager(t)
	c := install(m, jsonResponse(baseClients(), 200))
	result, err := Clients(ctx, m, "oxi", args("recorder", "recorder", "since_ms", "100", "until_ms", "200", "search", "192.168."))
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, result, jsonx.Obj("success", true, "recorder", "recorder", "sample_size", 3, "sample_limited", false,
		"clients", []any{jsonx.Obj("ip", "192.168.0.2", "count", 2), jsonx.Obj("ip", "2001:db8::2", "count", 1)}))
	got := c.calls[0]
	if got.url != "http://127.0.0.1:8082/api/oxidns/clients" || got.rawQ != "recorder=recorder&since_ms=100&until_ms=200&search=192.168." ||
		got.header["Authorization"] != "Bearer agent-private-token" || !got.req.NoRedirects || !got.req.NoProxy {
		t.Fatalf("%+v", got)
	}
	serialized, _ := jsonx.Default(result)
	for _, secret := range []string{"agent-private-token", "core-private-token", "private configuration"} {
		if strings.Contains(string(serialized), secret) {
			t.Fatal(secret)
		}
	}
}

func TestClientsSampleBoundaryAndResolvedRecorder(t *testing.T) {
	for _, tc := range []struct {
		sample  int
		rows    []any
		limited bool
	}{
		{0, []any{}, false},
		{10000, []any{jsonx.Obj("key", "192.0.2.1", "count", 10000, "share", 1)}, true},
	} {
		m := clientsManager(t)
		install(m, jsonResponse(jsonx.Obj("ok", true, "recorder", "auto-records", "sample_size", tc.sample, "rows", tc.rows), 200))
		result, err := Clients(ctx, m, "oxi", nil)
		if err != nil {
			t.Fatal(err)
		}
		if result.Value("sample_limited") != tc.limited || result.GetInt("sample_size") != int64(tc.sample) || result.Value("recorder") != "auto-records" {
			t.Fatal(jsonx.Repr(result))
		}
	}
}

func TestClientsRejectUnrelatedFiltersAndBadSearchBeforeNetwork(t *testing.T) {
	for _, params := range [][][2]string{
		args("qname", "example"), args("qtype", "A"), args("client_ip", "192.0.2.1"), args("limit", "20"),
		args("cursor", "100:1"), args("status", "all"), args("search", "a", "search", "b"),
		args("recorder", "../config"), args("recorder", "a..b"), args("search", "a\nb"),
		args("search", "\x7f"), args("search", "\u0085"), args("search", strings.Repeat("x", 65)), args("search", strings.Repeat("测", 22)),
		args("since_ms", "-1"), args("since_ms", "101", "until_ms", "100"), args("agent_id", "another"),
	} {
		m := clientsManager(t)
		c := install(m, jsonResponse(baseClients(), 200))
		_, err := Clients(ctx, m, "oxi", params)
		wantMonitorError(t, err, "INVALID_QUERY", 400)
		if len(c.calls) != 0 {
			t.Fatal(params)
		}
	}
}

func TestClientsAcceptBoundedUTF8Search(t *testing.T) {
	m := clientsManager(t)
	c := install(m, jsonResponse(baseClients(), 200))
	if _, err := Clients(ctx, m, "oxi", args("search", "测试")); err != nil {
		t.Fatal(err)
	}
	if c.calls[0].query.Get("search") != "测试" || len(c.calls[0].query) != 1 {
		t.Fatal(c.calls[0].query)
	}
}

func TestClientsValidateCoreResponseWithoutLeaking(t *testing.T) {
	huge, _ := jsonx.ParseNumber("1" + strings.Repeat("0", 400))
	many := make([]any, 10001)
	for i := range many {
		many[i] = jsonx.Obj("key", "192.0.2.1", "count", 1)
	}
	for _, change := range []*jsonx.Object{
		jsonx.Obj("sample_size", true), jsonx.Obj("sample_size", -1), jsonx.Obj("sample_size", 10001), jsonx.Obj("sample_size", huge),
		jsonx.Obj("recorder", "other"), jsonx.Obj("recorder", "../secret"), jsonx.Obj("rows", nil),
		jsonx.Obj("rows", []any{jsonx.Obj("key", "core-password", "count", 3)}),
		jsonx.Obj("rows", []any{jsonx.Obj("key", "192.0.2.1", "count", true)}),
		jsonx.Obj("rows", []any{jsonx.Obj("key", "192.0.2.1", "count", 0)}),
		jsonx.Obj("rows", []any{jsonx.Obj("key", "192.0.2.1", "count", 4)}),
		jsonx.Obj("rows", []any{jsonx.Obj("key", "192.0.2.1", "count", 2), jsonx.Obj("key", "192.0.2.1", "count", 1)}),
		jsonx.Obj("rows", []any{jsonx.Obj("key", "192.0.2.1", "count", 2)}),
		jsonx.Obj("rows", many),
	} {
		data := baseClients()
		data.Update(change)
		m := clientsManager(t)
		install(m, jsonResponse(data, 200))
		_, err := Clients(ctx, m, "oxi", args("recorder", "recorder"))
		e := wantMonitorError(t, err, "INVALID_RESPONSE", 502)
		if strings.Contains(e.Error(), "core-password") {
			t.Fatal(e)
		}
	}
}

func TestClientsAndExactIPRequireNewAgentButOldQueriesKeepWorking(t *testing.T) {
	m := newManager(t, nil)
	c := install(m, jsonResponse(jsonx.Obj("ok", true, "records", []any{}, "next_cursor", nil), 200))
	result, err := Queries(ctx, m, "oxi", nil)
	if err != nil || len(result.GetList("records")) != 0 {
		t.Fatal(err)
	}
	c.calls = nil
	for _, op := range []func() error{
		func() error { _, err := Clients(ctx, m, "oxi", nil); return err },
		func() error { _, err := Queries(ctx, m, "oxi", args("client_ip", "192.0.2.1")); return err },
	} {
		e := wantMonitorError(t, op(), "UPGRADE_REQUIRED", 0)
		if !strings.Contains(e.Error(), "1.7.1-go") || len(c.calls) != 0 {
			t.Fatal(e)
		}
	}
	m2 := clientsManager(t)
	c2 := install(m2, jsonResponse(jsonx.Obj("ok", true, "records", []any{}, "next_cursor", nil), 200))
	if _, err := Queries(ctx, m2, "oxi", args("client_ip", "2001:0DB8:0:0:0:0:0:2")); err != nil {
		t.Fatal(err)
	}
	if c2.calls[0].query.Get("client_ip") != "2001:db8::2" {
		t.Fatal(c2.calls[0].query)
	}
}

func TestExactClientFilterRequiresACompleteIP(t *testing.T) {
	for _, ip := range []string{"192.168.", "192.0.2.1:53", "localhost", "fe80::2%eth0"} {
		m := clientsManager(t)
		c := install(m, jsonResponse(jsonx.NewObject(), 200))
		_, err := Queries(ctx, m, "oxi", args("client_ip", ip))
		wantMonitorError(t, err, "INVALID_QUERY", 0)
		if len(c.calls) != 0 {
			t.Fatal(ip)
		}
	}
}

func TestMissingClientsEndpointReportsCorrectUpgrade(t *testing.T) {
	for _, response := range []*agents.Response{
		{StatusCode: 404, Body: []byte("404 page not found")},
		jsonResponse(jsonx.Obj("code", "NOT_FOUND"), 404),
	} {
		m := clientsManager(t)
		install(m, response)
		_, err := Clients(ctx, m, "oxi", nil)
		e := wantMonitorError(t, err, "UPGRADE_REQUIRED", 0)
		if !strings.Contains(e.Error(), "1.7.1-go") {
			t.Fatal(e)
		}
	}
}

// --- Go-specific checks of the Python behaviour the tests above rely on ---

func TestOverviewRejectsNonTextMetrics(t *testing.T) {
	for _, value := range []any{nil, []any{}, 42, jsonx.NewObject()} {
		data := baseOverview()
		data.Set("metrics", value)
		m := newManager(t, nil)
		install(m, jsonResponse(data, 200))
		_, err := Overview(ctx, m, "oxi", nil)
		wantMonitorError(t, err, "INVALID_RESPONSE", 502)
	}
}

func TestErrorCodeThatIsAListIsAnUnreachableAgent(t *testing.T) {
	// `code not in MESSAGES` raises TypeError for a list, caught as
	// AGENT_UNREACHABLE.
	m := newManager(t, nil)
	install(m, jsonResponse(jsonx.Obj("success", false, "code", []any{"x"}), 502))
	_, err := Overview(ctx, m, "oxi", nil)
	wantMonitorError(t, err, "AGENT_UNREACHABLE", 502)
}

func TestBodyReadFailureAfterStatusChecks(t *testing.T) {
	m := newManager(t, nil)
	install(m, &agents.Response{StatusCode: 401, BodyError: errors.New("reset")})
	_, err := Overview(ctx, m, "oxi", nil)
	wantMonitorError(t, err, "AGENT_AUTH_FAILED", 502)
	install(m, &agents.Response{StatusCode: 200, Body: []byte("{"), BodyError: errors.New("reset")})
	_, err = Overview(ctx, m, "oxi", nil)
	wantMonitorError(t, err, "AGENT_UNREACHABLE", 502)
}

type fakeSnapshotKey struct{}

func (m *fakeManager) WithReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	snapshot := make(map[string]*jsonx.Object, len(m.agents))
	for id, agent := range m.agents {
		snapshot[id] = agent.DeepCopy()
	}
	return fn(context.WithValue(ctx, fakeSnapshotKey{}, snapshot))
}
