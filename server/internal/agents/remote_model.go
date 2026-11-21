package agents

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"configflow/internal/jsonresp"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// RemoteRequest carries already encoded device bytes. Header/body ownership and
// the original stored credential are enforced by CallAgent.
type RemoteRequest struct {
	Method, Path     string
	Header           http.Header
	Body             []byte
	Timeout          Timeout
	LegacyLogSources bool
}

func remoteFailure(message string) (json.RawMessage, error) {
	return json.Marshal(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{false, message})
}
func remoteSuccess(message string) (json.RawMessage, error) {
	return json.Marshal(struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}{true, message})
}
func remoteConnection(err error, secrets redact.Secrets) (json.RawMessage, error) {
	return remoteFailure("Connection error: " + redact.DiagnosticText(err.Error(), secrets, 0))
}
func remoteFields(data json.RawMessage) (model.Fields, error) {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return fields, ErrInvalidAgentReply
	}
	return fields, nil
}
func remoteFlag(fields model.Fields, name string) (bool, error) {
	raw, present := fields.Get(name)
	if !present {
		return false, nil
	}
	var flag *bool
	if json.Unmarshal(raw, &flag) != nil || flag == nil {
		return false, ErrInvalidAgentReply
	}
	return *flag, nil
}
func remoteText(fields model.Fields, name string) string {
	raw, present := fields.Get(name)
	text, _ := storedText(raw, present, nil)
	return text
}

// RemoteCall reads one selected live Agent and sends one request. Arbitrary valid
// JSON remains opaque for status/log APIs; command-specific owners validate it.
func (m *Manager) RemoteCall(ctx context.Context, id string, request RemoteRequest) (json.RawMessage, *model.Agent, error) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		ctx = lifecycle.Detach(ctx)
	}
	a, err := m.ReadAgent(ctx, id, false)
	if err != nil {
		return nil, nil, err
	}
	if a == nil {
		raw, err := remoteFailure(msgAgentNotFound)
		return raw, nil, err
	}
	response, err := m.CallAgent(ctx, a, request.Method, request.Path, request.Header, request.Body, request.Timeout)
	if err != nil {
		if !IsTransportError(err) {
			return nil, a, err
		}
		raw, err := remoteConnection(err, agentDiagnosticSecrets(a))
		return raw, a, err
	}
	if response.StatusCode == 200 {
		data := response.jsonData()
		// Dynamic validates all JSON members recursively without a tag-based decoder.
		var value model.Dynamic
		if json.Unmarshal(data, &value) != nil {
			return nil, a, ErrInvalidAgentReply
		}
		return data, a, nil
	}
	if response.StatusCode == 404 && request.LegacyLogSources {
		return []byte(`{"success":true,"legacy":true,"sources":[]}`), a, nil
	}
	raw, err := remoteFailure("HTTP " + strconv.Itoa(response.StatusCode))
	return raw, a, err
}

// SetLoggingModel records a successful setting in one typed transaction. Neither
// malformed upstream replies nor an invalid stored config can produce a write.
func (m *Manager) SetLoggingModel(ctx context.Context, id string, enabled json.RawMessage, header http.Header, body []byte) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	result, a, err := m.RemoteCall(ctx, id, RemoteRequest{Method: http.MethodPost, Path: "/api/config/logging", Timeout: Seconds(5), Header: header, Body: body})
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrAgentNotFound
	}
	fields, err := remoteFields(result)
	if err != nil {
		return nil, err
	}
	success, err := remoteFlag(fields, "success")
	if err != nil {
		return nil, err
	}
	if !success {
		return result, nil
	}
	err = m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		current := registryAgent(*records, id)
		if current == nil {
			return nil
		}
		raw, present, err := current.MemberJSON("logging_config")
		if err != nil {
			return err
		}
		var config model.Fields
		if present && json.Unmarshal(raw, &config) != nil {
			return repo.NewValidationError(msgLoggingConfigShape)
		}
		if err := config.Set("enabled", enabled); err != nil {
			return err
		}
		raw, err = json.Marshal(config)
		if err != nil {
			return err
		}
		return current.SetJSON("logging_config", raw)
	})
	return result, err
}

// DirectOperation identifies guarded service operations sent to one Agent.
type DirectOperation uint8

const (
	// RestartService asks the Agent to restart its managed service.
	RestartService DirectOperation = iota
	// UninstallService starts removal while preserving the server's registry entry.
	UninstallService
	// UpdateService starts the legacy Agent binary update endpoint.
	UpdateService
)

// RunDirectOperation owns direct service request/response semantics. DNS HA
// serialization remains with the surrounding service-operation guard.
func (m *Manager) RunDirectOperation(ctx context.Context, id string, operation DirectOperation, header http.Header, body []byte) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	a, err := m.ReadAgent(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return nil, ErrAgentNotFound
	}
	if operation != UpdateService && upgradeBusyModel(a) {
		return remoteFailure(msgDeploymentUpgradeBusy)
	}
	path := "/api/restart"
	switch operation {
	case UninstallService:
		path = "/api/uninstall"
	case UpdateService:
		path = "/api/update"
	}
	response, err := m.CallAgent(ctx, a, http.MethodPost, path, header, body, Seconds(10))
	if err != nil {
		if !IsTransportError(err) {
			return nil, err
		}
		if operation == RestartService {
			return json.Marshal(struct {
				Success bool   `json:"success"`
				Message string `json:"message"`
				Details string `json:"error_details"`
			}{false, "Unable to connect to Agent", redact.DiagnosticText(err.Error(), agentDiagnosticSecrets(a), 0)})
		}
		return remoteConnection(err, agentDiagnosticSecrets(a))
	}
	if response.StatusCode != 200 {
		if operation == RestartService {
			failure, err := ResponseFailureModel(response, "HTTP "+strconv.Itoa(response.StatusCode), agentDiagnosticSecrets(a))
			if err != nil {
				return nil, err
			}
			return json.Marshal(failure)
		}
		return remoteFailure("HTTP " + strconv.Itoa(response.StatusCode))
	}
	if operation == UpdateService {
		return remoteSuccess("Agent update started")
	}
	if operation == UninstallService {
		var reply model.Dynamic
		if json.Unmarshal(response.jsonData(), &reply) != nil {
			return nil, ErrInvalidAgentReply
		}
		return remoteSuccess("Agent uninstall started. The agent will be removed in a few seconds.")
	}
	fields, err := remoteFields(response.jsonData())
	if err != nil {
		return nil, err
	}
	success, err := remoteFlag(fields, "success")
	if err != nil {
		return nil, err
	}
	if success {
		return response.jsonData(), nil
	}
	failure, err := ResponseFailureModel(response, "Restart failed", agentDiagnosticSecrets(a))
	if err != nil {
		return nil, err
	}
	return json.Marshal(failure)
}

// AgentRequestModel calls an object-valued feature endpoint using exact names.
// DNSHA replies are states without a required success member.
func (m *Manager) AgentRequestModel(ctx context.Context, a *model.Agent, request RemoteRequest, feature string, dnsHA bool) (model.Fields, error) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		ctx = lifecycle.Detach(ctx)
	}
	response, err := m.CallAgent(ctx, a, request.Method, request.Path, request.Header, request.Body, request.Timeout)
	if err != nil {
		return model.Fields{}, err
	}
	label := a.Name
	if label == "" {
		label = a.ID
	}
	label = redact.DiagnosticText(label, agentDiagnosticSecrets(a), 120)
	if response.StatusCode == 404 {
		if dnsHA {
			return model.Fields{}, &RemoteError{Message: fmt.Sprintf(msgAgentDNSHAUnsupported, label)}
		}
		return model.Fields{}, &RemoteError{Message: fmt.Sprintf(msgAgentFeatureUnsupported, label, feature)}
	}
	if response.StatusCode == 409 && strings.HasPrefix(request.Path, "/api/region-check") {
		return model.Fields{}, &RemoteError{Message: msgRegionIngress}
	}
	fields, parseErr := remoteFields(response.jsonData())
	if parseErr != nil && response.StatusCode == 200 {
		return model.Fields{}, parseErr
	}
	success, flagErr := remoteFlag(fields, "success")
	if flagErr != nil && response.StatusCode == 200 {
		return model.Fields{}, flagErr
	}
	_, hasSuccess := fields.Get("success")
	if response.StatusCode != 200 || !dnsHA && !success || dnsHA && hasSuccess && !success {
		message := ""
		if dnsHA {
			message = remoteText(fields, "error")
		}
		if message == "" {
			message = remoteText(fields, "message")
		}
		if message == "" {
			message = "Agent 返回 HTTP " + strconv.Itoa(response.StatusCode)
		}
		return model.Fields{}, &RemoteError{Message: redact.DiagnosticText(message, agentDiagnosticSecrets(a), 0)}
	}
	return fields, nil
}

// LegacyConfigOptions carries the nontransactional config endpoint's wire data.
// Captured identity strings are checked against original stored member tokens.
type LegacyConfigOptions struct {
	Extra                                  model.Fields
	Revision                               json.RawMessage
	ExpectedProfileID, ExpectedServiceType *string
}

// PushLegacyConfigModel refuses transactional services and writes only confirmed
// legacy publication metadata after the guarded request succeeds.
func (m *Manager) PushLegacyConfigModel(ctx context.Context, id, content string, options LegacyConfigOptions) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	a, err := m.ReadAgent(ctx, id, false)
	if err != nil {
		return nil, err
	}
	if a == nil {
		return remoteFailure(msgAgentNotFound)
	}
	service, present, err := a.MemberJSON("service_type")
	if err != nil {
		return nil, err
	}
	serviceName, valid := storedText(service, present, nil)
	if !present {
		serviceName = "mihomo"
		valid = true
	}
	mismatch := !valid
	if options.ExpectedProfileID != nil {
		profile, err := m.ResolveAgentProfile(ctx, a)
		if err != nil {
			return nil, err
		}
		mismatch = mismatch || profile != *options.ExpectedProfileID
	}
	if options.ExpectedServiceType != nil {
		mismatch = mismatch || serviceName != *options.ExpectedServiceType
	}
	if mismatch {
		failure, err := deploymentFailure(msgLegacyBindingChanged, 409)
		if err != nil {
			return nil, err
		}
		return json.Marshal(failure)
	}
	if serviceName == "mihomo" || serviceName == "mosdns" || serviceName == "oxidns" {
		failure, err := deploymentFailure("A complete deployment bundle is required", 409)
		if err != nil {
			return nil, err
		}
		return json.Marshal(failure)
	}
	checksum := md5.Sum([]byte(content))
	version := hex.EncodeToString(checksum[:])
	var payload model.Fields
	for _, item := range []struct{ name, value string }{{"config", content}, {"md5", version}} {
		raw, err := json.Marshal(item.value)
		if err != nil {
			return nil, err
		}
		if err := payload.Set(item.name, raw); err != nil {
			return nil, err
		}
	}
	for key, raw := range options.Extra.All() {
		if err := payload.Set(key, raw); err != nil {
			return nil, err
		}
	}
	body, err := jsonresp.Encode(payload)
	if err != nil {
		return nil, err
	}
	response, err := m.CallAgent(ctx, a, http.MethodPost, "/api/config/update", http.Header{"Content-Type": {"application/json; charset=utf-8"}}, body, Seconds(10))
	if err != nil {
		return json.Marshal(struct {
			Success bool   `json:"success"`
			Message string `json:"message"`
			Details string `json:"error_details"`
		}{false, "Unable to connect to Agent", redact.DiagnosticText(err.Error(), agentDiagnosticSecrets(a), 0)})
	}
	if response.StatusCode != 200 {
		failure, err := ResponseFailureModel(response, "HTTP "+strconv.Itoa(response.StatusCode), agentDiagnosticSecrets(a))
		if err != nil {
			return nil, err
		}
		return json.Marshal(failure)
	}
	fields, err := remoteFields(response.jsonData())
	if err != nil {
		return nil, err
	}
	success, err := remoteFlag(fields, "success")
	if err != nil {
		return nil, err
	}
	if !success {
		failure, err := ResponseFailureModel(response, "Config update failed", agentDiagnosticSecrets(a))
		if err != nil {
			return nil, err
		}
		return json.Marshal(failure)
	}
	err = m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		current := registryAgent(*records, id)
		if current == nil {
			return nil
		}
		current.ConfigVersion = version[:8]
		current.PushedAt = new(utcNowZ())
		if len(options.Revision) > 0 && string(options.Revision) != "null" {
			return current.SetJSON("config_revision", options.Revision)
		}
		return tx.Err()
	})
	if err != nil {
		return nil, err
	}
	return remoteSuccess("Config pushed successfully")
}

// Encoding failures cannot hide an already unusable endpoint in the old facade.
// This lookup is error-only; successful request preparation reads the Agent once.
func (m *Manager) remotePayloadError(ctx context.Context, id string, payloadErr error) error {
	a, err := m.ReadAgent(ctx, id, false)
	if err != nil {
		return err
	}
	if a != nil {
		_, hostOK := storedText(a.MemberJSON("host"))
		_, tokenOK := storedText(a.MemberJSON("token"))
		_, portOK, err := a.MemberJSON("port")
		if !hostOK || !tokenOK || !portOK || err != nil {
			return ErrAgentEndpoint
		}
	}
	return payloadErr
}

// RunServiceOperation fences one native direct operation with the HA owner.
// The callback runs once under its lease and retains shutdown cancellation.
func (m *Manager) RunServiceOperation(ctx context.Context, id string, operation DirectOperation, header http.Header, body []byte) (json.RawMessage, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	run := func() (json.RawMessage, error) { return m.RunDirectOperation(ctx, id, operation, header, body) }
	uninstall := operation == UninstallService
	if hook := currentHooks().HAServiceOperationTyped; hook != nil {
		return hook(ctx, m, id, run, uninstall)
	}
	a, err := m.ReadAgent(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if rejection, err := haOperationConflict(a); rejection != nil || err != nil {
		if err != nil {
			return nil, err
		}
		return model.Marshal(rejection)
	}
	if uninstall && HAEnabledModel(a) {
		rejection, err := deploymentFailure(msgHAUninstall, 409)
		if err != nil {
			return nil, err
		}
		return model.Marshal(rejection)
	}
	return run()
}

// AgentLogsPath encodes the established Agent log-selection query.
func AgentLogsPath(lines, logPath, source string) string {
	path := "/api/logs?lines=" + lines
	if source != "" {
		return path + "&source=" + quote(source)
	}
	if logPath != "" {
		return path + "&log_path=" + quote(logPath)
	}
	return path
}

// RemotePayloadError preserves the endpoint guard when a caller cannot encode
// a device payload. It performs no device request or state mutation.
func (m *Manager) RemotePayloadError(ctx context.Context, id string, payloadErr error) error {
	return m.remotePayloadError(ctx, id, payloadErr)
}

// FeatureRequestModel encodes a native JSON payload at the Agent transport owner.
// A nil payload sends no body, including for GET and DELETE requests.
func (m *Manager) FeatureRequestModel(ctx context.Context, a *model.Agent, request RemoteRequest, feature string, payload json.RawMessage) (model.Fields, error) {
	if payload != nil {
		header, body, err := jsonBody(payload)
		if err != nil {
			return model.Fields{}, err
		}
		request.Header, request.Body = header, body
	}
	return m.AgentRequestModel(ctx, a, request, feature, false)
}

// ProbeDomainsModel sends a bounded domain-probe batch through the native owner.
func (m *Manager) ProbeDomainsModel(ctx context.Context, a *model.Agent, payload json.RawMessage, timeout Timeout) (model.Fields, error) {
	if timeout == (Timeout{}) {
		timeout = Seconds(200)
	}
	return m.FeatureRequestModel(ctx, a, RemoteRequest{Method: http.MethodPost, Path: "/api/domain-probe", Timeout: timeout}, msgDomainProbe, payload)
}

// RegionTargetsModel reads the Agent's available region paths; empty values become [].
func (m *Manager) RegionTargetsModel(ctx context.Context, a *model.Agent) (json.RawMessage, error) {
	fields, err := m.FeatureRequestModel(ctx, a, RemoteRequest{Method: http.MethodGet, Path: "/api/region-check/targets", Timeout: Seconds(15)}, msgRegionCheck, nil)
	if err != nil {
		return nil, err
	}
	raw, _ := fields.Get("targets")
	present, err := model.JSONHasContent(raw)
	if err != nil {
		return nil, err
	}
	if !present {
		return json.RawMessage("[]"), nil
	}
	return raw, nil
}

// RegionCheckModel sends one bounded region-check batch.
func (m *Manager) RegionCheckModel(ctx context.Context, a *model.Agent, payload json.RawMessage) (model.Fields, error) {
	return m.FeatureRequestModel(ctx, a, RemoteRequest{Method: http.MethodPost, Path: "/api/region-check", Timeout: Seconds(120)}, msgRegionCheck, payload)
}

// ReturnRoutingStatusModel reads the registered Agent's return-route state.
func (m *Manager) ReturnRoutingStatusModel(ctx context.Context, a *model.Agent) (model.Fields, error) {
	return m.FeatureRequestModel(ctx, a, RemoteRequest{Method: http.MethodGet, Path: "/api/return-routing", Timeout: Seconds(15)}, msgReturnRoutingStatus, nil)
}

// ReturnRoutingApplyModel applies a native return-route specification.
func (m *Manager) ReturnRoutingApplyModel(ctx context.Context, a *model.Agent, spec json.RawMessage) (model.Fields, error) {
	return m.FeatureRequestModel(ctx, a, RemoteRequest{Method: http.MethodPut, Path: "/api/return-routing", Timeout: Seconds(60)}, msgReturnRoutingManage, spec)
}

// ReturnRoutingRemoveModel removes return routes, optionally restoring old rules.
func (m *Manager) ReturnRoutingRemoveModel(ctx context.Context, a *model.Agent, restore bool) (model.Fields, error) {
	path := "/api/return-routing"
	if restore {
		path += "?restore_legacy=1"
	}
	return m.FeatureRequestModel(ctx, a, RemoteRequest{Method: http.MethodDelete, Path: path, Timeout: Seconds(60)}, msgReturnRoutingManage, nil)
}
