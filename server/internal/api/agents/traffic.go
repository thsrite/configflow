package agents

// Traffic routes of backend/routes/agents.py (A05): lines 728–750
// (agent_traffic_report), 781–884 (traffic history: Agent report, query,
// switch, clear, retention) and 2100–2170 (traffic stats and trend of the
// monitoring history).

import (
	"errors"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/auth"
	"configflow/internal/discovery"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
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
	report, errorResponse := ReadJSONBody(c, trafficstore.ReportMaxContentLength, "Invalid traffic report")
	if errorResponse != nil {
		return errorResponse, nil
	}
	if !mihomoFlag(agent, "domain_discovery_enabled") {
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
	if value := agent.Value("profile_id"); jsonx.Truthy(value) {
		profileID = jsonx.Str(value)
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
	report, errorResponse := ReadJSONBody(c, metrics.TrafficReportMaxContentLength, "Invalid traffic history report")
	if errorResponse != nil {
		return errorResponse, nil
	}
	if !mihomoFlag(agent, "traffic_history_enabled") {
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
func trafficHistoryState(c *httpx.Ctx, manager *agents.Manager, agent *jsonx.Object) (*jsonx.Object, error) {
	enabled := mihomoFlag(agent, "traffic_history_enabled")
	var since any
	if enabled {
		since = agent.Value("traffic_history_since")
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
	version := agent.Value("version")
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
func mihomoAgentOrError(c *httpx.Ctx, manager *agents.Manager) (*jsonx.Object, *httpx.Response, error) {
	agent, err := manager.GetAgentByID(c.Context(), c.Param("agent_id"), true)
	if err != nil {
		return nil, nil, err
	}
	if agent == nil {
		return nil, failure(404, "Agent not found"), nil
	}
	if !jsonx.Equal(agent.GetOr("service_type", "mihomo"), "mihomo") {
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
	agent, err := manager.SetTrafficHistory(c.Context(), c.Param("agent_id"), enabled)
	if err != nil {
		var valueError *agents.ValueError
		var conflict *agents.DeploymentConflict
		switch {
		case errors.As(err, &valueError):
			return failure(400, valueError.Message), nil
		case errors.As(err, &conflict):
			return failure(409, conflict.Message), nil
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
