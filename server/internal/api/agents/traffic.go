package agents

// Traffic routes of backend/routes/agents.py (A05): lines 728–750
// (agent_traffic_report), 781–884 (traffic history: Agent report, query,
// switch, clear, retention) and 2100–2170 (traffic stats and trend of the
// monitoring history).

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/auth"
	"configflow/internal/discovery"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/model"
	"configflow/internal/repo"
)

func registerTraffic(r *httpx.Router) {
	// Agent-authenticated (Bearer token of the Agent); listed as public in
	// internal/auth like routes/auth.py.
	r.POST("/api/agents/<agent_id>/traffic-report", "agents.agent_traffic_report", agentTrafficReport)
	r.POST("/api/agents/<agent_id>/traffic-history", "agents.agent_traffic_history_report", agentTrafficHistoryReport)
	r.GET("/api/agents/<agent_id>/traffic-history", "agents.get_agent_traffic_history", auth.Require(getAgentTrafficHistory))
	r.PUT("/api/agents/<agent_id>/traffic-history", "agents.set_agent_traffic_history", auth.Require(setAgentTrafficHistory))
	r.DELETE("/api/agents/<agent_id>/traffic-history", "agents.clear_agent_traffic_history", auth.Require(clearAgentTrafficHistory))
	r.PUT("/api/agents/traffic-history/retention", "agents.set_traffic_history_retention", auth.Require(setTrafficHistoryRetention))
	r.GET("/api/agents/<agent_id>/traffic/stats", "agents.get_agent_traffic_stats", auth.Require(getAgentTrafficStats))
	r.GET("/api/agents/<agent_id>/traffic/trend", "agents.get_agent_traffic_trend", auth.Require(getAgentTrafficTrend))
}

// agentTrafficReport is agent_traffic_report: an Agent reports its Mihomo
// traffic summary (domain discovery). Errors are not caught (500 page).
func agentTrafficReport(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	agentID := c.Param("agent_id")
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := authenticatedAgent(c, manager, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return failure(401, "Unauthorized"), nil
	}
	report, errorResponse := readLegacyTrafficBody(c, trafficstore.ReportMaxContentLength, "Invalid traffic report")
	if errorResponse != nil {
		return errorResponse, nil
	}
	if !heartbeatFeature(agent, "domain_discovery_enabled") {
		return httpx.JSONStatus(409, jsonx.Obj("success", false, "enabled", false, "message", "Domain discovery disabled")), nil
	}
	cleaned, err := trafficstore.CleanReport(report)
	if err != nil {
		return nil, err
	}
	if cleaned == nil {
		return failure(400, "Invalid traffic report"), nil
	}
	store, err := trafficstore.Get(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := store.AddReport(agentID, cleaned, nil); err != nil {
		return nil, err
	}
	profileID := ""
	if agent.ProfileID != "" {
		profileID = agent.ProfileID
	} else if profileID, err = repo.GetDefaultProfileID(ctx); err != nil {
		return nil, err
	}
	revisions, err := discovery.ProviderRevisions(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true, "enabled", true, "rule_providers", revisions)), nil
}

// agentTrafficHistoryReport is agent_traffic_history_report: an Agent
// reports hourly traffic increments (流量记录).
func agentTrafficHistoryReport(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	agentID := c.Param("agent_id")
	manager, err := agents.Get(ctx)
	if err != nil {
		return nil, err
	}
	agent, err := authenticatedAgent(c, manager, agentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return failure(401, "Unauthorized"), nil
	}
	report, errorResponse := readLegacyTrafficBody(c, metrics.TrafficReportMaxContentLength, "Invalid traffic history report")
	if errorResponse != nil {
		return errorResponse, nil
	}
	if !heartbeatFeature(agent, "traffic_history_enabled") {
		return httpx.JSONStatus(409, jsonx.Obj("success", false, "enabled", false, "message", "Traffic history disabled")), nil
	}
	cleaned := metrics.CleanTrafficReport(report, nil)
	if cleaned == nil {
		return failure(400, "Invalid traffic history report"), nil
	}
	metrics.TrafficForManager(manager).AddReport(agentID, cleaned)
	return httpx.JSON(jsonx.Obj("success", true)), nil
}

// trafficHistoryState is _traffic_history_state(agent).
func trafficHistoryState(c *httpx.Ctx, manager *agents.Manager, agent *model.Agent) (*jsonx.Object, error) {
	enabled := heartbeatFeature(agent, "traffic_history_enabled")
	var since any
	if enabled {
		raw, _, _ := agent.MemberJSON("traffic_history_since")
		if len(raw) > 0 {
			since, _ = jsonx.Parse(raw)
		}
	}
	system, err := manager.Repository().GetSystem(c.Context())
	if err != nil {
		return nil, err
	}
	retention, err := metrics.RetentionDays(system)
	if err != nil {
		return nil, err
	}
	choices := make([]any, len(metrics.RetentionChoices))
	for i, choice := range metrics.RetentionChoices {
		choices[i] = choice
	}
	version := agent.Version
	return jsonx.Obj(
		"enabled", enabled,
		"since", since,
		"supported", metrics.AgentSupported(version, metrics.MinTrafficAgentVersion),
		"min_version", metrics.MinTrafficAgentVersion,
		"full_chain", metrics.AgentSupported(version, metrics.FullChainAgentVersion),
		"full_chain_version", metrics.FullChainAgentVersion,
		"retention_days", retention,
		"retention_choices", choices,
	), nil
}

// mihomoAgentOrError is _mihomo_agent_or_error(agent_id).
func mihomoAgentOrError(c *httpx.Ctx, manager *agents.Manager) (*model.Agent, *httpx.Response, error) {
	agent, err := manager.ReadAgent(c.Context(), c.Param("agent_id"), true)
	if err != nil {
		return nil, nil, err
	}
	if agent == nil {
		return nil, failure(404, "Agent not found"), nil
	}
	if agent.ServiceType != "mihomo" {
		return nil, failure(400, "流量记录只支持 mihomo Agent"), nil
	}
	return agent, nil, nil
}

func inList(value string, choices []string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}

// getAgentTrafficHistory is get_agent_traffic_history: the selected range
// grouped by device, target, chain, exit node or policy group, with the
// trend.
func getAgentTrafficHistory(c *httpx.Ctx) (*httpx.Response, error) {
	manager, err := agents.Get(c.Context())
	if err != nil {
		return nil, err
	}
	agent, response, err := mihomoAgentOrError(c, manager)
	if response != nil || err != nil {
		return response, err
	}
	rangeKey, by := c.ArgDefault("range", "24h"), c.ArgDefault("by", "source")
	if _, ok := metrics.Ranges[rangeKey]; !ok || !inList(by, metrics.Aggregates) {
		return failure(400, "range 或 by 参数无效"), nil
	}
	data, err := trafficHistoryState(c, manager, agent)
	if err != nil {
		return nil, err
	}
	result, err := metrics.TrafficForManager(manager).Query(c.Param("agent_id"), rangeKey, by, nil)
	if err != nil {
		return nil, err
	}
	data.Update(result)
	return httpx.JSON(jsonx.Obj("success", true, "data", data)), nil
}

// setAgentTrafficHistory is set_agent_traffic_history: switch the traffic
// history; switching off with clear: true also deletes what was recorded.
func setAgentTrafficHistory(c *httpx.Ctx) (*httpx.Response, error) {
	payload, _ := c.JSONSilent().(*jsonx.Object)
	if payload == nil {
		return failure(400, "enabled 必须是布尔值"), nil
	}
	enabled, ok := payload.Value("enabled").(bool)
	clear, clearOK := payload.GetOr("clear", false).(bool)
	if !ok || !clearOK {
		return failure(400, "enabled 必须是布尔值"), nil
	}
	manager, err := agents.Get(c.Context())
	if err != nil {
		return nil, err
	}
	if _, response, err := mihomoAgentOrError(c, manager); response != nil || err != nil {
		return response, err
	}
	agent, err := manager.SetTrafficHistoryModel(c.Context(), c.Param("agent_id"), enabled)
	if err != nil {
		if response := setTrafficHistoryError(err); response != nil {
			return response, nil
		}
		return nil, err
	}
	if agent == nil {
		return failure(404, "Agent not found"), nil
	}
	if !enabled && clear {
		if err := metrics.TrafficForManager(manager).Delete(c.Param("agent_id")); err != nil {
			return nil, err
		}
	}
	state, err := trafficHistoryState(c, manager, agent)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true, "data", state)), nil
}

// setTrafficHistoryError is the except chain of set_agent_traffic_history:
// `except ValueError` comes first, and AgentDeploymentConflict is a
// ValueError, so a conflict (ProfileInUse of the system transaction) answers
// 400 like every other ValueError — the `except AgentDeploymentConflict` 409
// branch after it is never reached. Repository validation errors are
// ValueErrors too; nil leaves the error to the 500 page.
func setTrafficHistoryError(err error) *httpx.Response {
	var valueError *agents.ValueError
	var conflict *agents.DeploymentConflict
	switch {
	case errors.As(err, &valueError):
		return failure(400, valueError.Message)
	case errors.As(err, &conflict):
		return failure(400, conflict.Message)
	case repo.IsValueError(err):
		return failure(400, err.Error())
	}
	return nil
}

// clearAgentTrafficHistory is clear_agent_traffic_history: delete the
// recorded traffic without changing the switch.
func clearAgentTrafficHistory(c *httpx.Ctx) (*httpx.Response, error) {
	manager, err := agents.Get(c.Context())
	if err != nil {
		return nil, err
	}
	if _, response, err := mihomoAgentOrError(c, manager); response != nil || err != nil {
		return response, err
	}
	if err := metrics.TrafficForManager(manager).Delete(c.Param("agent_id")); err != nil {
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true)), nil
}

// setTrafficHistoryRetention is set_traffic_history_retention: the
// retention shared by every Agent.
func setTrafficHistoryRetention(c *httpx.Ctx) (*httpx.Response, error) {
	var days any
	if payload, ok := c.JSONSilent().(*jsonx.Object); ok && payload != nil {
		days = payload.Value("days")
	}
	manager, err := agents.Get(c.Context())
	if err != nil {
		return nil, err
	}
	stored, err := metrics.SetRetentionDays(c.Context(), manager, days)
	if err != nil {
		var valueError *metrics.ValueError
		if errors.As(err, &valueError) {
			return failure(400, valueError.Message), nil
		}
		if repo.IsValueError(err) {
			// `except ValueError` also catches the repository's
			// ProfileValidationError / ProfileInUse / ProfileExists.
			return failure(400, err.Error()), nil
		}
		return nil, err
	}
	return httpx.JSON(jsonx.Obj("success", true, "data", jsonx.Obj("retention_days", stored))), nil
}

// getAgentTrafficStats is get_agent_traffic_stats (period, default total).
func getAgentTrafficStats(c *httpx.Ctx) (*httpx.Response, error) {
	const prefix = "获取Agent流量统计失败"
	manager, _, response := agentForMetrics(c, prefix)
	if response != nil {
		return response, nil
	}
	period := c.ArgDefault("period", "total")
	stats, err := metrics.ForManager(manager).GetTrafficStats(c.Param("agent_id"), period)
	if err != nil {
		return metricsFailure(prefix, err), nil
	}
	return httpx.JSON(jsonx.Obj("success", true, "data", jsonx.Obj("agent_id", c.Param("agent_id"), "stats", stats))), nil
}

// getAgentTrafficTrend is get_agent_traffic_trend (hours default 24,
// interval default 5 minutes).
func getAgentTrafficTrend(c *httpx.Ctx) (*httpx.Response, error) {
	const prefix = "获取Agent流量趋势失败"
	manager, _, response := agentForMetrics(c, prefix)
	if response != nil {
		return response, nil
	}
	hours := argBigInt(c, "hours", 24)
	interval := argBigInt(c, "interval", 5)
	trend, err := metrics.ForManager(manager).GetTrafficTrend(c.Param("agent_id"), hours, interval)
	if err != nil {
		return metricsFailure(prefix, err), nil
	}
	return httpx.JSON(jsonx.Obj("success", true, "data", jsonx.Obj(
		"agent_id", c.Param("agent_id"), "trend", trend, "hours", jsonx.BigInt(hours),
		"interval_minutes", jsonx.BigInt(interval), "data_points", len(trend)))), nil
}

// readLegacyTrafficBody is retained for the pending A05 native engine task.
func readLegacyTrafficBody(c *httpx.Ctx, maxLength int64, invalidMessage string) (any, *httpx.Response) {
	if length, ok := c.ContentLength(); ok && length > maxLength {
		return nil, failure(413, "Request body too large")
	}
	var raw []byte
	if stream := c.Stream(); stream != nil {
		raw, _ = io.ReadAll(io.LimitReader(stream, maxLength+1))
	}
	if int64(len(raw)) > maxLength {
		return nil, failure(413, "Request body too large")
	}
	if !c.IsJSON() {
		return nil, failure(400, invalidMessage)
	}
	c.Request.Body = io.NopCloser(bytes.NewReader(raw))
	value, err := c.JSONForceError()
	if err != nil {
		// json.loads raises a plain ValueError (not JSONDecodeError) for an
		// int literal beyond sys.get_int_max_str_digits(): not caught here,
		// the route answers it as a 500.
		if strings.HasPrefix(err.Error(), "Exceeds the limit (") {
			return nil, failure(500, err.Error())
		}
		return nil, failure(400, invalidMessage)
	}
	return value, nil
}
