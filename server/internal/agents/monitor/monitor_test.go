package monitor

// Port of backend/test_oxidns_monitor.py: monitoring authenticates both hops
// and never exposes core configuration. The Python tests replace
// requests.Session; here a fake agents.Client answers. The two route tests
// are in internal/api/agents/oxidns_test.go.

import (
	"configflow/internal/agents"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"errors"
	"golang.org/x/text/encoding/charmap"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
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

// Compatibility codecs are test-only; production uses the native owners.

// Checkpoint is the legacy hook codec; invalid optional revisions are ignored.
func Checkpoint(ctx context.Context, m Manager, id, deploymentID string, value any) error {
	revision, _ := value.(string)
	return CheckpointStatistics(ctx, registryFor(m), id, deploymentID, revision)
}

// Complete is the legacy terminal-status codec.
func Complete(ctx context.Context, m Manager, id, deploymentID string, value any) error {
	switch value.(type) {
	case []any, *jsonx.Object:
		return errors.New(msgPublicationDamaged)
	}
	status, _ := value.(string)
	return CompleteStatistics(ctx, registryFor(m), id, deploymentID, status)
}

// verifyPublication is the legacy read-snapshot codec used by boundary tests.
func verifyPublication(ctx context.Context, m Manager, id, digest string, raw *jsonx.Object) error {
	var pending PendingPublication
	if err := metricJSON(raw, &pending); err != nil {
		return err
	}
	return verifyNativePublication(ctx, registryFor(m), id, digest, &pending)
}

func metricJSON(value any, target any) error {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return err
	}
	return jsondecode.Unmarshal(raw, target)
}
func metricObject(value any) *jsonx.Object {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	decoded, err := jsonx.Parse(raw)
	if err != nil {
		return nil
	}
	return decoded.(*jsonx.Object)
}
func nativeObservations(value *jsonx.Object) *Observations {
	var out Observations
	_ = metricJSON(value, &out)
	return &out
}

// ParseMetrics converts native observations for pending legacy callers.
func ParseMetrics(text string) (*jsonx.Object, error) {
	o, err := ReadMetrics(text)
	if err != nil {
		return nil, err
	}
	return metricObject(o), nil
}

// MetricsCountersRestarted reports reset counters through the legacy codec.
func MetricsCountersRestarted(a, b *jsonx.Object) bool {
	return CountersRestarted(nativeObservations(a), nativeObservations(b))
}

// ApplyMetricsBaseline adapts existing dashboard callers.
func ApplyMetricsBaseline(a, b *jsonx.Object) (*jsonx.Object, bool, error) {
	var x, y Observations
	if err := metricJSON(a, &x); err != nil {
		return nil, false, err
	}
	if err := metricJSON(b, &y); err != nil {
		return nil, false, err
	}
	out, partial, err := ApplyBaseline(&x, &y)
	if err != nil {
		return nil, false, err
	}
	return metricObject(out), partial, nil
}

// ZeroBaseline adapts existing dashboard callers.
func ZeroBaseline(a *jsonx.Object) *jsonx.Object {
	return metricObject(ZeroObservations(nativeObservations(a)))
}

// AddObservations adapts existing dashboard callers.
func AddObservations(a, b *jsonx.Object) *jsonx.Object {
	return metricObject(CarryObservations(nativeObservations(a), nativeObservations(b)))
}

// SummarizeObservations adapts existing dashboard callers.
func SummarizeObservations(a *jsonx.Object) *jsonx.Object {
	return metricObject(Summarize(nativeObservations(a)))
}

// SummarizeMetrics adapts the legacy metrics entry point.
func SummarizeMetrics(text string) (*jsonx.Object, error) {
	o, err := ReadMetrics(text)
	if err != nil {
		return nil, err
	}
	return metricObject(Summarize(o)), nil
}

func identity(row *jsonx.Object, labels []string) string {
	parts := make([]any, len(labels))
	for i, key := range labels {
		parts[i] = row.Value(key)
	}
	raw, _ := jsonx.Compact(parts)
	return string(raw)
}

// ValidateState validates the legacy representation through the typed owner.
func ValidateState(value any) bool {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return false
	}
	_, err = DecodeState(raw)
	return err == nil
}

// ValidateObservations validates legacy observation input through the typed owner.
func ValidateObservations(value any) bool {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return false
	}
	_, err = decodeObservations(raw)
	return err == nil
}
func (s *StateStore) read(path string) (*jsonx.Object, error) {
	state, err := s.readState(path)
	if err != nil || state == nil {
		return nil, err
	}
	return metricObject(state), nil
}

// Read is the legacy dashboard-state codec.
func (s *StateStore) Read(ctx context.Context, id string) (*jsonx.Object, error) {
	state, err := s.ReadState(ctx, id)
	if err != nil || state == nil {
		return nil, err
	}
	return metricObject(state), nil
}

// CompareAndSet is the legacy dashboard-state codec.
func (s *StateStore) CompareAndSet(ctx context.Context, id, expected string, replacement *jsonx.Object) (*jsonx.Object, error) {
	var state *DashboardState
	if replacement != nil {
		raw, err := jsonx.Compact(replacement)
		if err != nil {
			return nil, err
		}
		state, err = DecodeState(raw)
		if err != nil {
			return nil, &StateError{"Invalid DNS statistics state"}
		}
	}
	result, err := s.CompareState(ctx, id, expected, state)
	if err != nil || result == nil {
		return nil, err
	}
	return metricObject(result), nil
}

// ExceedsDigitLimit is exceedsDigitLimit for the routes.
func ExceedsDigitLimit(value any) bool { return exceedsDigitLimit(value) }

// exceedsDigitLimit reports an int literal longer than 4300 digits, which
// json.loads refuses with ValueError.
func exceedsDigitLimit(value any) bool {
	switch v := value.(type) {
	case jsonx.Number:
		if v.IsInt() {
			text := v.String()
			if len(text) > 0 && text[0] == '-' {
				text = text[1:]
			}
			return len(text) > maxStrDigits
		}
	case []any:
		for _, item := range v {
			if exceedsDigitLimit(item) {
				return true
			}
		}
	case *jsonx.Object:
		for _, item := range v.Values() {
			if exceedsDigitLimit(item) {
				return true
			}
		}
	}
	return false
}

// maxStrDigits remains only for the unmigrated HTTP request binder.
const maxStrDigits = 4300

// PanelCall is the compatibility codec for API/agents and rulesync.
func PanelCall(ctx context.Context, m Manager, id, resource, method string, args [][2]string, body any, minimum string) (*jsonx.Object, error) {
	var raw []byte
	var err error
	if body != nil {
		raw, err = jsonx.Compact(body)
		if err != nil {
			return nil, err
		}
	}
	result, err := CallPanel(ctx, registryFor(m), id, resource, method, args, raw, minimum)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(encoded)
	if err != nil {
		return nil, panelFail("INVALID_RESPONSE", 0, "")
	}
	return value.(*jsonx.Object), nil
}

// CheckCoreUpdate is the compatibility codec for the core version operation.
func CheckCoreUpdate(ctx context.Context, m Manager, id string, force bool) (*jsonx.Object, error) {
	args := [][2]string{}
	if force {
		args = append(args, [2]string{"force", "1"})
	}
	return PanelCall(ctx, m, id, "core/update", "GET", args, nil, "")
}
func panelAgent(ctx context.Context, m Manager, id, minimum, feature string) (*jsonx.Object, error) {
	a, err := readPanelAgent(ctx, registryFor(m), id, minimum, feature)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil, err
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}

func legacyAgent(a *model.Agent) *jsonx.Object {
	if a == nil {
		return nil
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return nil
	}
	value, err := jsonx.Parse(raw)
	if err != nil {
		return nil
	}
	return value.(*jsonx.Object)
}
func typedAgent(a *jsonx.Object) (*model.Agent, error) {
	if a == nil {
		return nil, nil
	}
	out := new(model.Agent)
	return out, metricJSON(a, out)
}
func slotRanking(ctx context.Context, m Manager, a *jsonx.Object, recorder string, since, until int64) (*Slot, error) {
	record, err := typedAgent(a)
	if err != nil {
		return nil, err
	}
	return readSlot(ctx, registryFor(m), record, recorder, since, until)
}

// CollectAgent is the legacy collection codec.
func CollectAgent(ctx context.Context, m Manager, store *RankingStore, a *jsonx.Object, recorders []string, now *int64, budget, retention int, pause time.Duration) (int, error) {
	record, err := typedAgent(a)
	if err != nil {
		return 0, err
	}
	return CollectAgentHistory(ctx, registryFor(m), store, record, recorders, now, budget, retention, pause)
}

// RoundManager is what collect_round needs of the Agent manager.
type RoundManager interface {
	Manager
	GetAllAgents(ctx context.Context, history bool) ([]*jsonx.Object, error)
}

// RoundManagerFromAgents adapts the registry's Manager for CollectRound.
func RoundManagerFromAgents(m *agents.Manager) RoundManager { return agentsManager{m} }

// Seams of collect_round for tests (the Python tests patch _recorders,
// collect_agent and manager.is_usable).
var (
	rankingRecorders = func(ctx context.Context, m Manager, agent *jsonx.Object) ([]string, error) {
		snap, err := overviewSnapshot(ctx, m, agent, true)
		if err != nil {
			return nil, err
		}
		out := make([]string, len(snap.recorders))
		for i, r := range snap.recorders {
			out[i] = r.(string)
		}
		return out, nil
	}
	rankingCollectAgent = func(ctx context.Context, m Manager, store *RankingStore, agent *jsonx.Object, recorders []string, now *int64) error {
		_, err := CollectAgent(ctx, m, store, agent, recorders, now, RankingSlotsPerRound, RankingCoreRetentionDays,
			200*time.Millisecond)
		return err
	}
	rankingIsUsable = func(agent *jsonx.Object) bool { return agents.IsUsable(agent, nil) }
)

type legacyRoundRegistry struct {
	legacyRegistry
	RoundManager
}

func (m legacyRoundRegistry) WithReadSnapshot(ctx context.Context, fn func(context.Context) error) error {
	return m.RoundManager.WithReadSnapshot(ctx, fn)
}
func (m legacyRoundRegistry) DataDir() string       { return m.RoundManager.DataDir() }
func (m legacyRoundRegistry) Client() agents.Client { return m.RoundManager.Client() }
func (m legacyRoundRegistry) ReadAgents(ctx context.Context, history bool, inspect func(*model.Agent)) ([]model.Agent, error) {
	list, err := m.GetAllAgents(ctx, history)
	if err != nil {
		return nil, err
	}
	out := make([]model.Agent, 0, len(list))
	for _, raw := range list {
		a, err := typedAgent(raw)
		if err != nil {
			return nil, err
		}
		if inspect != nil {
			inspect(a)
		}
		out = append(out, *a)
	}
	return out, nil
}
func (m legacyRoundRegistry) historyUsable(a *model.Agent) bool {
	return rankingIsUsable(legacyAgent(a))
}
func (m legacyRoundRegistry) historyRecorders(ctx context.Context, a *model.Agent) ([]string, error) {
	return rankingRecorders(ctx, m.RoundManager, legacyAgent(a))
}
func (m legacyRoundRegistry) historyCollect(ctx context.Context, store *RankingStore, a *model.Agent, recorders []string, now *int64) error {
	return rankingCollectAgent(ctx, m.RoundManager, store, legacyAgent(a), recorders, now)
}

// CollectRound is the legacy collector entry point.
func CollectRound(ctx context.Context, m RoundManager, store *RankingStore, now *int64) error {
	if native, ok := m.(RoundRegistry); ok {
		return CollectHistoryRound(ctx, native, store, now)
	}
	return CollectHistoryRound(ctx, legacyRoundRegistry{legacyRegistry{m}, m}, store, now)
}

// HistorySupported is supported(agent).
func HistorySupported(agent *jsonx.Object) bool { return versionAtLeast(agent, HistoryAgentVersion) }

// Manager is what the monitor needs of the Agent manager.
type Manager interface {
	// WithReadSnapshot excludes configuration writers during verification and leaf CAS.
	WithReadSnapshot(context.Context, func(context.Context) error) error
	GetAgentByID(ctx context.Context, agentID string, history bool) (*jsonx.Object, error)
	DataDir() string
	Client() agents.Client
}

// FromAgents adapts the registry's Manager.
func FromAgents(m *agents.Manager) Manager { return agentsManager{m} }

func agentFor(ctx context.Context, m Manager, id, minimum string, history bool) (*jsonx.Object, error) {
	a, err := readDNSAgent(ctx, registryFor(m), id, minimum, history)
	if err != nil {
		return nil, err
	}
	return legacyAgent(a), nil
}
func versionAtLeast(raw *jsonx.Object, minimum string) bool {
	a, err := typedAgent(agentMembers(raw, "version"))
	return err == nil && a != nil && modelVersionAtLeast(a, minimum)
}

// agentMembers projects only fields consumed by a legacy scalar read. Lazy
// history sentinels and unrelated opaque values must never enter its JSON codec.
func agentMembers(raw *jsonx.Object, names ...string) *jsonx.Object {
	if raw == nil {
		return nil
	}
	out := jsonx.NewObject()
	for _, name := range names {
		if value, present := raw.Get(name); present {
			out.Set(name, value)
		}
	}
	return out
}

var doRequest = request

func request(ctx context.Context, m Manager, raw *jsonx.Object, endpoint string, params query, minimum string) (*jsonx.Object, error) {
	a, err := typedAgent(raw)
	if err != nil {
		return nil, err
	}
	fields, err := monitorRequest(ctx, registryFor(m), a, endpoint, params, minimum)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(fields)
	if err != nil {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	value, err := jsonx.Parse(encoded)
	if err != nil {
		return nil, NewError("INVALID_RESPONSE", 0, "")
	}
	return value.(*jsonx.Object), nil
}
func latin1(value string) (string, bool) {
	encoded, err := charmap.ISO8859_1.NewEncoder().String(value)
	return encoded, err == nil
}
func text(value any, limit int) string {
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return truncateText(s, limit)
}

func displayUpstream(value any) string { return displayAddress(text(value, 2048)) }

func target(raw *jsonx.Object) (string, error) {
	a, err := typedAgent(raw)
	if err != nil {
		return "", err
	}
	return TargetIdentity(a)
}

type snapshotResult struct {
	data, core, observations *jsonx.Object
	recorders, labels        []any
}

func overviewSnapshot(ctx context.Context, m Manager, raw *jsonx.Object, dashboard bool) (*snapshotResult, error) {
	a, err := typedAgent(raw)
	if err != nil {
		return nil, err
	}
	snapshot, err := readOverviewSnapshot(ctx, registryFor(m), a, dashboard)
	if err != nil {
		return nil, err
	}
	tags := make([]any, len(snapshot.Recorders))
	for i, tag := range snapshot.Recorders {
		tags[i] = tag
	}
	labels := make([]any, len(snapshot.Labels))
	for i, label := range snapshot.Labels {
		labels[i] = jsonx.Obj("plugin_tag", label.PluginTag, "upstream", label.Upstream, "address", label.Address)
	}
	return &snapshotResult{data: metricObject(snapshot.Identity), core: metricObject(snapshot.Core), observations: metricObject(snapshot.Observations), recorders: tags, labels: labels}, nil
}
func rankingsUnavailable(code string, fields ...any) *jsonx.Object {
	r := unavailableRankings(code)
	out := metricObject(r)
	for i := 0; i+1 < len(fields); i += 2 {
		out.Set(fields[i].(string), fields[i+1])
	}
	return out
}
func rankingSection(value any, kind string) (*jsonx.Object, error) {
	return rankingSectionRows(value, kind, 10)
}
func rankingSectionRows(value any, kind string, limit int) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	section, err := decodeRankingSection(raw, kind, limit)
	if err != nil {
		return nil, err
	}
	return metricObject(section), nil
}
func verifyTarget(ctx context.Context, m Manager, id, expected string) error {
	return verifyNativeTarget(ctx, registryFor(m), id, expected)
}
func compareState(ctx context.Context, m Manager, store *StateStore, id, expected string, replacement *jsonx.Object, verify func(context.Context) error) error {
	var state *DashboardState
	if replacement != nil {
		raw, err := jsonx.Compact(replacement)
		if err != nil {
			return err
		}
		state, err = DecodeState(raw)
		if err != nil {
			return err
		}
	}
	return compareDashboardState(ctx, registryFor(m), store, id, expected, state, verify)
}
func mapStateErrors(err error) error { return nativeStateError(err) }

// ValidRecorderTag validates a legacy recorder value.
func ValidRecorderTag(value any) bool {
	text, ok := value.(string)
	return ok && validRecorderTag(text)
}

// Overview is the legacy response codec.
func Overview(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	result, err := ReadOverview(ctx, registryFor(m), agentID, args)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

// ResetDashboard is the legacy response codec.
func ResetDashboard(ctx context.Context, m Manager, agentID string, body any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(body)
	if err != nil {
		return nil, invalidQuery()
	}
	result, err := ResetStatistics(ctx, registryFor(m), agentID, raw)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

// Queries is the legacy response codec.
func Queries(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	result, err := ReadQueries(ctx, registryFor(m), agentID, args)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

// Query is the legacy response codec.
func Query(ctx context.Context, m Manager, agentID, recordID string, args [][2]string) (*jsonx.Object, error) {
	result, err := ReadQuery(ctx, registryFor(m), agentID, recordID, args)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

// Clients is the legacy response codec.
func Clients(ctx context.Context, m Manager, agentID string, args [][2]string) (*jsonx.Object, error) {
	result, err := ReadClients(ctx, registryFor(m), agentID, args)
	if err != nil {
		return nil, err
	}
	return metricObject(result), nil
}

func record(value any, detail bool) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	out, err := queryRecord(raw, detail)
	if err != nil {
		return nil, err
	}
	decoded, err := jsonx.Parse(out)
	if err != nil {
		return nil, err
	}
	return decoded.(*jsonx.Object), nil
}

// Advance is the legacy pure state-transition codec.
func Advance(previous, snapshot, core, observations *jsonx.Object, target string, agent *jsonx.Object, manual bool) (*jsonx.Object, error) {
	var prev *DashboardState
	if previous != nil {
		raw, err := jsonx.Compact(previous)
		if err != nil {
			return nil, err
		}
		prev, err = DecodeState(raw)
		if err != nil {
			return nil, err
		}
	}
	var a *model.Agent
	if agent != nil {
		a = new(model.Agent)
		if err := metricJSON(agent, a); err != nil {
			return nil, err
		}
	}
	var stamp SnapshotIdentity
	if err := metricJSON(snapshot, &stamp); err != nil {
		return nil, err
	}
	var status CoreStatus
	if err := metricJSON(core, &status); err != nil {
		return nil, err
	}
	state, err := AdvanceState(prev, &stamp, &status, nativeObservations(observations), target, a, manual)
	if err != nil {
		return nil, err
	}
	if state == prev && previous != nil {
		return previous, nil
	}
	return metricObject(state), nil
}

// Totals is the legacy period-total codec.
func Totals(state, observations *jsonx.Object) (*jsonx.Object, bool, error) {
	raw, err := jsonx.Compact(state)
	if err != nil {
		return nil, false, err
	}
	s, err := DecodeState(raw)
	if err != nil {
		return nil, false, err
	}
	adjusted, partial, err := StateTotals(s, nativeObservations(observations))
	if err != nil {
		return nil, false, err
	}
	return metricObject(adjusted), partial, nil
}

// HARulesFunc is oxidns_ha_publish.rules(manager, agent_id, method): the
// task answer and its HTTP status; a *HABusyError for its ValueErrors.
type HARulesFunc func(ctx context.Context, m Manager, agentID, method string) (*jsonx.Object, int, error)

// SetHARules installs oxidns_ha_publish.rules (A18) and returns a function
// restoring the previous one.
func SetHARules(fn HARulesFunc) (restore func()) {
	return SetHARulesJSON(func(ctx context.Context, m Registry, id, method string) (json.RawMessage, int, error) {
		var old Manager
		switch m := m.(type) {
		case Manager:
			old = m
		case legacyRegistry:
			old = m.Manager
		default:
			return nil, 0, ErrHARulesNotPorted
		}
		result, status, err := fn(ctx, old, id, method)
		if err != nil {
			return nil, status, err
		}
		raw, err := json.Marshal(result)
		return raw, status, err
	})
}

// SupportsPaths is the legacy model adapter.
func SupportsPaths(agent *jsonx.Object) bool {
	return versionAtLeast(agent, RuleUpdatePathsMinAgentVersion)
}

func ruleUpdateTask(value any) (*jsonx.Object, error) {
	raw, err := jsonx.Compact(value)
	if err != nil {
		return nil, err
	}
	task, err := decodeRuleTask(raw)
	if err != nil {
		return nil, err
	}
	raw, err = json.Marshal(task)
	if err != nil {
		return nil, err
	}
	value, err = jsonx.Parse(raw)
	if err != nil {
		return nil, err
	}
	return value.(*jsonx.Object), nil
}

// RuleUpdateCall converts the legacy package representation once.
func RuleUpdateCall(ctx context.Context, m Manager, id, method string, value any) (*jsonx.Object, int, error) {
	var paths []string
	if value != nil {
		var ok bool
		paths, ok = nativeTags(value)
		if !ok {
			return nil, 0, RuleUpdateFail("INVALID_REQUEST", 400)
		}
	}
	result, status, err := CallRuleUpdate(ctx, registryFor(m), id, method, paths)
	if err != nil {
		return nil, status, err
	}
	var raw []byte
	if result.Pair != nil {
		raw = result.Pair
	} else {
		raw, err = json.Marshal(result)
	}
	if err != nil {
		return nil, 0, err
	}
	decoded, err := jsonx.Parse(raw)
	if err != nil {
		return nil, 0, err
	}
	return decoded.(*jsonx.Object), status, nil
}

func ruleUpdateAgent(ctx context.Context, m Manager, id string) (*jsonx.Object, error) {
	agent, err := agentFor(ctx, m, id, RuleUpdateMinAgentVersion, false)
	return agent, err
}

type legacyRegistry struct{ Manager }

func (m legacyRegistry) ReadAgent(ctx context.Context, id string, history bool) (*model.Agent, error) {
	raw, err := m.GetAgentByID(ctx, id, history)
	if err != nil || raw == nil {
		return nil, err
	}
	data, err := jsonx.Compact(raw)
	if err != nil {
		return nil, err
	}
	var a model.Agent
	if err := jsondecode.Unmarshal(data, &a); err != nil {
		return nil, err
	}
	return &a, nil
}
func registryFor(m Manager) Registry {
	if typed, ok := m.(Registry); ok {
		return typed
	}
	return legacyRegistry{m}
}

func nativeTags(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	tags := make([]string, len(list))
	for i, value := range list {
		tag, ok := value.(string)
		if !ok {
			return nil, false
		}
		tags[i] = tag
	}
	return tags, true
}

// ValidRequestTags validates the legacy request representation.
func ValidRequestTags(value any) (bool, error) {
	if list, ok := value.([]any); ok {
		for _, tag := range list {
			if _, ok := tag.([]any); ok {
				return false, errors.New("cache tags must be strings")
			}
			if _, ok := tag.(*jsonx.Object); ok {
				return false, errors.New("cache tags must be strings")
			}
		}
	}
	tags, ok := nativeTags(value)
	return ok && validTags(tags, true), nil
}

// CacheCall converts the legacy request and response at the package boundary.
func CacheCall(ctx context.Context, m Manager, agentID, method string, value any) (*jsonx.Object, error) {
	var tags []string
	if value != nil {
		valid, err := ValidRequestTags(value)
		if err != nil {
			return nil, err
		}
		if !valid {
			return nil, CacheFail("INVALID_REQUEST", 400)
		}
		tags, _ = nativeTags(value)
	}
	result, err := CallCache(ctx, registryFor(m), agentID, method, tags)
	if err != nil {
		var partial *CacheOperationError
		if errors.As(err, &partial) {
			flushed := make([]any, len(partial.Flushed))
			for i, tag := range partial.Flushed {
				flushed[i] = tag
			}
			return nil, &FlushError{Err: partial.Err, Flushed: flushed}
		}
		return nil, err
	}
	key, values := "caches", result.Caches
	if method == "POST" {
		key, values = "flushed", result.Flushed
	}
	out := make([]any, len(values))
	for i, tag := range values {
		out[i] = tag
	}
	return jsonx.Obj("success", result.Success, key, out), nil
}

func (m legacyRegistry) monitorReply(ctx context.Context, a *model.Agent, endpoint string, params query, minimum string) (model.Fields, error) {
	var fields model.Fields
	if err := ctx.Err(); err != nil {
		return fields, err
	}
	raw, err := json.Marshal(a)
	if err != nil {
		return fields, err
	}
	decoded, err := jsonx.Parse(raw)
	if err != nil {
		return fields, err
	}
	answer, err := doRequest(ctx, m.Manager, decoded.(*jsonx.Object), endpoint, params, minimum)
	if err != nil {
		return fields, err
	}
	raw, err = jsonx.Compact(answer)
	if err != nil {
		return fields, err
	}
	if err := ctx.Err(); err != nil {
		return fields, err
	}
	raw, _ = jsonresp.NormalizeNonFinite(raw)
	err = jsondecode.Unmarshal(raw, &fields)
	if err != nil {
		return fields, NewError("INVALID_RESPONSE", 0, "")
	}
	return fields, nil
}

// FlushError is the legacy partial-flush response codec.
type FlushError struct {
	Err     *Error
	Flushed []any
}

func (e *FlushError) Error() string { return e.Err.Message }
func (e *FlushError) Unwrap() error { return e.Err }
