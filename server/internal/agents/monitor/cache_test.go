package monitor

// Port of backend/test_oxidns_cache.py (call-level cases): cache flushing
// targets one registered OxiDNS Agent and its own cache tags. The route
// cases are in internal/api/agents/panel_test.go.

import (
	"errors"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
)

func cacheManager(t *testing.T, changes map[string][]any) (*fakeManager, *replyClient) {
	records := map[string]*jsonx.Object{
		"dns": jsonx.Obj("id", "dns", "host", "127.0.0.1", "port", 8082, "service_type", "oxidns",
			"version", "1.8.36-go", "token", "dns-secret"),
		"other": jsonx.Obj("id", "other", "host", "127.0.0.2", "port", 8083, "service_type", "oxidns",
			"version", "1.8.36-go", "token", "other-secret"),
		"proxy": jsonx.Obj("id", "proxy", "host", "127.0.0.1", "port", 8084, "service_type", "mihomo",
			"version", "1.8.36-go", "token", "proxy-secret"),
	}
	for id, fields := range changes {
		for i := 0; i+1 < len(fields); i += 2 {
			records[id].Set(fields[i].(string), fields[i+1])
		}
	}
	client := &replyClient{}
	return &fakeManager{agents: records, dataDir: t.TempDir(), client: client}, client
}

func rawResponse(status int, body string) *agents.Response {
	return &agents.Response{StatusCode: status, Body: []byte(body)}
}

func TestListAndFlushReachOnlyTheNamedAgent(t *testing.T) {
	m, client := cacheManager(t, nil)
	client.add(jsonResponse(jsonx.Obj("success", true, "caches", []any{"main_cache", "probe"}), 200),
		jsonResponse(jsonx.Obj("success", true, "flushed", []any{"probe"}), 200))
	result, err := CacheCall(ctx, m, "dns", "GET", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, result, jsonx.Obj("success", true, "caches", []any{"main_cache", "probe"}))
	result, err = CacheCall(ctx, m, "dns", "POST", []any{"probe"})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, result, jsonx.Obj("success", true, "flushed", []any{"probe"}))
	if client.calls[0].url != "http://127.0.0.1:8082/api/oxidns/cache" || client.calls[1].url != "http://127.0.0.1:8082/api/oxidns/cache/flush" {
		t.Fatal(client.calls)
	}
	for _, c := range client.calls {
		if c.header["Authorization"] != "Bearer dns-secret" || !c.req.NoProxy || !c.req.NoRedirects || strings.Contains(c.header["Authorization"], "other") {
			t.Fatal(c)
		}
	}
	if client.calls[0].req.Body != nil || string(client.calls[1].req.Body) != `{"tags": ["probe"]}` ||
		client.calls[1].header["Content-Type"] != "application/json" || client.calls[0].header["Content-Type"] != "" {
		t.Fatal(client.calls)
	}
	if client.calls[0].req.Timeout != agents.Pair(3, 40) || client.calls[0].req.MaxBody != CacheMaxResponseBytes {
		t.Fatal(client.calls[0].req)
	}
}

func TestCacheRejectsUnsupportedTargetsBeforeRequest(t *testing.T) {
	for _, tc := range []struct {
		agentID string
		changes map[string][]any
		code    string
	}{
		{"missing", nil, "AGENT_NOT_FOUND"},
		{"proxy", nil, "WRONG_SERVICE"},
		{"dns", map[string][]any{"dns": {"version", "1.8.35-go"}}, "UPGRADE_REQUIRED"},
	} {
		m, client := cacheManager(t, tc.changes)
		_, err := CacheCall(ctx, m, tc.agentID, "POST", []any{"probe"})
		wantMonitorError(t, err, tc.code, 0)
		if client.count() != 0 {
			t.Fatal(tc)
		}
	}
}

func TestCacheRejectsInvalidRequestsBeforeContactingAgent(t *testing.T) {
	for _, tc := range []struct {
		method string
		tags   any
	}{
		{"POST", nil}, {"POST", []any{}}, {"POST", []any{"../reload"}}, {"POST", []any{"probe", "probe"}},
		{"POST", []any{"_x"}}, {"POST", []any{strings.Repeat("a", 65)}}, {"POST", "probe"}, {"GET", []any{"probe"}},
		{"DELETE", nil},
	} {
		m, client := cacheManager(t, nil)
		_, err := CacheCall(ctx, m, "dns", tc.method, tc.tags)
		wantMonitorError(t, err, "INVALID_REQUEST", 400)
		if client.count() != 0 {
			t.Fatal(tc)
		}
	}
}

func TestPartialFailureReportsCompletedCaches(t *testing.T) {
	m, client := cacheManager(t, nil)
	client.add(jsonResponse(jsonx.Obj("success", false, "code", "CACHE_FLUSH_FAILED", "message", "core secret detail",
		"flushed", []any{"main_cache"}), 502))
	_, err := CacheCall(ctx, m, "dns", "POST", []any{"main_cache", "probe"})
	var flushErr *FlushError
	if !errors.As(err, &flushErr) || flushErr.Err.Code != "CACHE_FLUSH_FAILED" || strings.Contains(flushErr.Err.Message, "secret") {
		t.Fatal(err)
	}
	wantJSON(t, flushErr.Flushed, []any{"main_cache"})
	wantMonitorError(t, err, "CACHE_FLUSH_FAILED", 502) // still a MonitorError
}

func TestUntrustedAgentResponsesAreValidated(t *testing.T) {
	for _, tc := range []struct {
		reply *agents.Response
		code  string
	}{
		{rawResponse(404, "404 page not found"), "UPGRADE_REQUIRED"},
		{jsonResponse(jsonx.Obj("success", false, "code", "CACHE_UNKNOWN"), 409), "CACHE_UNKNOWN"},
		{jsonResponse(jsonx.Obj("success", true, "flushed", []any{"main_cache"}), 200), "INVALID_RESPONSE"},
		{jsonResponse(jsonx.Obj("success", true, "flushed", []any{"../x"}), 200), "INVALID_RESPONSE"},
		{jsonResponse(jsonx.Obj("success", true), 302), "INVALID_RESPONSE"},
		{jsonResponse(jsonx.Obj("success", false), 401), "AGENT_AUTH_FAILED"},
	} {
		m, client := cacheManager(t, nil)
		client.add(tc.reply)
		_, err := CacheCall(ctx, m, "dns", "POST", []any{"probe"})
		wantMonitorError(t, err, tc.code, 0)
	}
}

func TestCacheStatusMappingAndPythonErrors(t *testing.T) {
	m, client := cacheManager(t, nil)
	client.add(jsonResponse(jsonx.Obj("success", false, "code", "CORE_BUSY", "flushed", nil), 503),
		jsonResponse(jsonx.Obj("success", false, "code", "NOPE"), 404),
		jsonResponse(jsonx.Obj("success", false, "code", []any{"x"}), 404),
		jsonResponse(jsonx.Obj("success", false, "code", []any{"x"}), 500),
		rawResponse(200, strings.Repeat(" ", CacheMaxResponseBytes+1)),
		rawResponse(200, "[]"))
	_, err := CacheCall(ctx, m, "dns", "POST", []any{"probe"})
	var flushErr *FlushError
	if !errors.As(err, &flushErr) || flushErr.Err.Status != 503 || len(flushErr.Flushed) != 0 {
		t.Fatal(err)
	}
	_, err = CacheCall(ctx, m, "dns", "POST", []any{"probe"})
	wantMonitorError(t, err, "UPGRADE_REQUIRED", 409)
	_, err = CacheCall(ctx, m, "dns", "POST", []any{"probe"})
	var monitorErr *Error
	if err == nil || errors.As(err, &monitorErr) {
		t.Fatal("404 with an unhashable code is a TypeError", err)
	}
	_, err = CacheCall(ctx, m, "dns", "POST", []any{"probe"})
	wantMonitorError(t, err, "CACHE_FLUSH_FAILED", 502)
	_, err = CacheCall(ctx, m, "dns", "POST", []any{"probe"})
	wantMonitorError(t, err, "INVALID_RESPONSE", 502)
	_, err = CacheCall(ctx, m, "dns", "POST", []any{"probe"})
	wantMonitorError(t, err, "INVALID_RESPONSE", 502)
	// A missing token is a KeyError outside the try block (500), a non-latin-1 one AGENT_UNREACHABLE.
	m.agents["dns"].Delete("token")
	if _, err = CacheCall(ctx, m, "dns", "GET", nil); err == nil || errors.As(err, &monitorErr) {
		t.Fatal(err)
	}
	m.agents["dns"].Set("token", "令牌")
	_, err = CacheCall(ctx, m, "dns", "GET", nil)
	wantMonitorError(t, err, "AGENT_UNREACHABLE", 502)
}

func TestValidRequestTagsFollowsPythonSetSemantics(t *testing.T) {
	for _, tc := range []struct {
		value any
		ok    bool
		err   bool
	}{
		{[]any{"a", "b"}, true, false}, {[]any{"a", "a"}, false, false}, {[]any{jsonx.Int(1), jsonx.Int(1)}, false, false},
		{[]any{"a", jsonx.Int(1)}, false, false}, {[]any{jsonx.NewObject()}, false, true}, {[]any{"a", []any{}}, false, true},
		{[]any{}, false, false}, {make([]any, 65), false, false}, {"a", false, false},
	} {
		ok, err := ValidRequestTags(tc.value)
		if ok != tc.ok || (err != nil) != tc.err {
			t.Fatal(tc, ok, err)
		}
	}
}
