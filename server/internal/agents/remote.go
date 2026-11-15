package agents

// The AgentManager methods that call an Agent's HTTP API directly (legacy
// configuration push, restart, status, logs, uninstall, update, logging
// configuration, domain discovery probes, return routing, DNS HA).

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"net/http"
	"strconv"

	"configflow/internal/jsonx"
	"configflow/internal/urlx"
)

// AgentURL is f"http://{agent['host']}:{agent['port']}{path}".
func AgentURL(agent *jsonx.Object, path string) string {
	return "http://" + jsonx.Str(agent.Value("host")) + ":" + jsonx.Str(agent.Value("port")) + path
}

// Call sends one request to the Agent with its Bearer token: path is the
// part after host:port ("/api/status"); body nil sends none. Errors are
// *TransportError.
func (m *Manager) Call(ctx context.Context, agent *jsonx.Object, method, path string, header http.Header, body []byte, timeout Timeout) (*Response, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+jsonx.Str(agent.Value("token")))
	for key, values := range header {
		h[http.CanonicalHeaderKey(key)] = values
	}
	response, err := m.httpClient().Do(ctx, Request{Method: method, URL: AgentURL(agent, path), Header: h, Body: body, Timeout: timeout})
	if err != nil {
		if !IsTransportError(err) {
			err = &TransportError{Err: err}
		}
		return nil, err
	}
	return response, nil
}

// jsonBody is requests' json= encoding (json.dumps, ASCII, ", " separators)
// with its Content-Type.
func jsonBody(value any) (http.Header, []byte, error) {
	body, err := jsonx.Default(value)
	if err != nil {
		return nil, nil, err
	}
	return http.Header{"Content-Type": {"application/json"}}, body, nil
}

// responseJSON is response.json() inside `except RequestException`: a body
// that is not JSON is a requests.JSONDecodeError, itself a RequestException.
func responseJSON(response *Response) (any, error) {
	value, err := response.JSON()
	if err != nil {
		return nil, &TransportError{Err: err}
	}
	return value, nil
}

func agentNotFound() *jsonx.Object {
	return jsonx.Obj("success", false, "message", "Agent not found")
}

func httpFailure(status int) *jsonx.Object {
	return jsonx.Obj("success", false, "message", "HTTP "+strconv.Itoa(status))
}

// connectionError is {'success': False, 'message': f'Connection error: {e}'}.
func connectionError(err error) *jsonx.Object {
	return jsonx.Obj("success", false, "message", "Connection error: "+err.Error())
}

// PushConfig are the arguments of push_config_to_agent besides agent_id and
// config_content.
type PushConfig struct {
	// Extra is extra_data without the bundle (directories,
	// ruleset_downloads, activate, base_url, resource_config,
	// source_snapshot, ...): merged into the legacy JSON payload.
	Extra *jsonx.Object
	// Bundle is extra_data['deployment_bundle'] (#24), nil when absent.
	Bundle              any
	ConfigRevision      any // int or nil
	ExpectedProfileID   *string
	ExpectedServiceType *string
}

// PushConfigToAgent is push_config_to_agent: mihomo/mosdns/oxidns Agents (and
// every call with a bundle) publish transactionally through the
// PublishDeployment hook; other services get the legacy /api/config/update.
func (m *Manager) PushConfigToAgent(ctx context.Context, agentID, configContent string, push PushConfig) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return agentNotFound(), nil
	}
	if push.ExpectedProfileID != nil || push.ExpectedServiceType != nil {
		mismatch := false
		if push.ExpectedProfileID != nil {
			profile, err := m.profileOf(ctx, agent)
			if err != nil {
				return nil, err
			}
			mismatch = !jsonx.Equal(profile, *push.ExpectedProfileID)
		}
		if !mismatch && push.ExpectedServiceType != nil {
			mismatch = !jsonx.Equal(agent.GetOr("service_type", "mihomo"), *push.ExpectedServiceType)
		}
		if mismatch {
			return jsonx.Obj("success", false, "message", "草稿已保存，但设备绑定已改变，请重新选择设备后发布。",
				"http_status", jsonx.Int(409)), nil
		}
	}
	service := agent.GetOr("service_type", "mihomo")
	if push.Bundle != nil || service == "mihomo" || service == "mosdns" || service == "oxidns" {
		if push.Bundle == nil {
			return jsonx.Obj("success", false, "message", "A complete deployment bundle is required", "http_status", jsonx.Int(409)), nil
		}
		options := PublishOptions{Activate: true, ConfigRevision: push.ConfigRevision}
		if value, ok := push.Extra.Get("activate"); ok {
			options.Activate = jsonx.Truthy(value)
		}
		options.BaseURL, _ = push.Extra.GetOr("base_url", "").(string)
		options.ResourceConfig, _ = push.Extra.Value("resource_config").(*jsonx.Object)
		options.SourceSnapshot, _ = push.Extra.Value("source_snapshot").(*jsonx.Object)
		return m.PublishDeployment(ctx, agentID, push.Bundle, options)
	}

	sum := md5.Sum([]byte(configContent))
	configMD5 := hex.EncodeToString(sum[:])
	payload := jsonx.Obj("config", configContent, "md5", configMD5)
	if push.Extra.Len() > 0 {
		payload.Update(push.Extra)
	}
	// 手动序列化JSON，确保中文不被转义
	body, err := jsonx.Encode(payload, jsonx.Options{})
	if err != nil {
		return nil, err
	}
	response, err := m.Call(ctx, agent, "POST", "/api/config/update",
		http.Header{"Content-Type": {"application/json; charset=utf-8"}}, body, Seconds(10))
	if err != nil {
		return jsonx.Obj("success", false, "message", "Unable to connect to Agent",
			"error_details", FailureText(err.Error(), tokenSecrets(agent), 0)), nil
	}
	if response.StatusCode != 200 {
		return ResponseFailure(response, "HTTP "+strconv.Itoa(response.StatusCode), tokenSecrets(agent)), nil
	}
	value, jsonErr := response.JSON()
	result, isObject := value.(*jsonx.Object)
	if jsonErr != nil || !isObject {
		return ResponseFailure(response, "Invalid Agent response", tokenSecrets(agent)), nil
	}
	if !jsonx.Truthy(result.Value("success")) {
		return ResponseFailure(response, "Config update failed", tokenSecrets(agent)), nil
	}
	_, err = m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
		item, _ := findAgent(*agents, agentID)
		if item == nil {
			return false, nil
		}
		item.Set("config_version", configMD5[:8])
		item.Set("pushed_at", utcNowZ())
		if push.ConfigRevision != nil {
			item.Set("config_revision", push.ConfigRevision)
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return jsonx.Obj("success", true, "message", "Config pushed successfully"), nil
}

// RestartAgentService is restart_agent_service (through the DNS HA service
// operation guard, #29).
func (m *Manager) RestartAgentService(ctx context.Context, agentID string) (*jsonx.Object, error) {
	return m.serviceOperation(ctx, agentID, func() (*jsonx.Object, error) { return m.restartAgentService(ctx, agentID) }, false)
}

func (m *Manager) serviceOperation(ctx context.Context, agentID string, operation func() (*jsonx.Object, error), uninstall bool) (*jsonx.Object, error) {
	if hook := currentHooks().HAServiceOperation; hook != nil {
		return hook(ctx, m, agentID, operation, uninstall)
	}
	return defaultServiceOperation(ctx, m, agentID, operation, uninstall)
}

func (m *Manager) restartAgentService(ctx context.Context, agentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return agentNotFound(), nil
	}
	if upgradeBusy(agent) {
		return jsonx.Obj("success", false, "message", "Agent update is in progress"), nil
	}
	response, err := m.Call(ctx, agent, "POST", "/api/restart", nil, nil, Seconds(10))
	if err != nil {
		return jsonx.Obj("success", false, "message", "Unable to connect to Agent",
			"error_details", FailureText(err.Error(), tokenSecrets(agent), 0)), nil
	}
	if response.StatusCode != 200 {
		return ResponseFailure(response, "HTTP "+strconv.Itoa(response.StatusCode), tokenSecrets(agent)), nil
	}
	value, jsonErr := response.JSON()
	result, isObject := value.(*jsonx.Object)
	if jsonErr != nil || !isObject {
		return ResponseFailure(response, "Invalid Agent response", tokenSecrets(agent)), nil
	}
	if jsonx.Truthy(result.Value("success")) {
		return result, nil
	}
	return ResponseFailure(response, "Restart failed", tokenSecrets(agent)), nil
}

// simpleGet is the shape of get_agent_status / get_logging_config: the JSON
// of a 200, "HTTP <status>" otherwise, "Connection error: ..." on transport
// failures (a non-JSON 200 included).
func (m *Manager) simpleCall(ctx context.Context, agentID, method, path string, payload any, timeout Timeout,
	onStatus func(status int) (any, bool)) (any, *jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, nil, err
	}
	if agent == nil {
		return agentNotFound(), nil, nil
	}
	var header http.Header
	var body []byte
	if payload != nil {
		if header, body, err = jsonBody(payload); err != nil {
			return nil, nil, err
		}
	}
	response, err := m.Call(ctx, agent, method, path, header, body, timeout)
	if err != nil {
		return connectionError(err), agent, nil
	}
	if response.StatusCode == 200 {
		value, err := responseJSON(response)
		if err != nil {
			return connectionError(err), agent, nil
		}
		return value, agent, nil
	}
	if onStatus != nil {
		if value, ok := onStatus(response.StatusCode); ok {
			return value, agent, nil
		}
	}
	return httpFailure(response.StatusCode), agent, nil
}

// GetAgentStatus is get_agent_status(agent_id): the Agent's /api/status JSON
// (any JSON value) or a failure object.
func (m *Manager) GetAgentStatus(ctx context.Context, agentID string) (any, error) {
	value, _, err := m.simpleCall(ctx, agentID, "GET", "/api/status", nil, Seconds(5), nil)
	return value, err
}

// GetAgentLogSources is get_agent_log_sources(agent_id): Agents without the
// endpoint (404) give {'success': True, 'legacy': True, 'sources': []}.
func (m *Manager) GetAgentLogSources(ctx context.Context, agentID string) (any, error) {
	value, _, err := m.simpleCall(ctx, agentID, "GET", "/api/logs/sources", nil, Seconds(5), func(status int) (any, bool) {
		if status == 404 {
			return jsonx.Obj("success", true, "legacy", true, "sources", []any{}), true
		}
		return nil, false
	})
	return value, err
}

// GetAgentLogs is get_agent_logs(agent_id, lines, log_path, source).
func (m *Manager) GetAgentLogs(ctx context.Context, agentID string, lines any, logPath, source string) (any, error) {
	path := "/api/logs?lines=" + jsonx.Str(lines)
	if source != "" {
		path += "&source=" + quote(source)
	} else if logPath != "" {
		path += "&log_path=" + quote(logPath)
	}
	value, _, err := m.simpleCall(ctx, agentID, "GET", path, nil, Seconds(5), nil)
	return value, err
}

// quote is urllib.parse.quote(value) (safe='/').
func quote(value string) string {
	plus := urlx.QuotePlus(value)
	// quote_plus encodes "/" and " "; quote keeps "/" and writes %20.
	out := make([]byte, 0, len(plus))
	for i := 0; i < len(plus); i++ {
		switch {
		case plus[i] == '+':
			out = append(out, "%20"...)
		case plus[i] == '%' && i+2 < len(plus) && plus[i+1] == '2' && (plus[i+2] == 'F' || plus[i+2] == 'f'):
			out = append(out, '/')
			i += 2
		default:
			out = append(out, plus[i])
		}
	}
	return string(out)
}

// ClearAgentLog is clear_agent_log(agent_id, log_path, source).
func (m *Manager) ClearAgentLog(ctx context.Context, agentID, logPath, source string) (any, error) {
	payload := jsonx.Obj("log_path", logPath)
	if source != "" {
		payload = jsonx.Obj("source", source)
	}
	value, _, err := m.simpleCall(ctx, agentID, "POST", "/api/logs/clear", payload, Seconds(5), nil)
	return value, err
}

// UninstallAgent is uninstall_agent (through the DNS HA service operation
// guard, #29).
func (m *Manager) UninstallAgent(ctx context.Context, agentID string) (*jsonx.Object, error) {
	return m.serviceOperation(ctx, agentID, func() (*jsonx.Object, error) { return m.uninstallAgent(ctx, agentID) }, true)
}

func (m *Manager) uninstallAgent(ctx context.Context, agentID string) (*jsonx.Object, error) {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return agentNotFound(), nil
	}
	if upgradeBusy(agent) {
		return jsonx.Obj("success", false, "message", "Agent update is in progress"), nil
	}
	response, err := m.Call(ctx, agent, "POST", "/api/uninstall", nil, nil, Seconds(10))
	if err != nil {
		return connectionError(err), nil
	}
	if response.StatusCode != 200 {
		return httpFailure(response.StatusCode), nil
	}
	if _, err := responseJSON(response); err != nil {
		return connectionError(err), nil
	}
	return jsonx.Obj("success", true, "message", "Agent uninstall started. The agent will be removed in a few seconds."), nil
}

// UpdateAgentVersion is update_agent_version (through the DNS HA service
// operation guard, #29).
func (m *Manager) UpdateAgentVersion(ctx context.Context, agentID, newVersion, binaryURL string) (*jsonx.Object, error) {
	return m.serviceOperation(ctx, agentID, func() (*jsonx.Object, error) {
		agent, err := m.GetAgentByID(ctx, agentID, true)
		if err != nil {
			return nil, err
		}
		if agent == nil {
			return agentNotFound(), nil
		}
		header, body, err := jsonBody(jsonx.Obj("version", newVersion, "download_url", binaryURL))
		if err != nil {
			return nil, err
		}
		response, err := m.Call(ctx, agent, "POST", "/api/update", header, body, Seconds(10))
		if err != nil {
			return connectionError(err), nil
		}
		if response.StatusCode == 200 {
			return jsonx.Obj("success", true, "message", "Agent update started"), nil
		}
		return httpFailure(response.StatusCode), nil
	}, false)
}

// ValidateAgentLogPath is validate_agent_log_path(agent_id, log_path).
func (m *Manager) ValidateAgentLogPath(ctx context.Context, agentID, logPath string) (any, error) {
	value, _, err := m.simpleCall(ctx, agentID, "POST", "/api/logs/validate", jsonx.Obj("path", logPath), Seconds(5), nil)
	return value, err
}

// GetLoggingConfig is get_logging_config(agent_id).
func (m *Manager) GetLoggingConfig(ctx context.Context, agentID string) (any, error) {
	value, _, err := m.simpleCall(ctx, agentID, "GET", "/api/config/logging", nil, Seconds(5), nil)
	return value, err
}

// SetLoggingConfig is set_logging_config(agent_id, enabled): a successful
// answer is recorded as logging_config.enabled.
func (m *Manager) SetLoggingConfig(ctx context.Context, agentID string, enabled bool) (any, error) {
	value, agent, err := m.simpleCall(ctx, agentID, "POST", "/api/config/logging", jsonx.Obj("enabled", enabled), Seconds(5), nil)
	if err != nil || agent == nil {
		return value, err
	}
	// The failure objects of simpleCall carry success=False; a 200 answer
	// that is JSON but not an object fails on result.get('success') with
	// an AttributeError, which Python does not catch.
	result, isObject := value.(*jsonx.Object)
	if !isObject {
		return nil, &RemoteError{Message: "'" + pyTypeName(value) + "' object has no attribute 'get'"}
	}
	if jsonx.Truthy(result.Value("success")) {
		_, err = m.UpdateAgents(ctx, "", func(_ context.Context, agents *[]any) (any, error) {
			item, _ := findAgent(*agents, agentID)
			if item == nil {
				return false, nil
			}
			// item.setdefault('logging_config', {})['enabled'] = enabled
			if !item.Has("logging_config") {
				item.Set("logging_config", jsonx.NewObject())
			}
			config, ok := item.Value("logging_config").(*jsonx.Object)
			if !ok {
				existing := item.Value("logging_config")
				if _, isList := existing.([]any); isList {
					return nil, &RemoteError{Message: "list indices must be integers or slices, not str"}
				}
				return nil, &RemoteError{Message: "'" + pyTypeName(existing) + "' object does not support item assignment"}
			}
			config.Set("enabled", enabled)
			return true, nil
		})
		if err != nil {
			return nil, err
		}
	}
	return value, nil
}

// AgentJSONRequest is _agent_json_request(agent, method, path, timeout,
// feature, ...): the domain discovery family of calls. Failures are errors
// whose text can be shown to the user (a *TransportError for transport
// failures, a *RemoteError otherwise).
func (m *Manager) AgentJSONRequest(ctx context.Context, agent *jsonx.Object, method, path string, timeout float64, feature string, payload any, query string) (*jsonx.Object, error) {
	var header http.Header
	var body []byte
	var err error
	if payload != nil {
		if header, body, err = jsonBody(payload); err != nil {
			return nil, err
		}
	}
	if query != "" {
		path += "?" + query
	}
	response, err := m.Call(ctx, agent, method, path, header, body, Seconds(timeout))
	if err != nil {
		return nil, err
	}
	if response.StatusCode == 404 {
		return nil, &RemoteError{Message: "Agent " + jsonx.Str(truthyOr(agent.Value("name"), agent.Value("id"))) + " 不支持" + feature + "，请升级"}
	}
	value, jsonErr := response.JSON()
	if jsonErr != nil {
		value = jsonx.NewObject()
	}
	if response.StatusCode == 409 && len(path) >= len("/api/region-check") && path[:len("/api/region-check")] == "/api/region-check" {
		return nil, &RemoteError{Message: "Agent 上的 Mihomo 还没有检测入口，请为该 Agent 开启域名发现后部署一次配置"}
	}
	result, isObject := value.(*jsonx.Object)
	if !isObject {
		return nil, &RemoteError{Message: "'" + pyTypeName(value) + "' object has no attribute 'get'"}
	}
	if response.StatusCode != 200 || !jsonx.Truthy(result.Value("success")) {
		message := result.Value("message")
		if !jsonx.Truthy(message) {
			message = "Agent 返回 HTTP " + strconv.Itoa(response.StatusCode)
		}
		return nil, &RemoteError{Message: jsonx.Str(message)}
	}
	return result, nil
}

// RemoteError is the RuntimeError the Agent call helpers raise with a
// message for the user.
type RemoteError struct{ Message string }

func (e *RemoteError) Error() string { return e.Message }

func pyTypeName(value any) string {
	switch value.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case string:
		return "str"
	case []any:
		return "list"
	case *jsonx.Object:
		return "dict"
	case jsonx.Number:
		if value.(jsonx.Number).IsInt() {
			return "int"
		}
		return "float"
	}
	return "object"
}

// ProbeDomains is probe_domains(agent, payload, timeout=200).
func (m *Manager) ProbeDomains(ctx context.Context, agent *jsonx.Object, payload any, timeout float64) (*jsonx.Object, error) {
	if timeout == 0 {
		timeout = 200
	}
	return m.AgentJSONRequest(ctx, agent, "POST", "/api/domain-probe", timeout, "域名探测", payload, "")
}

// ReturnRoutingStatus is return_routing_status(agent).
func (m *Manager) ReturnRoutingStatus(ctx context.Context, agent *jsonx.Object) (*jsonx.Object, error) {
	return m.AgentJSONRequest(ctx, agent, "GET", "/api/return-routing", 15, "回流规则状态", nil, "")
}

// ReturnRoutingApply is return_routing_apply(agent, spec).
func (m *Manager) ReturnRoutingApply(ctx context.Context, agent *jsonx.Object, spec any) (*jsonx.Object, error) {
	return m.AgentJSONRequest(ctx, agent, "PUT", "/api/return-routing", 60, "回流规则管理", spec, "")
}

// ReturnRoutingRemove is return_routing_remove(agent, restore_legacy).
func (m *Manager) ReturnRoutingRemove(ctx context.Context, agent *jsonx.Object, restoreLegacy bool) (*jsonx.Object, error) {
	query := ""
	if restoreLegacy {
		query = "restore_legacy=1"
	}
	return m.AgentJSONRequest(ctx, agent, "DELETE", "/api/return-routing", 60, "回流规则管理", nil, query)
}

// RegionTargets is region_targets(agent): the targets list (or []).
func (m *Manager) RegionTargets(ctx context.Context, agent *jsonx.Object) (any, error) {
	result, err := m.AgentJSONRequest(ctx, agent, "GET", "/api/region-check/targets", 15, "区域检测", nil, "")
	if err != nil {
		return nil, err
	}
	return truthyOr(result.Value("targets"), []any{}), nil
}

// RegionCheck is region_check(agent, payload).
func (m *Manager) RegionCheck(ctx context.Context, agent *jsonx.Object, payload any) (*jsonx.Object, error) {
	return m.AgentJSONRequest(ctx, agent, "POST", "/api/region-check", 120, "区域检测", payload, "")
}

// OxidnsHARequest is _oxidns_ha_request(agent, method, timeout, path, ...):
// the DNS HA calls; a success answer is the state itself (no success field).
func (m *Manager) OxidnsHARequest(ctx context.Context, agent *jsonx.Object, method string, timeout Timeout, path string, payload any) (*jsonx.Object, error) {
	var header http.Header
	var body []byte
	var err error
	if payload != nil {
		if header, body, err = jsonBody(payload); err != nil {
			return nil, err
		}
	}
	response, err := m.Call(ctx, agent, method, "/api/oxidns-ha"+path, header, body, timeout)
	if err != nil {
		return nil, err
	}
	if response.StatusCode == 404 {
		return nil, &RemoteError{Message: "Agent " + jsonx.Str(truthyOr(agent.Value("name"), agent.Value("id"))) + " 不支持 DNS 高可用，请升级"}
	}
	value, _ := response.JSON()
	result, isObject := value.(*jsonx.Object)
	if !isObject {
		result = jsonx.NewObject()
	}
	if response.StatusCode != 200 || result.Value("success") == false {
		message := result.Value("error")
		if !jsonx.Truthy(message) {
			message = result.Value("message")
		}
		if !jsonx.Truthy(message) {
			message = "Agent 返回 HTTP " + strconv.Itoa(response.StatusCode)
		}
		return nil, &RemoteError{Message: FailureText(jsonx.Str(message), tokenSecrets(agent), 0)}
	}
	return result, nil
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
