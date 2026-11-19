package agents

// The AgentManager methods that call an Agent's HTTP API directly (legacy
// configuration push, restart, status, logs, uninstall, update, logging
// configuration, domain discovery probes, return routing, DNS HA).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"

	"configflow/internal/jsonresp"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"golang.org/x/text/encoding/charmap"
)

// AgentURL adapts the legacy address helper to stored typed fields.
func AgentURL(agent *jsonx.Object, path string) string {
	a, err := agentModel(agent)
	if err != nil || a == nil {
		return ""
	}
	return "http://" + a.Host + ":" + strconv.FormatInt(int64(a.Port), 10) + path
}

// Call adapts an old record to the single native request owner.
func (m *Manager) Call(ctx context.Context, agent *jsonx.Object, method, path string, header http.Header, body []byte, timeout Timeout) (*Response, error) {
	a, err := agentModel(agent)
	if err != nil {
		return nil, err
	}
	return m.CallAgent(ctx, a, method, path, header, body, timeout)
}

// ErrHeaderEncoding rejects a header outside the Agent's Latin-1 wire contract.
var ErrHeaderEncoding = errors.New(msgAgentHeaderEncoding)

// EncodeHeaderLatin1 uses the native ISO-8859-1 codec. Invalid values are
// rejected without repeating credentials or interpreter exception details.
func EncodeHeaderLatin1(value string) (string, error) {
	encoded, err := charmap.ISO8859_1.NewEncoder().String(value)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrHeaderEncoding, err)
	}
	return encoded, nil
}

// jsonBody is requests' json= encoding (json.dumps, ASCII, ", " separators)
// with its Content-Type. Like requests 2.31 (allow_nan=False), NaN and
// infinities are refused before anything is sent with InvalidJSONError, a
// RequestException: a *TransportError.
func jsonBody(value any) (http.Header, []byte, error) {
	body, err := jsonresp.EncodeDevice(value)
	if err != nil {
		return nil, nil, &TransportError{Err: errors.New(msgInvalidDevicePayload)}
	}
	return http.Header{"Content-Type": {"application/json"}}, body, nil
}

type PushConfig struct {
	// Extra is extra_data without the bundle (directories,
	// ruleset_downloads, activate, base_url, resource_config,
	// source_snapshot, ...): merged into the legacy JSON payload.
	Extra               *jsonx.Object
	ConfigRevision      any // int or nil
	ExpectedProfileID   *string
	ExpectedServiceType *string
}

// remoteLegacyValue only encodes/decodes the API facade's opaque JSON values.
func remoteLegacyValue(raw json.RawMessage, err error) (any, error) {
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(raw)
}
func remoteLegacyObject(raw json.RawMessage, err error) (*jsonx.Object, error) {
	value, err := remoteLegacyValue(raw, err)
	if err != nil {
		return nil, err
	}
	result, ok := value.(*jsonx.Object)
	if !ok {
		return nil, ErrInvalidAgentReply
	}
	return result, nil
}
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
func (m *Manager) PushLegacyConfig(ctx context.Context, id, content string, push PushConfig) (*jsonx.Object, error) {
	var extra model.Fields
	if push.Extra != nil {
		data, err := json.Marshal(push.Extra)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &extra); err != nil {
			return nil, err
		}
	}
	var revision json.RawMessage
	if push.ConfigRevision != nil {
		var err error
		revision, err = json.Marshal(push.ConfigRevision)
		if err != nil {
			return nil, err
		}
	}
	return remoteLegacyObject(m.PushLegacyConfigModel(ctx, id, content, LegacyConfigOptions{Extra: extra, Revision: revision, ExpectedProfileID: push.ExpectedProfileID, ExpectedServiceType: push.ExpectedServiceType}))
}

// RestartAgentService serializes the native operation with the HA guard.
func (m *Manager) RestartAgentService(ctx context.Context, id string) (*jsonx.Object, error) {
	return m.serviceOperation(ctx, id, func() (*jsonx.Object, error) {
		return remoteLegacyObject(m.RunDirectOperation(ctx, id, RestartService, nil, nil))
	}, false)
}
func (m *Manager) serviceOperation(ctx context.Context, id string, operation func() (*jsonx.Object, error), uninstall bool) (*jsonx.Object, error) {
	if hook := currentHooks().HAServiceOperation; hook != nil {
		return hook(ctx, m, id, operation, uninstall)
	}
	return defaultServiceOperation(ctx, m, id, operation, uninstall)
}

// GetAgentStatus returns the Agent's valid status JSON through the facade codec.
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
func quote(value string) string {
	plus := url.QueryEscape(value)
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

// ClearAgentLog adapts the old source-or-path request envelope.
func (m *Manager) ClearAgentLog(ctx context.Context, id string, logPath, source any) (any, error) {
	payload := jsonx.Obj("log_path", logPath)
	if jsonx.Truthy(source) {
		payload = jsonx.Obj("source", source)
	}
	return m.legacyRemote(ctx, id, "POST", "/api/logs/clear", payload, false)
}

// UninstallAgent serializes removal through the HA service-operation guard.
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
type RemoteError struct{ Message string }

func (e *RemoteError) Error() string { return e.Message }

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

// RegionTargets decodes the feature envelope's targets, defaulting null to [].
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
