package agents

// The direct Agent calls of manager.py part A (status, logs, uninstall,
// update, logging configuration, the domain discovery and DNS HA calls).
// Python has no tests for them; the expected values were produced by
// running manager.py with the same Agent answers (testdata/remote_ref.py,
// CPython 3.11 + requests on the test server).

import (
	"configflow/internal/model"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
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
		before := remoteReferenceHead(t, e)
		got, err := fn()
		response, _ := handle(Request{})
		if verifyNativeRemoteReference(t, e, name, string(response.Body), response.StatusCode, want, err, before) {
			return
		}
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
		before := remoteReferenceHead(t, e)
		_, err := e.m.SetLoggingConfig(e.ctx, id, true)
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
		before := remoteReferenceHead(t, e)
		_, err := e.m.SetLoggingConfig(e.ctx, id, false)
		name := "logging_existing_none"
		if c.existing != nil {
			name = "logging_existing_list"
		}
		if verifyNativeRemoteReference(t, e, name, `{"success": true}`, 200, c.want, err, before) {
			continue
		}
		if err == nil || err.Error() != c.want {
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
	before := remoteReferenceHead(t, e)
	got, err := e.m.OxidnsHAStatus(e.ctx, agent)
	if !verifyNativeRemoteReference(t, e, "ha_list", "[1]", 200, jsonx.NewObject(), err, before) {
		if err != nil || got.Len() != 0 {
			t.Errorf("ha_list %v %v", got, err)
		}
	}
	if _, err := e.m.OxidnsHAHandoff(e.ctx, agent, "put", "x"); err == nil || err.Error() != "Unsupported DNS handoff operation" {
		t.Errorf("handoff %v", err)
	}
}

func (m *Manager) UninstallAgent(ctx context.Context, id string) (*jsonx.Object, error) {
	return m.serviceOperation(ctx, id, func() (*jsonx.Object, error) {
		return remoteLegacyObject(m.RunDirectOperation(ctx, id, UninstallService, nil, nil))
	}, true)
}

// UpdateAgentVersion encodes the legacy binary-update envelope.

func (m *Manager) UpdateAgentVersion(ctx context.Context, id, version, binaryURL string) (*jsonx.Object, error) {
	header, body, err := jsonBody(struct {
		Version string `json:"version"`
		URL     string `json:"download_url"`
	}{version, binaryURL})
	if err != nil {
		return nil, err
	}
	return m.serviceOperation(ctx, id, func() (*jsonx.Object, error) {
		return remoteLegacyObject(m.RunDirectOperation(ctx, id, UpdateService, header, body))
	}, false)
}

// ValidateAgentLogPath encodes an opaque legacy path request member.

func (m *Manager) AgentJSONRequest(ctx context.Context, agent *jsonx.Object, method, path string, timeout float64, feature string, payload any, query string) (*jsonx.Object, error) {
	a, err := agentModel(agent)
	if err != nil {
		return nil, err
	}
	var header http.Header
	var body []byte
	if payload != nil {
		header, body, err = jsonBody(payload)
		if err != nil {
			return nil, err
		}
	}
	if query != "" {
		path += "?" + query
	}
	fields, err := m.AgentRequestModel(ctx, a, RemoteRequest{Method: method, Path: path, Timeout: Seconds(timeout), Header: header, Body: body}, feature, false)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(fields)
	return remoteLegacyObject(raw, err)
}

// RemoteError carries a safe user-facing device refusal.

func (m *Manager) ProbeDomains(ctx context.Context, agent *jsonx.Object, payload any, timeout float64) (*jsonx.Object, error) {
	if timeout == 0 {
		timeout = 200
	}
	return m.AgentJSONRequest(ctx, agent, "POST", "/api/domain-probe", timeout, "域名探测", payload, "")
}

// ReturnRoutingStatus adapts the remaining router caller to the native owner.

func (m *Manager) RegionTargets(ctx context.Context, agent *jsonx.Object) (any, error) {
	result, err := m.AgentJSONRequest(ctx, agent, "GET", "/api/region-check/targets", 15, "区域检测", nil, "")
	if err != nil {
		return nil, err
	}
	value := result.Value("targets")
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	present, err := model.JSONHasContent(raw)
	if err != nil {
		return nil, err
	}
	if !present {
		return []any{}, nil
	}
	return value, nil
}

// RegionCheck is region_check(agent, payload).

func (m *Manager) RegionCheck(ctx context.Context, agent *jsonx.Object, payload any) (*jsonx.Object, error) {
	return m.AgentJSONRequest(ctx, agent, "POST", "/api/region-check", 120, "区域检测", payload, "")
}

// OxidnsHARequest adapts the old HA payload to the exact-key native protocol.

func (m *Manager) OxidnsHARequest(ctx context.Context, agent *jsonx.Object, method string, timeout Timeout, path string, payload any) (*jsonx.Object, error) {
	a, err := agentModel(agent)
	if err != nil {
		return nil, err
	}
	var header http.Header
	var body []byte
	if payload != nil {
		header, body, err = jsonBody(payload)
		if err != nil {
			return nil, err
		}
	}
	fields, err := m.AgentRequestModel(ctx, a, RemoteRequest{Method: method, Path: "/api/oxidns-ha" + path, Timeout: timeout, Header: header, Body: body}, "", true)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(fields)
	return remoteLegacyObject(raw, err)
}

// OxidnsHAStatus is oxidns_ha_status(agent).

func (m *Manager) OxidnsHAStatus(ctx context.Context, agent *jsonx.Object) (*jsonx.Object, error) {
	return m.OxidnsHARequest(ctx, agent, "GET", Pair(5, 15), "", nil)
}

// OxidnsHAPreview is oxidns_ha_preview(agent, spec).

func (m *Manager) OxidnsHAPreview(ctx context.Context, agent *jsonx.Object, spec any) (*jsonx.Object, error) {
	return m.OxidnsHARequest(ctx, agent, "POST", Pair(5, 30), "/preview", spec)
}

// OxidnsHAApply is oxidns_ha_apply(agent, spec).

func (m *Manager) OxidnsHAApply(ctx context.Context, agent *jsonx.Object, spec any) (*jsonx.Object, error) {
	return m.OxidnsHARequest(ctx, agent, "PUT", Pair(5, 90), "", spec)
}

// OxidnsHARemove is oxidns_ha_remove(agent).

func (m *Manager) OxidnsHARemove(ctx context.Context, agent *jsonx.Object) (*jsonx.Object, error) {
	return m.OxidnsHARequest(ctx, agent, "DELETE", Pair(5, 90), "", nil)
}

// OxidnsHAHandoff is oxidns_ha_handoff(agent, method, operation_id).

func (m *Manager) OxidnsHAHandoff(ctx context.Context, agent *jsonx.Object, method, operationID string) (*jsonx.Object, error) {
	switch method {
	case "get":
		return m.OxidnsHARequest(ctx, agent, "GET", Pair(5, 15), "/handoff", nil)
	case "post", "delete":
		upper := "POST"
		if method == "delete" {
			upper = "DELETE"
		}
		return m.OxidnsHARequest(ctx, agent, upper, Pair(5, 15), "/handoff", jsonx.Obj("operation_id", operationID))
	}
	return nil, &ValueError{Message: "Unsupported DNS handoff operation"}
}

func AgentURL(agent *jsonx.Object, path string) string {
	a, err := agentModel(agent)
	if err != nil || a == nil {
		return ""
	}
	return "http://" + a.Host + ":" + strconv.FormatInt(int64(a.Port), 10) + path
}

// Call adapts an old record to the single native request owner.

func (m *Manager) legacyRemote(ctx context.Context, id, method, path string, payload any, sources bool) (any, error) {
	var header http.Header
	var body []byte
	if payload != nil {
		var err error
		header, body, err = jsonBody(payload)
		if err != nil {
			return nil, m.remotePayloadError(ctx, id, err)
		}
	}
	raw, _, err := m.RemoteCall(ctx, id, RemoteRequest{Method: method, Path: path, Header: header, Body: body, Timeout: Seconds(5), LegacyLogSources: sources})
	return remoteLegacyValue(raw, err)
}

// PushLegacyConfig adapts old callers to the native publication owner.

func (m *Manager) GetAgentStatus(ctx context.Context, id string) (any, error) {
	return m.legacyRemote(ctx, id, "GET", "/api/status", nil, false)
}

// GetAgentLogSources retains the legacy endpoint fallback for older Agents.

func (m *Manager) GetAgentLogSources(ctx context.Context, id string) (any, error) {
	return m.legacyRemote(ctx, id, "GET", "/api/logs/sources", nil, true)
}

// GetAgentLogs preserves the caller's log selection in its device request.

func (m *Manager) GetAgentLogs(ctx context.Context, id string, lines any, logPath, source string) (any, error) {
	path := "/api/logs?lines=" + jsonx.Str(lines)
	if source != "" {
		path += "&source=" + quote(source)
	} else if logPath != "" {
		path += "&log_path=" + quote(logPath)
	}
	return m.legacyRemote(ctx, id, "GET", path, nil, false)
}

func (m *Manager) ClearAgentLog(ctx context.Context, id string, logPath, source any) (any, error) {
	payload := jsonx.Obj("log_path", logPath)
	if jsonx.Truthy(source) {
		payload = jsonx.Obj("source", source)
	}
	return m.legacyRemote(ctx, id, "POST", "/api/logs/clear", payload, false)
}

// UninstallAgent serializes removal through the HA service-operation guard.

func (m *Manager) ValidateAgentLogPath(ctx context.Context, id string, path any) (any, error) {
	return m.legacyRemote(ctx, id, "POST", "/api/logs/validate", jsonx.Obj("path", path), false)
}

// GetLoggingConfig reads the remote logging endpoint.

func (m *Manager) GetLoggingConfig(ctx context.Context, id string) (any, error) {
	return m.legacyRemote(ctx, id, "GET", "/api/config/logging", nil, false)
}

// SetLoggingConfig adapts the facade input to the typed logging transaction.

func (m *Manager) SetLoggingConfig(ctx context.Context, id string, enabled any) (any, error) {
	header, body, err := jsonBody(jsonx.Obj("enabled", enabled))
	if err != nil {
		return nil, m.remotePayloadError(ctx, id, err)
	}
	raw, err := json.Marshal(enabled)
	if err != nil {
		return nil, err
	}
	return remoteLegacyValue(m.SetLoggingModel(ctx, id, raw, header, body))
}

// AgentJSONRequest encodes an old feature payload and returns an opaque object.
