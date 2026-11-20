package monitor

// Port of backend/test_oxidns_cache.py (call-level cases): cache flushing
// targets one registered OxiDNS Agent and its own cache tags. The route
// cases are in internal/api/agents/panel_test.go.

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"configflow/internal/agents"
	"configflow/internal/model"
)

func cacheManager(t *testing.T, changes map[string][]any) (*fakeManager, *replyClient) {
	records := map[string]*model.Agent{
		"dns": mustAgentMap(map[string]any{
			"id": "dns", "host": "127.0.0.1", "port": 8082, "service_type": "oxidns",
			"version": "1.8.36-go", "token": "dns-secret",
		}),
		"other": mustAgentMap(map[string]any{
			"id": "other", "host": "127.0.0.2", "port": 8083, "service_type": "oxidns",
			"version": "1.8.36-go", "token": "other-secret",
		}),
		"proxy": mustAgentMap(map[string]any{
			"id": "proxy", "host": "127.0.0.1", "port": 8084, "service_type": "mihomo",
			"version": "1.8.36-go", "token": "proxy-secret",
		}),
	}
	for id, fields := range changes {
		for i := 0; i+1 < len(fields); i += 2 {
			setAgentField(records[id], fields[i].(string), fields[i+1])
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
	client.add(jsonResponse(map[string]any{"success": true, "caches": []any{"main_cache", "probe"}}, 200),
		jsonResponse(map[string]any{"success": true, "flushed": []any{"probe"}}, 200))
	result, err := CallCache(ctx, m, "dns", "GET", nil)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, result, map[string]any{"success": true, "caches": []any{"main_cache", "probe"}})
	result, err = CallCache(ctx, m, "dns", "POST", []string{"probe"})
	if err != nil {
		t.Fatal(err)
	}
	wantJSON(t, result, map[string]any{"success": true, "flushed": []any{"probe"}})
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
		_, err := CallCache(ctx, m, tc.agentID, "POST", []string{"probe"})
		wantMonitorError(t, err, tc.code, 0)
		if client.count() != 0 {
			t.Fatal(tc)
		}
	}
}

func TestCacheRejectsInvalidRequestsBeforeContactingAgent(t *testing.T) {
	for _, tc := range []struct {
		method string
		tags   []string
	}{
		{"POST", nil}, {"POST", []string{}}, {"POST", []string{"../reload"}}, {"POST", []string{"probe", "probe"}},
		{"POST", []string{"_x"}}, {"POST", []string{strings.Repeat("a", 65)}}, {"GET", []string{"probe"}},
		{"DELETE", nil},
	} {
		m, client := cacheManager(t, nil)
		_, err := CallCache(ctx, m, "dns", tc.method, tc.tags)
		wantMonitorError(t, err, "INVALID_REQUEST", 400)
		if client.count() != 0 {
			t.Fatal(tc)
		}
	}
}

func TestPartialFailureReportsCompletedCaches(t *testing.T) {
	m, client := cacheManager(t, nil)
	client.add(jsonResponse(map[string]any{"success": false, "code": "CACHE_FLUSH_FAILED", "message": "core secret detail",
		"flushed": []any{"main_cache"}}, 502))
	_, err := CallCache(ctx, m, "dns", "POST", []string{"main_cache", "probe"})
	var flushErr *CacheOperationError
	if !errors.As(err, &flushErr) || flushErr.Err.Code != "CACHE_FLUSH_FAILED" || strings.Contains(flushErr.Err.Message, "secret") {
		t.Fatal(err)
	}
	wantJSON(t, flushErr.Flushed, []string{"main_cache"})
	wantMonitorError(t, err, "CACHE_FLUSH_FAILED", 502) // still a MonitorError
}

func TestUntrustedAgentResponsesAreValidated(t *testing.T) {
	for _, tc := range []struct {
		reply *agents.Response
		code  string
	}{
		{rawResponse(404, "404 page not found"), "UPGRADE_REQUIRED"},
		{jsonResponse(map[string]any{"success": false, "code": "CACHE_UNKNOWN"}, 409), "CACHE_UNKNOWN"},
		{jsonResponse(map[string]any{"success": true, "flushed": []any{"main_cache"}}, 200), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": true, "flushed": []any{"../x"}}, 200), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": true}, 302), "INVALID_RESPONSE"},
		{jsonResponse(map[string]any{"success": false}, 401), "AGENT_AUTH_FAILED"},
	} {
		m, client := cacheManager(t, nil)
		client.add(tc.reply)
		_, err := CallCache(ctx, m, "dns", "POST", []string{"probe"})
		wantMonitorError(t, err, tc.code, 0)
	}
}

func TestCacheStatusMappingAndPythonErrors(t *testing.T) {
	m, client := cacheManager(t, nil)
	client.add(jsonResponse(map[string]any{"success": false, "code": "CORE_BUSY", "flushed": nil}, 503),
		jsonResponse(map[string]any{"success": false, "code": "NOPE"}, 404),
		jsonResponse(map[string]any{"success": false, "code": []any{"x"}}, 404),
		jsonResponse(map[string]any{"success": false, "code": []any{"x"}}, 500),
		rawResponse(200, strings.Repeat(" ", CacheMaxResponseBytes+1)),
		rawResponse(200, "[]"))
	_, err := CallCache(ctx, m, "dns", "POST", []string{"probe"})
	var flushErr *CacheOperationError
	if !errors.As(err, &flushErr) || flushErr.Err.Status != 503 || len(flushErr.Flushed) != 0 {
		t.Fatal(err)
	}
	_, err = CallCache(ctx, m, "dns", "POST", []string{"probe"})
	wantMonitorError(t, err, "UPGRADE_REQUIRED", 409)
	_, err = CallCache(ctx, m, "dns", "POST", []string{"probe"})
	var monitorErr *Error
	if err == nil || errors.As(err, &monitorErr) {
		t.Fatal("404 with an unhashable code is a TypeError", err)
	}
	_, err = CallCache(ctx, m, "dns", "POST", []string{"probe"})
	wantMonitorError(t, err, "CACHE_FLUSH_FAILED", 502)
	_, err = CallCache(ctx, m, "dns", "POST", []string{"probe"})
	wantMonitorError(t, err, "INVALID_RESPONSE", 502)
	_, err = CallCache(ctx, m, "dns", "POST", []string{"probe"})
	wantMonitorError(t, err, "INVALID_RESPONSE", 502)
	// A missing token is a KeyError outside the try block (500), a non-latin-1 one AGENT_UNREACHABLE.
	deleteAgentField(m.agents["dns"], "token")
	if _, err = CallCache(ctx, m, "dns", "GET", nil); err == nil || errors.As(err, &monitorErr) {
		t.Fatal(err)
	}
	setAgentField(m.agents["dns"], "token", "令牌")
	_, err = CallCache(ctx, m, "dns", "GET", nil)
	wantMonitorError(t, err, "AGENT_UNREACHABLE", 502)
}

func TestValidRequestTagsEnforcesBoundsAndUniqueness(t *testing.T) {
	atLimit := make([]string, CacheMaxTags)
	for i := range atLimit {
		atLimit[i] = fmt.Sprintf("tag-%d", i)
	}
	overLimit := append(append([]string(nil), atLimit...), fmt.Sprintf("tag-%d", CacheMaxTags))
	for _, tc := range []struct {
		name string
		tags []string
		ok   bool
	}{
		{"two-valid", []string{"a", "b"}, true},
		{"at-limit-unique", atLimit, true},
		{"over-limit-unique", overLimit, false},
		{"duplicate", []string{"a", "a"}, false},
		{"empty", []string{}, false},
		{"nil", nil, false},
		{"bad-prefix", []string{"_bad"}, false},
	} {
		if got := validTags(tc.tags, true); got != tc.ok {
			t.Fatalf("%s: got %v want %v", tc.name, got, tc.ok)
		}
	}
}
