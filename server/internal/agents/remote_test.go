package agents

// The direct Agent calls of manager.py part A (status, logs, uninstall,
// update, logging configuration, the domain discovery and DNS HA calls).
// Python has no tests for them; the expected values were produced by
// running manager.py with the same Agent answers (testdata/remote_ref.py,
// CPython 3.11 + requests on the test server).

import (
	"net/http"
	"testing"

	"configflow/internal/jsonx"
)

func raw(body string, status int) func(Request) (*Response, error) {
	return func(Request) (*Response, error) {
		return &Response{StatusCode: status, Header: http.Header{}, Body: []byte(body)}, nil
	}
}

func TestDirectAgentCallsAnswerLikePython(t *testing.T) {
	e := newEnv(t)
	call := func(name string, handle func(Request) (*Response, error), fn func() (any, error), want any) {
		t.Helper()
		e.fake.set(handle)
		got, err := fn()
		if err != nil || !jsonx.Equal(got, want) {
			t.Errorf("%s: %v %v, want %v", name, got, err, want)
		}
	}
	id := e.id()
	call("status_non_json", raw("not json", 200), func() (any, error) { return e.m.GetAgentStatus(e.ctx, id) },
		jsonx.Obj("success", false, "message", "Connection error: Expecting value: line 1 column 1 (char 0)"))
	call("status_404", raw("{}", 404), func() (any, error) { return e.m.GetAgentStatus(e.ctx, id) },
		jsonx.Obj("success", false, "message", "HTTP 404"))
	call("status_list", raw("[1]", 200), func() (any, error) { return e.m.GetAgentStatus(e.ctx, id) }, []any{jsonx.Int(1)})
	call("sources_404", raw("x", 404), func() (any, error) { return e.m.GetAgentLogSources(e.ctx, id) },
		jsonx.Obj("success", true, "legacy", true, "sources", []any{}))
	call("uninstall_non_json", raw("oops", 200), func() (any, error) { return e.m.UninstallAgent(e.ctx, id) },
		jsonx.Obj("success", false, "message", "Connection error: Expecting value: line 1 column 1 (char 0)"))
	call("update_500", raw("", 500), func() (any, error) { return e.m.UpdateAgentVersion(e.ctx, id, "1", "u") },
		jsonx.Obj("success", false, "message", "HTTP 500"))
	call("missing", raw("{}", 200), func() (any, error) { return e.m.GetAgentStatus(e.ctx, "missing") },
		jsonx.Obj("success", false, "message", "Agent not found"))

	e.fake.requests = nil
	call("logs_url", raw(`{"success": true}`, 200), func() (any, error) {
		return e.m.GetAgentLogs(e.ctx, id, jsonx.Int(50), "/var/log/a b?.log", "")
	}, jsonx.Obj("success", true))
	call("logs_source", raw(`{"success": true}`, 200), func() (any, error) {
		return e.m.GetAgentLogs(e.ctx, id, jsonx.Int(50), "/x", "mihomo/日志 a")
	}, jsonx.Obj("success", true))
	urls := e.fake.urls()
	if len(urls) != 2 || urls[0] != "http://agent.test:8080/api/logs?lines=50&log_path=/var/log/a%20b%3F.log" ||
		urls[1] != "http://agent.test:8080/api/logs?lines=50&source=mihomo/%E6%97%A5%E5%BF%97%20a" {
		t.Errorf("urls %v", urls)
	}
}

func TestSetLoggingConfigLikePython(t *testing.T) {
	e := newEnv(t)
	id := e.id()
	fails := func(name, body, want string) {
		t.Helper()
		e.fake.set(raw(body, 200))
		if _, err := e.m.SetLoggingConfig(e.ctx, id, true); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %s", name, err, want)
		}
	}
	fails("list", "[1]", "'list' object has no attribute 'get'")
	fails("null", "null", "'NoneType' object has no attribute 'get'")
	e.fake.set(raw(`{"success": true, "x": 1}`, 200))
	if got := must[any](t)(e.m.SetLoggingConfig(e.ctx, id, true)); !jsonx.Equal(got, jsonx.Obj("success", true, "x", 1)) {
		t.Fatalf("ok %v", got)
	}
	if got := e.stored(id).Value("logging_config"); !jsonx.Equal(got, jsonx.Obj("enabled", true)) {
		t.Fatalf("saved %v", got)
	}
	e.fake.set(raw(`{"success": false}`, 200))
	if got := must[any](t)(e.m.SetLoggingConfig(e.ctx, id, false)); !jsonx.Equal(got, jsonx.Obj("success", false)) ||
		!jsonx.Equal(e.stored(id).Value("logging_config"), jsonx.Obj("enabled", true)) {
		t.Fatalf("failure %v", got)
	}
	for _, c := range []struct {
		existing any
		want     string
	}{{nil, "'NoneType' object does not support item assignment"}, {[]any{jsonx.Int(1)}, "list indices must be integers or slices, not str"}} {
		e.editSystem(func(system *jsonx.Object) { e.firstAgent(system).Set("logging_config", c.existing) })
		e.fake.set(raw(`{"success": true}`, 200))
		if _, err := e.m.SetLoggingConfig(e.ctx, id, false); err == nil || err.Error() != c.want {
			t.Errorf("existing %v: %v", c.existing, err)
		}
	}
}

func TestAgentJSONAndDNSHARequestsLikePython(t *testing.T) {
	e := newEnv(t)
	agent := e.get(e.id())
	agent.Set("name", "t")
	fails := func(name string, handle func(Request) (*Response, error), fn func() error, want string) {
		t.Helper()
		e.fake.set(handle)
		if err := fn(); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %s", name, err, want)
		}
	}
	probe := func() error { _, err := e.m.ProbeDomains(e.ctx, agent, jsonx.NewObject(), 0); return err }
	fails("json_404", raw("", 404), probe, "Agent t 不支持域名探测，请升级")
	fails("json_list", raw("[1]", 200), probe, "'list' object has no attribute 'get'")
	fails("json_null", raw("null", 200), probe, "'NoneType' object has no attribute 'get'")
	fails("json_fail_msg", raw(`{"success": false, "message": "busy"}`, 500), probe, "busy")
	fails("json_fail_nomsg", raw("not json", 502), probe, "Agent 返回 HTTP 502")
	fails("region_409", raw("[]", 409), func() error { _, err := e.m.RegionCheck(e.ctx, agent, jsonx.NewObject()); return err },
		"Agent 上的 Mihomo 还没有检测入口，请为该 Agent 开启域名发现后部署一次配置")
	e.fake.set(raw(`{"success": true, "targets": null}`, 200))
	if got := must[any](t)(e.m.RegionTargets(e.ctx, agent)); !jsonx.Equal(got, []any{}) {
		t.Errorf("targets %v", got)
	}
	status := func() error { _, err := e.m.OxidnsHAStatus(e.ctx, agent); return err }
	fails("ha_404", raw("", 404), status, "Agent t 不支持 DNS 高可用，请升级")
	fails("ha_false", raw(`{"success": false, "error": "bad `+agent.GetString("token")+`"}`, 200), status, "bad [REDACTED]")
	e.fake.set(raw("[1]", 200))
	if got := must[*jsonx.Object](t)(e.m.OxidnsHAStatus(e.ctx, agent)); got.Len() != 0 {
		t.Errorf("ha_list %v", got)
	}
	if _, err := e.m.OxidnsHAHandoff(e.ctx, agent, "put", "x"); err == nil || err.Error() != "Unsupported DNS handoff operation" {
		t.Errorf("handoff %v", err)
	}
}
