package agents

// Agent operation, update and address routes of backend/routes/agents.py
// (lines 1034–1232 and 2171–2185, A15): agent_address, restart_agent,
// get_agent_status, get_agent_logs, get_agent_log_sources, clear_agent_log,
// validate_log_path, get/set_logging_config, uninstall_agent,
// update_agent_version, get_agent_upgrade, report_agent_upgrade,
// get_latest_agent_version.

import (
	"encoding/json"
	"errors"

	"configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonresp"
	"configflow/internal/model"
	"configflow/internal/redact"
	"fmt"
	"net/http"
)

type upgradesHandler struct {
	deps Deps
}

func (h *upgradesHandler) register(r *httpx.Router) {
	r.Handle([]string{"GET", "PUT"}, "/api/agents/<agent_id>/address", "agents.agent_address", auth.Require(upgradeStateErrors(h.agentAddress)))
	r.POST("/api/agents/<agent_id>/restart", "agents.restart_agent", auth.Require(h.operation(restartAgent)))
	r.GET("/api/agents/<agent_id>/status", "agents.get_agent_status", auth.Require(h.getAgentStatus))
	r.GET("/api/agents/<agent_id>/logs", "agents.get_agent_logs", auth.Require(h.operation(h.getAgentLogs)))
	r.GET("/api/agents/<agent_id>/logs/sources", "agents.get_agent_log_sources", auth.Require(h.operation(getAgentLogSources)))
	r.POST("/api/agents/<agent_id>/logs/clear", "agents.clear_agent_log", auth.Require(h.operation(clearAgentLog)))
	r.POST("/api/agents/<agent_id>/logs/validate", "agents.validate_log_path", auth.Require(h.operation(validateLogPath)))
	r.GET("/api/agents/<agent_id>/config/logging", "agents.get_logging_config", auth.Require(h.operation(getLoggingConfig)))
	r.POST("/api/agents/<agent_id>/config/logging", "agents.set_logging_config", auth.Require(h.operation(setLoggingConfig)))
	r.POST("/api/agents/<agent_id>/uninstall", "agents.uninstall_agent", auth.Require(h.operation(uninstallAgent)))
	r.POST("/api/agents/<agent_id>/update", "agents.update_agent_version", auth.Require(upgradeStateErrors(h.updateAgentVersion)))
	r.GET("/api/agents/<agent_id>/upgrade", "agents.get_agent_upgrade", upgradeStateErrors(h.getAgentUpgrade))
	r.POST("/api/agents/<agent_id>/upgrade/report", "agents.report_agent_upgrade", upgradeStateErrors(h.reportAgentUpgrade))
	r.GET("/api/agents/latest-version", "agents.get_latest_agent_version", auth.Require(h.getLatestAgentVersion))
}

// upgradeStateErrors surfaces damaged stored evidence without changing its 500 class.
func upgradeStateErrors(handler httpx.HandlerFunc) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		response, err := handler(c)
		var corrupt *upgrades.CorruptStateError
		if errors.As(err, &corrupt) {
			return failure(500, corrupt.Error()), nil
		}
		return response, err
	}
}

// operation is the `try: ... except Exception as e: return jsonify({'success':
// False, 'message': str(e)}), 500` wrapper of the operation routes.
func (h *upgradesHandler) operation(handler func(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error)) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		manager, err := h.deps.manager(c.Context())
		var response *httpx.Response
		if err == nil {
			response, err = handler(c, manager)
		}
		if err != nil {
			if errors.Is(err, agents.ErrAgentNotFound) {
				return failure(404, err.Error()), nil
			}
			if response, ok := deviceResponseFailure(err); ok {
				return response, nil
			}
			return failure(500, err.Error()), nil
		}
		return response, nil
	}
}

// resultResponse retains the remote envelope and its existing success status.
func resultResponse(result json.RawMessage) (*httpx.Response, error) {
	fields, err := jsondecode.Select(result, "success")
	if err != nil {
		return nil, fmt.Errorf("invalid Agent operation response: %w", err)
	}
	status := 500
	if success, err := model.Truthy(fields["success"]); err == nil && success {
		status = 200
	}
	return httpx.JSONStatus(status, result), nil
}
func requestData(c *httpx.Ctx) (model.Fields, error) {
	var raw json.RawMessage
	var fields model.Fields
	if err := c.Bind(&raw); err != nil {
		return fields, err
	}
	nonempty, err := model.Truthy(raw)
	if err != nil {
		return fields, err
	}
	if !nonempty {
		return fields, nil
	}
	err = jsondecode.Unmarshal(raw, &fields)
	return fields, err
}
func requestMember(fields model.Fields, name string, fallback json.RawMessage) json.RawMessage {
	if raw, present := fields.Get(name); present {
		return raw
	}
	return fallback
}
func hasJSONContent(raw json.RawMessage) bool {
	present, err := model.Truthy(raw)
	return err == nil && present
}
func deviceJSON(payload any) (http.Header, []byte, error) {
	body, err := jsonresp.Encode(payload)
	if err != nil {
		return nil, nil, err
	}
	return http.Header{"Content-Type": {"application/json"}}, body, nil
}
func callOperation(c *httpx.Ctx, m *agents.Manager, method, path string, payload any, sources bool) (json.RawMessage, error) {
	var header http.Header
	var body []byte
	var err error
	if payload != nil {
		header, body, err = deviceJSON(payload)
		if err != nil {
			return nil, err
		}
	}
	result, agent, err := m.RemoteCall(c.Context(), c.Param("agent_id"), agents.RemoteRequest{Method: method, Path: path, Timeout: agents.Seconds(5), Header: header, Body: body, LegacyLogSources: sources})
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, agents.ErrAgentNotFound
	}
	return result, nil
}

func (h *upgradesHandler) agentAddress(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := h.deps.manager(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := manager.ReadAgent(ctx, c.Param("agent_id"), true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return failure(404, "Agent not found"), nil
	}
	if c.Method() == "GET" {
		data, err := upgrades.DescribeModel(ctx, manager, agent)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(struct {
			Success bool                         `json:"success"`
			Data    *upgrades.AddressDescription `json:"data"`
		}{true, data}), nil
	}
	var body struct {
		Host string `json:"host"`
	}
	c.BindSilent(&body)
	result, status, err := upgrades.ChangeModel(ctx, manager, agent, body.Host)
	if err != nil {
		return nil, err
	}
	return httpx.JSONStatus(status, result), nil
}

func restartAgent(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	result, err := m.RunServiceOperation(c.Context(), c.Param("agent_id"), agents.RestartService, nil, nil)
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func (h *upgradesHandler) getAgentStatus(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := h.deps.manager(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := manager.ReadAgent(ctx, c.Param("agent_id"), true)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return failure(404, "Agent not found"), nil
	}
	// agent.get('status', 'unknown'): only a missing member defaults; a stored
	// null stays null.
	status, err := json.Marshal(agent.Status)
	if err != nil {
		return nil, err
	}
	if agent.Status == "" {
		status = []byte(`"unknown"`)
	}
	return httpx.JSON(map[string]any{"success": true, "status": json.RawMessage(status)}), nil
}

func (h *upgradesHandler) getAgentLogs(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	ctx := h.deps.withRepo(c.Context())
	lines := argBigInt(c, "lines", 100)
	logPath := c.ArgDefault("log_path", "")
	source := c.ArgDefault("source", "")
	result, err := callOperation(c, m, http.MethodGet, agents.AgentLogsPath(lines.String(), logPath, source), nil, false)
	if err != nil {
		return nil, err
	}
	var fields map[string]json.RawMessage
	if err := jsondecode.Unmarshal(result, &fields); err != nil || fields == nil {
		return nil, fmt.Errorf("invalid Agent log response")
	}
	for _, key := range []string{"logs", "error_logs"} {
		raw, present := fields[key]
		if !present || len(raw) == 0 || raw[0] != '"' {
			continue
		}
		var text string
		if jsondecode.Unmarshal(raw, &text) != nil || text == "" {
			continue
		}
		redacted, err := redact.RedactSecretsInText(ctx, text)
		if err != nil {
			return nil, err
		}
		fields[key], err = json.Marshal(redacted)
		if err != nil {
			return nil, err
		}
	}
	result, err = json.Marshal(fields)
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func getAgentLogSources(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	result, err := callOperation(c, m, http.MethodGet, "/api/logs/sources", nil, true)
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func clearAgentLog(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	data, err := requestData(c)
	if err != nil {
		return nil, err
	}
	logPath := requestMember(data, "log_path", json.RawMessage(`""`))
	source := requestMember(data, "source", json.RawMessage(`""`))
	if !hasJSONContent(logPath) && !hasJSONContent(source) {
		return failure(400, "日志路径不能为空"), nil
	}
	payload := map[string]json.RawMessage{"log_path": logPath}
	if hasJSONContent(source) {
		payload = map[string]json.RawMessage{"source": source}
	}
	result, err := callOperation(c, m, http.MethodPost, "/api/logs/clear", payload, false)
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func validateLogPath(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	data, err := requestData(c)
	if err != nil {
		return nil, err
	}
	logPath := requestMember(data, "path", json.RawMessage(`""`))
	if !hasJSONContent(logPath) {
		return failure(400, "日志路径不能为空"), nil
	}
	result, err := callOperation(c, m, http.MethodPost, "/api/logs/validate", map[string]json.RawMessage{"path": logPath}, false)
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func getLoggingConfig(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	result, err := callOperation(c, m, http.MethodGet, "/api/config/logging", nil, false)
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func setLoggingConfig(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	data, err := requestData(c)
	if err != nil {
		return nil, m.RemotePayloadError(c.Context(), c.Param("agent_id"), err)
	}
	enabled := requestMember(data, "enabled", json.RawMessage("true"))
	header, body, err := deviceJSON(map[string]json.RawMessage{"enabled": enabled})
	if err != nil {
		return nil, err
	}
	result, err := m.SetLoggingModel(c.Context(), c.Param("agent_id"), enabled, header, body)
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func uninstallAgent(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	ctx := c.Context()
	agentID := c.Param("agent_id")
	result, err := m.RunServiceOperation(ctx, agentID, agents.UninstallService, nil, nil)
	if err != nil {
		return nil, err
	}
	fields, decodeErr := jsondecode.Select(result, "success")
	if decodeErr != nil {
		return nil, decodeErr
	}
	if string(fields["success"]) == "true" {
		// 卸载成功后从数据库删除
		if _, err := m.DeleteAgent(ctx, agentID); err != nil {
			return nil, err
		}
	}
	return resultResponse(result)
}

func (h *upgradesHandler) updateAgentVersion(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := h.deps.manager(ctx)
	if err != nil {
		return nil, err
	}
	result, err := upgrades.StartModel(ctx, manager, c.Param("agent_id"))
	if err != nil {
		if upgrades.IsRefused(err) {
			return failure(409, agents.UpgradeErrorMessage(err.Error())), nil
		}
		return nil, err
	}
	status := 202
	if result.Status == "failed" {
		status = 409
	}
	public, err := agents.PublicUpgradeStateModel(result)
	if err != nil {
		return nil, err
	}
	return httpx.JSONStatus(status, public), nil
}

// agentTokenMatches is `agent and provided and
// _constant_time_ascii_equal(provided, agent.get('token', ”))`.
func agentTokenMatches(c *httpx.Ctx, agent *model.Agent) bool {
	if agent == nil {
		return false
	}
	header, present := c.Header("Authorization")
	provided, ok := auth.ParseBearerToken(header, present)
	token := agent.Token
	return token != "" && ok && provided != "" && agents.ConstantTimeASCIIEqual(provided, token)
}

func (h *upgradesHandler) getAgentUpgrade(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := h.deps.manager(ctx)
	if err != nil {
		return nil, err
	}
	agentID := c.Param("agent_id")
	agent, err := manager.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if agentTokenMatches(c, agent) {
		// Legacy bootstrap reads its own pending plan; no recursive Agent call.
		if _, err := upgrades.StoredState(agent); err != nil {
			return nil, err
		}
		state, present, err := model.EncodeMember(agent, "latest_upgrade")
		if err != nil {
			return nil, err
		}
		if !present || string(state) == "null" {
			state = json.RawMessage(`{}`)
		}
		return httpx.JSON(json.RawMessage(state)), nil
	}
	return auth.Require(func(c *httpx.Ctx) (*httpx.Response, error) {
		state, err := upgrades.PollModel(ctx, manager, agentID)
		if err != nil {
			if upgrades.IsRefused(err) {
				return failure(404, agents.UpgradeErrorMessage(err.Error())), nil
			}
			return nil, err
		}
		public, err := agents.PublicUpgradeStateModel(state)
		if err != nil {
			return nil, err
		}
		return httpx.JSON(public), nil
	})(c)
}

// ReportBodyLimit is the largest legacy upgrade report accepted (bytes).
const ReportBodyLimit = 4096

func (h *upgradesHandler) reportAgentUpgrade(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := h.deps.manager(ctx)
	if err != nil {
		return nil, err
	}
	agentID := c.Param("agent_id")
	agent, err := manager.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if !agentTokenMatches(c, agent) {
		return httpx.JSONStatus(401, map[string]bool{"success": false}), nil
	}
	if length, ok := c.ContentLength(); !ok || length > ReportBodyLimit {
		return httpx.JSONStatus(413, map[string]bool{"success": false}), nil
	}
	var payload json.RawMessage
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	state, err := upgrades.ReportLegacyJSON(ctx, manager, agentID, payload)
	if err != nil {
		if errors.Is(err, upgrades.ErrInvalidReport) || upgrades.IsRefused(err) {
			return failure(400, "Agent 上报的更新结果格式无效。"), nil
		}
		return nil, err
	}
	return httpx.JSON(state), nil
}

func (h *upgradesHandler) getLatestAgentVersion(c *httpx.Ctx) (*httpx.Response, error) {
	return httpx.JSON(map[string]any{"success": true, "version": agents.GetLatestVersion()}), nil
}
