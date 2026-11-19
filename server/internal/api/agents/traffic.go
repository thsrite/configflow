package agents

// Traffic routes of backend/routes/agents.py (A05): lines 728–750
// (agent_traffic_report), 781–884 (traffic history: Agent report, query,
// switch, clear, retention) and 2100–2170 (traffic stats and trend of the
// monitoring history).

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/auth"
	"configflow/internal/discovery"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
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
	report, errorResponse := readTrafficBody(c, trafficstore.ReportMaxContentLength, "Invalid traffic report")
	if errorResponse != nil {
		return errorResponse, nil
	}
	if !heartbeatFeature(agent, "domain_discovery_enabled") {
		return httpx.JSONStatus(409, map[string]any{"success": false, "enabled": false, "message": "Domain discovery disabled"}), nil
	}
	cleaned, err := trafficstore.DecodeReport(report)
	if err != nil {
		return nil, err
	}
	if cleaned == nil {
		return failure(400, "Invalid traffic report"), nil
	}
	profileID, err := manager.ResolveAgentProfile(ctx, agent)
	if err != nil {
		return nil, err
	}
	store, err := trafficstore.GetNative(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := store.Add(ctx, agentID, cleaned, nil); err != nil {
		return nil, err
	}

	revisions, err := discovery.ProviderRevisions(ctx, profileID)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(map[string]any{"success": true, "enabled": true, "rule_providers": revisions}), nil
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
	report, errorResponse := readTrafficBody(c, metrics.TrafficReportMaxContentLength, "Invalid traffic history report")
	if errorResponse != nil {
		return errorResponse, nil
	}
	if !heartbeatFeature(agent, "traffic_history_enabled") {
		return httpx.JSONStatus(409, map[string]any{"success": false, "enabled": false, "message": "Traffic history disabled"}), nil
	}
	cleaned := metrics.DecodeTrafficReport(report, nil)
	if cleaned == nil {
		return failure(400, "Invalid traffic history report"), nil
	}
	if err := metrics.TrafficForManager(manager).AddTraffic(ctx, agentID, cleaned); err != nil {
		return nil, err
	}
	return httpx.JSON(map[string]any{"success": true}), nil
}

// trafficState combines settings with the optional native query result.
type trafficState struct {
	Enabled          bool            `json:"enabled"`
	Since            json.RawMessage `json:"since"`
	Supported        bool            `json:"supported"`
	MinVersion       string          `json:"min_version"`
	FullChain        bool            `json:"full_chain"`
	FullChainVersion string          `json:"full_chain_version"`
	RetentionDays    int64           `json:"retention_days"`
	RetentionChoices []int64         `json:"retention_choices"`
	*metrics.TrafficQuery
}

// trafficHistoryState is _traffic_history_state(agent).
func trafficHistoryState(c *httpx.Ctx, manager *agents.Manager, agent *model.Agent) (*trafficState, error) {
	enabled := heartbeatFeature(agent, "traffic_history_enabled")
	since := json.RawMessage("null")
	if enabled {
		raw, present, err := agent.MemberJSON("traffic_history_since")
		if err != nil {
			return nil, err
		}
		if present {
			since = raw
		}
	}
	system, err := manager.Repository().System(c.Context())
	if err != nil {
		return nil, err
	}
	retention, err := metrics.TrafficRetention(system)
	if err != nil {
		return nil, err
	}
	return &trafficState{Enabled: enabled, Since: since,
		Supported:        metrics.SupportedTrafficVersion(agent.Version, metrics.MinTrafficAgentVersion),
		MinVersion:       metrics.MinTrafficAgentVersion,
		FullChain:        metrics.SupportedTrafficVersion(agent.Version, metrics.FullChainAgentVersion),
		FullChainVersion: metrics.FullChainAgentVersion,
		RetentionDays:    retention, RetentionChoices: metrics.RetentionChoices}, nil
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
	// agent.get('service_type', 'mihomo'): a missing member is mihomo; null or
	// a wrong type (R5 fallback "") is not.
	_, present, err := agent.MemberJSON("service_type")
	if err != nil {
		return nil, nil, err
	}
	if present && agent.ServiceType != "mihomo" {
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
	result, err := metrics.TrafficForManager(manager).QueryTraffic(c.Context(), c.Param("agent_id"), rangeKey, by, nil)
	if err != nil {
		return nil, err
	}
	data.TrafficQuery = result
	return httpx.JSON(map[string]any{"success": true, "data": data}), nil
}

// setAgentTrafficHistory is set_agent_traffic_history: switch the traffic
// history; switching off with clear: true also deletes what was recorded.
func setAgentTrafficHistory(c *httpx.Ctx) (*httpx.Response, error) {
	raw, _ := io.ReadAll(c.Stream())
	payload, decodeErr := jsondecode.Select(raw, "enabled", "clear")
	if !c.IsJSON() || decodeErr != nil {
		return failure(400, "enabled 必须是布尔值"), nil
	}
	enabledRaw, clearRaw := bytes.TrimSpace(payload["enabled"]), bytes.TrimSpace(payload["clear"])
	if !bytes.Equal(enabledRaw, []byte("true")) && !bytes.Equal(enabledRaw, []byte("false")) || len(clearRaw) > 0 && !bytes.Equal(clearRaw, []byte("true")) && !bytes.Equal(clearRaw, []byte("false")) {
		return failure(400, "enabled 必须是布尔值"), nil
	}
	enabled, clear := bytes.Equal(enabledRaw, []byte("true")), bytes.Equal(clearRaw, []byte("true"))
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
		if err := metrics.TrafficForManager(manager).DeleteContext(c.Context(), c.Param("agent_id")); err != nil {
			return nil, err
		}
	}
	state, err := trafficHistoryState(c, manager, agent)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(map[string]any{"success": true, "data": state}), nil
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
	if err := metrics.TrafficForManager(manager).DeleteContext(c.Context(), c.Param("agent_id")); err != nil {
		return nil, err
	}
	return httpx.JSON(map[string]any{"success": true}), nil
}

// setTrafficHistoryRetention is set_traffic_history_retention: the
// retention shared by every Agent.
func setTrafficHistoryRetention(c *httpx.Ctx) (*httpx.Response, error) {
	var days int64
	raw, _ := io.ReadAll(c.Stream())
	payload, decodeErr := jsondecode.Select(raw, "days")
	if !c.IsJSON() || decodeErr != nil || jsondecode.Unmarshal(payload["days"], &days) != nil {
		days = 0
	}
	manager, err := agents.Get(c.Context())
	if err != nil {
		return nil, err
	}
	stored, err := metrics.SetTrafficRetention(c.Context(), manager, days)
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
	return httpx.JSON(map[string]any{"success": true, "data": map[string]any{"retention_days": stored}}), nil
}

// getAgentTrafficStats is get_agent_traffic_stats (period, default total).
func getAgentTrafficStats(c *httpx.Ctx) (*httpx.Response, error) {
	const prefix = "获取Agent流量统计失败"
	manager, _, response := agentForMetrics(c, prefix)
	if response != nil {
		return response, nil
	}
	period := c.ArgDefault("period", "total")
	stats, err := metrics.ForManager(manager).TrafficStatistics(c.Param("agent_id"), period)
	if err != nil {
		return metricsFailure(prefix, err), nil
	}
	return httpx.JSON(map[string]any{"success": true, "data": map[string]any{"agent_id": c.Param("agent_id"), "stats": stats}}), nil
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
	trend, err := metrics.ForManager(manager).TrafficTrend(c.Param("agent_id"), hours, interval)
	if err != nil {
		return metricsFailure(prefix, err), nil
	}
	return httpx.JSON(map[string]any{"success": true, "data": map[string]any{
		"agent_id": c.Param("agent_id"), "trend": trend, "hours": hours,
		"interval_minutes": interval, "data_points": len(trend)}}), nil
}

// readTrafficBody bounds the body and preserves exact Agent JSON for the owner decoder.
func readTrafficBody(c *httpx.Ctx, maxLength int64, invalidMessage string) (json.RawMessage, *httpx.Response) {
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
	if !json.Valid(raw) {
		return nil, failure(400, invalidMessage)
	}
	return raw, nil
}
