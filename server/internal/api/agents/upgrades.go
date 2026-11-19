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
	"strings"

	"configflow/internal/agents"
	"configflow/internal/agents/upgrades"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/redact"
)

func registerUpgrades(r *httpx.Router) {
	r.Handle([]string{"GET", "PUT"}, "/api/agents/<agent_id>/address", "agents.agent_address", auth.Require(upgradeStateErrors(agentAddress)))
	r.POST("/api/agents/<agent_id>/restart", "agents.restart_agent", auth.Require(operation(restartAgent)))
	r.GET("/api/agents/<agent_id>/status", "agents.get_agent_status", auth.Require(getAgentStatus))
	r.GET("/api/agents/<agent_id>/logs", "agents.get_agent_logs", auth.Require(operation(getAgentLogs)))
	r.GET("/api/agents/<agent_id>/logs/sources", "agents.get_agent_log_sources", auth.Require(operation(getAgentLogSources)))
	r.POST("/api/agents/<agent_id>/logs/clear", "agents.clear_agent_log", auth.Require(operation(clearAgentLog)))
	r.POST("/api/agents/<agent_id>/logs/validate", "agents.validate_log_path", auth.Require(operation(validateLogPath)))
	r.GET("/api/agents/<agent_id>/config/logging", "agents.get_logging_config", auth.Require(operation(getLoggingConfig)))
	r.POST("/api/agents/<agent_id>/config/logging", "agents.set_logging_config", auth.Require(operation(setLoggingConfig)))
	r.POST("/api/agents/<agent_id>/uninstall", "agents.uninstall_agent", auth.Require(operation(uninstallAgent)))
	r.POST("/api/agents/<agent_id>/update", "agents.update_agent_version", auth.Require(upgradeStateErrors(updateAgentVersion)))
	r.GET("/api/agents/<agent_id>/upgrade", "agents.get_agent_upgrade", upgradeStateErrors(getAgentUpgrade))
	r.POST("/api/agents/<agent_id>/upgrade/report", "agents.report_agent_upgrade", upgradeStateErrors(reportAgentUpgrade))
	r.GET("/api/agents/latest-version", "agents.get_latest_agent_version", auth.Require(getLatestAgentVersion))
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
func operation(handler func(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error)) httpx.HandlerFunc {
	return func(c *httpx.Ctx) (*httpx.Response, error) {
		manager, err := agents.Get(c.Context())
		var response *httpx.Response
		if err == nil {
			response, err = handler(c, manager)
		}
		if err != nil {
			if response, ok := deviceResponseFailure(err); ok {
				return response, nil
			}
			return failure(500, err.Error()), nil
		}
		return response, nil
	}
}

// pyRouteError is a Python exception raised inside an operation route; its
// text is str(e).
type pyRouteError struct{ message string }

func (e *pyRouteError) Error() string { return e.message }

func pyTypeError(message string) error { return &pyRouteError{message} }

// resultResponse is `jsonify(result), 200 if result.get('success') else 500`.
func resultResponse(result any) (*httpx.Response, error) {
	object, ok := result.(*jsonx.Object)
	if !ok || object == nil {
		return nil, pyTypeError(httpx.NoAttributeGet(result))
	}
	status := 500
	if jsonx.Truthy(object.Value("success")) {
		status = 200
	}
	return httpx.JSONStatus(status, result), nil
}

// requestData is `request.get_json() or {}` followed by data.get(...): the
// JSON errors (415/400) and the AttributeError of a truthy non-dict become
// the route's 500 message.
func requestData(c *httpx.Ctx) (*jsonx.Object, error) {
	value, err := c.JSON()
	if err != nil {
		return nil, pyTypeError(err.Error())
	}
	if !jsonx.Truthy(value) {
		return jsonx.NewObject(), nil
	}
	object, ok := value.(*jsonx.Object)
	if !ok {
		return nil, pyTypeError(httpx.NoAttributeGet(value))
	}
	return object, nil
}

func agentAddress(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
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
	result, err := m.RestartAgentService(c.Context(), c.Param("agent_id"))
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func getAgentStatus(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := manager.GetAgentByID(ctx, c.Param("agent_id"), true)
	if err != nil {
		return nil, err
	}
	if agent == nil || agent.Len() == 0 {
		return failure(404, "Agent not found"), nil
	}
	return httpx.JSON(jsonx.Obj("success", true, "status", agent.GetOr("status", "unknown"))), nil
}

// contains is `key in result` for any JSON value.
func contains(result any, key string) (bool, error) {
	switch v := result.(type) {
	case *jsonx.Object:
		if v == nil {
			break
		}
		return v.Has(key), nil
	case []any:
		for _, item := range v {
			if jsonx.Equal(item, key) {
				return true, nil
			}
		}
		return false, nil
	case string:
		return strings.Contains(v, key), nil
	}
	return false, pyTypeError("argument of type '" + httpx.PyTypeName(result) + "' is not iterable")
}

func getAgentLogs(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	ctx := c.Context()
	lines := argBigInt(c, "lines", 100)
	logPath := c.ArgDefault("log_path", "")
	source := c.ArgDefault("source", "")
	result, err := m.GetAgentLogs(ctx, c.Param("agent_id"), jsonx.BigInt(lines), logPath, source)
	if err != nil {
		return nil, err
	}
	for _, key := range []string{"logs", "error_logs"} {
		found, err := contains(result, key)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		object, ok := result.(*jsonx.Object)
		if !ok {
			if _, isList := result.([]any); isList {
				return nil, pyTypeError("list indices must be integers or slices, not str")
			}
			return nil, pyTypeError("string indices must be integers, not 'str'")
		}
		if text, ok := object.Value(key).(string); ok && text != "" {
			redacted, err := redact.RedactSecretsInText(ctx, text)
			if err != nil {
				return nil, err
			}
			object.Set(key, redacted)
		}
	}
	return resultResponse(result)
}

func getAgentLogSources(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	result, err := m.GetAgentLogSources(c.Context(), c.Param("agent_id"))
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
	logPath := data.GetOr("log_path", "")
	source := data.GetOr("source", "")
	if !jsonx.Truthy(logPath) && !jsonx.Truthy(source) {
		return failure(400, "日志路径不能为空"), nil
	}
	result, err := m.ClearAgentLog(c.Context(), c.Param("agent_id"), logPath, source)
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
	logPath := data.GetOr("path", "")
	if !jsonx.Truthy(logPath) {
		return failure(400, "日志路径不能为空"), nil
	}
	result, err := m.ValidateAgentLogPath(c.Context(), c.Param("agent_id"), logPath)
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func getLoggingConfig(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	result, err := m.GetLoggingConfig(c.Context(), c.Param("agent_id"))
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func setLoggingConfig(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	data, err := requestData(c)
	if err != nil {
		return nil, err
	}
	result, err := m.SetLoggingConfig(c.Context(), c.Param("agent_id"), data.GetOr("enabled", true))
	if err != nil {
		return nil, err
	}
	return resultResponse(result)
}

func uninstallAgent(c *httpx.Ctx, m *agents.Manager) (*httpx.Response, error) {
	ctx := c.Context()
	agentID := c.Param("agent_id")
	result, err := m.UninstallAgent(ctx, agentID)
	if err != nil {
		return nil, err
	}
	if jsonx.Truthy(result.Value("success")) {
		// 卸载成功后从数据库删除
		if _, err := m.DeleteAgent(ctx, agentID); err != nil {
			return nil, err
		}
	}
	return resultResponse(result)
}

func updateAgentVersion(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	result, err := upgrades.StartModel(ctx, manager, c.Param("agent_id"))
	if err != nil {
		if upgrades.IsValueError(err) {
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
	raw, present, err := agent.MemberJSON("token")
	var token string
	valid := err == nil && present && len(raw) > 0 && raw[0] == '"' && jsondecode.Unmarshal(raw, &token) == nil
	return valid && ok && provided != "" && agents.ConstantTimeASCIIEqual(provided, token)
}

func getAgentUpgrade(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
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
		state, present, err := agent.MemberJSON("latest_upgrade")
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
			if upgrades.IsValueError(err) {
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

func reportAgentUpgrade(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agentID := c.Param("agent_id")
	agent, err := manager.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if !agentTokenMatches(c, agent) {
		return httpx.JSONStatus(401, jsonx.Obj("success", false)), nil
	}
	if length, ok := c.ContentLength(); !ok || length > ReportBodyLimit {
		return httpx.JSONStatus(413, jsonx.Obj("success", false)), nil
	}
	var payload json.RawMessage
	if err := c.Bind(&payload); err != nil {
		return nil, err
	}
	state, err := upgrades.ReportLegacyJSON(ctx, manager, agentID, payload)
	if err != nil {
		if errors.Is(err, upgrades.ErrInvalidReport) || upgrades.IsValueError(err) {
			return failure(400, "Agent 上报的更新结果格式无效。"), nil
		}
		return nil, err
	}
	return httpx.JSON(state), nil
}

func getLatestAgentVersion(c *httpx.Ctx) (*httpx.Response, error) {
	return httpx.JSON(jsonx.Obj("success", true, "version", agents.GetLatestVersion())), nil
}
