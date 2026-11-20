package agents

// The direct Agent calls of manager.py part A (status, logs, uninstall,
// update, logging configuration, the domain discovery and DNS HA calls).
// Python has no tests for them; the expected values were produced by
// running manager.py with the same Agent answers (testdata/remote_ref.py,
// CPython 3.11 + requests on the test server).

import (
	"encoding/json"
	"net/http"
	"testing"

	"configflow/internal/model"
)

func raw(body string, status int) func(Request) (*Response, error) {
	return func(Request) (*Response, error) {
		return &Response{StatusCode: status, Header: http.Header{}, Body: []byte(body)}, nil
	}
}

func TestDirectAgentCallsAnswerLikePython(t *testing.T) {
	e := newEnv(t)
	call := func(name string, handle func(Request) (*Response, error), fn func() (json.RawMessage, error), want any) {
		t.Helper()
		e.fake.set(handle)
		before := remoteReferenceHead(t, e)
		got, err := fn()
		response, _ := handle(Request{})
		if verifyNativeRemoteReference(t, e, name, string(response.Body), response.StatusCode, want, err, before) {
			return
		}
		if err != nil {
			t.Errorf("%s: unexpected err %v", name, err)
			return
		}
		if diff := diffJSONValues(t, want, got); diff != "" {
			t.Errorf("%s: %s", name, diff)
		}
	}
	id := e.id()
	getStatus := func(agentID string) func() (json.RawMessage, error) {
		return func() (json.RawMessage, error) {
			raw, _, err := e.m.RemoteCall(e.ctx, agentID, RemoteRequest{Method: "GET", Path: "/api/status", Timeout: Seconds(5)})
			return raw, err
		}
	}
	call("status_non_json", raw("not json", 200), getStatus(id),
		map[string]any{"success": false, "message": "Connection error: Expecting value: line 1 column 1 (char 0)"})
	call("status_404", raw("{}", 404), getStatus(id),
		map[string]any{"success": false, "message": "HTTP 404"})
	call("status_list", raw("[1]", 200), getStatus(id), []any{1})
	call("sources_404", raw("x", 404), func() (json.RawMessage, error) {
		raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "GET", Path: "/api/logs/sources", Timeout: Seconds(5), LegacyLogSources: true})
		return raw, err
	}, map[string]any{"success": true, "legacy": true, "sources": []any{}})
	call("uninstall_non_json", raw("oops", 200), func() (json.RawMessage, error) {
		return e.m.RunDirectOperation(e.ctx, id, UninstallService, nil, nil)
	}, map[string]any{"success": false, "message": "Connection error: Expecting value: line 1 column 1 (char 0)"})
	call("update_500", raw("", 500), func() (json.RawMessage, error) {
		header, body, err := jsonBody(struct {
			Version string `json:"version"`
			URL     string `json:"download_url"`
		}{"1", "u"})
		if err != nil {
			return nil, err
		}
		return e.m.RunDirectOperation(e.ctx, id, UpdateService, header, body)
	}, map[string]any{"success": false, "message": "HTTP 500"})
	call("missing", raw("{}", 200), getStatus("missing"),
		map[string]any{"success": false, "message": "Agent not found"})

	e.fake.requests = nil
	call("logs_url", raw(`{"success": true}`, 200), func() (json.RawMessage, error) {
		raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "GET", Path: "/api/logs?lines=50&log_path=" + quote("/var/log/a b?.log"), Timeout: Seconds(5)})
		return raw, err
	}, map[string]any{"success": true})
	call("logs_source", raw(`{"success": true}`, 200), func() (json.RawMessage, error) {
		raw, _, err := e.m.RemoteCall(e.ctx, id, RemoteRequest{Method: "GET", Path: "/api/logs?lines=50&source=" + quote("mihomo/日志 a"), Timeout: Seconds(5)})
		return raw, err
	}, map[string]any{"success": true})
	urls := e.fake.urls()
	if len(urls) != 2 || urls[0] != "http://agent.test:8080/api/logs?lines=50&log_path=/var/log/a%20b%3F.log" ||
		urls[1] != "http://agent.test:8080/api/logs?lines=50&source=mihomo/%E6%97%A5%E5%BF%97%20a" {
		t.Errorf("urls %v", urls)
	}
}

func TestSetLoggingConfigLikePython(t *testing.T) {
	e := newEnv(t)
	id := e.id()
	setLogging := func(enabled bool) (json.RawMessage, error) {
		header, body, err := jsonBody(map[string]any{"enabled": enabled})
		if err != nil {
			return nil, err
		}
		return e.m.SetLoggingModel(e.ctx, id, mustJSON(t, enabled), header, body)
	}
	fails := func(name, body, want string) {
		t.Helper()
		e.fake.set(raw(body, 200))
		before := remoteReferenceHead(t, e)
		_, err := setLogging(true)
		if verifyNativeRemoteReference(t, e, "logging_"+name, body, 200, want, err, before) {
			return
		}
		if err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %s", name, err, want)
		}
	}
	fails("list", "[1]", "'list' object has no attribute 'get'")
	fails("null", "null", "'NoneType' object has no attribute 'get'")
	e.fake.set(raw(`{"success": true, "x": 1}`, 200))
	if got := must[json.RawMessage](t)(setLogging(true)); diffJSONValues(t, map[string]any{"success": true, "x": 1}, got) != "" {
		t.Fatalf("ok %s", got)
	}
	if got := e.stored(id).LoggingConfig; got == nil || diffJSONValues(t, map[string]any{"enabled": true}, got) != "" {
		t.Fatalf("saved %+v", got)
	}
	e.fake.set(raw(`{"success": false}`, 200))
	if got := must[json.RawMessage](t)(setLogging(false)); diffJSONValues(t, map[string]any{"success": false}, got) != "" ||
		diffJSONValues(t, map[string]any{"enabled": true}, e.stored(id).LoggingConfig) != "" {
		t.Fatalf("failure %s", got)
	}
	for _, c := range []struct {
		existing string
		name     string
		want     string
	}{
		{"null", "logging_existing_none", "'NoneType' object does not support item assignment"},
		{"[1]", "logging_existing_list", "list indices must be integers or slices, not str"},
	} {
		e.editSystem(func(system *model.System) {
			_ = e.firstAgent(system).SetJSON("logging_config", json.RawMessage(c.existing))
		})
		e.fake.set(raw(`{"success": true}`, 200))
		before := remoteReferenceHead(t, e)
		_, err := setLogging(false)
		if verifyNativeRemoteReference(t, e, c.name, `{"success": true}`, 200, c.want, err, before) {
			continue
		}
		if err == nil || err.Error() != c.want {
			t.Errorf("existing %s: %v", c.existing, err)
		}
	}
}

func TestAgentJSONAndDNSHARequestsLikePython(t *testing.T) {
	e := newEnv(t)
	agent := e.get(e.id())
	agent.Name = "t"
	fails := func(name string, handle func(Request) (*Response, error), fn func() error, want string) {
		t.Helper()
		e.fake.set(handle)
		before := remoteReferenceHead(t, e)
		err := fn()
		response, _ := handle(Request{})
		if verifyNativeRemoteReference(t, e, name, string(response.Body), response.StatusCode, want, err, before) {
			return
		}
		if err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %s", name, err, want)
		}
	}
	probe := func() error {
		_, err := e.m.ProbeDomainsModel(e.ctx, agent, json.RawMessage(`{}`), Timeout{})
		return err
	}
	fails("json_404", raw("", 404), probe, "Agent t 不支持域名探测，请升级")
	fails("json_list", raw("[1]", 200), probe, "'list' object has no attribute 'get'")
	fails("json_null", raw("null", 200), probe, "'NoneType' object has no attribute 'get'")
	fails("json_fail_msg", raw(`{"success": false, "message": "busy"}`, 500), probe, "busy")
	fails("json_fail_nomsg", raw("not json", 502), probe, "Agent 返回 HTTP 502")
	fails("region_409", raw("[]", 409), func() error { _, err := e.m.RegionCheckModel(e.ctx, agent, json.RawMessage(`{}`)); return err },
		"Agent 上的 Mihomo 还没有检测入口，请为该 Agent 开启域名发现后部署一次配置")
	e.fake.set(raw(`{"success": true, "targets": null}`, 200))
	if got := must[json.RawMessage](t)(e.m.RegionTargetsModel(e.ctx, agent)); diffJSONValues(t, []any{}, got) != "" {
		t.Errorf("targets %s", got)
	}
	haStatus := func() (model.Fields, error) {
		return e.m.AgentRequestModel(e.ctx, agent, RemoteRequest{Method: "GET", Path: "/api/oxidns-ha", Timeout: Pair(5, 15)}, "", true)
	}
	status := func() error { _, err := haStatus(); return err }
	fails("ha_404", raw("", 404), status, "Agent t 不支持 DNS 高可用，请升级")
	fails("ha_false", raw(`{"success": false, "error": "bad `+agent.Token+`"}`, 200), status, "bad [REDACTED]")
	e.fake.set(raw("[1]", 200))
	before := remoteReferenceHead(t, e)
	got, err := haStatus()
	if !verifyNativeRemoteReference(t, e, "ha_list", "[1]", 200, map[string]any{}, err, before) {
		if err != nil || got.Len() != 0 {
			t.Errorf("ha_list %v %v", got, err)
		}
	}
}
