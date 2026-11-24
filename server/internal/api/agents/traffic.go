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

type trafficHandler struct {
	deps    Deps
	metrics *metricsHandler
}

func (h *trafficHandler) register(r *httpx.Router) {
	// Agent-authenticated (Bearer token of the Agent); listed as public in
	// internal/auth like routes/auth.py.
	r.POST("/api/agents/<agent_id>/traffic-report", "agents.agent_traffic_report", h.agentTrafficReport)
	r.POST("/api/agents/<agent_id>/traffic-history", "agents.agent_traffic_history_report", h.agentTrafficHistoryReport)
	r.GET("/api/agents/<agent_id>/traffic-history", "agents.get_agent_traffic_history", auth.Require(h.getAgentTrafficHistory))
	r.PUT("/api/agents/<agent_id>/traffic-history", "agents.set_agent_traffic_history", auth.Require(h.setAgentTrafficHistory))
	r.DELETE("/api/agents/<agent_id>/traffic-history", "agents.clear_agent_traffic_history", auth.Require(h.clearAgentTrafficHistory))
	r.PUT("/api/agents/traffic-history/retention", "agents.set_traffic_history_retention", auth.Require(h.setTrafficHistoryRetention))
	r.GET("/api/agents/<agent_id>/traffic/stats", "agents.get_agent_traffic_stats", auth.Require(h.getAgentTrafficStats))
	r.GET("/api/agents/<agent_id>/traffic/trend", "agents.get_agent_traffic_trend", auth.Require(h.getAgentTrafficTrend))
}

// agentTrafficReport is agent_traffic_report: an Agent reports its Mihomo
// traffic summary (domain discovery). Errors are not caught (500 page).
func (h *trafficHandler) agentTrafficReport(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.deps.withRepo(c.Context())
	agentID := c.Param("agent_id")
	manager, err := h.deps.manager(ctx)
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
	store, err := h.deps.trafficStore(ctx)
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
func (h *trafficHandler) agentTrafficHistoryReport(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	agentID := c.Param("agent_id")
	manager, err := h.deps.manager(ctx)
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
	if err := h.deps.trafficHistory(manager).AddTraffic(ctx, agentID, cleaned); err != nil {
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
		raw, present, err := model.EncodeMember(agent, "traffic_history_since")
		if err != nil {
			return nil, err
		}
		if present {
			since = raw
		}
	}
	retention, err := metrics.TrafficRetentionFromRepo(c.Context(), manager.Repository())
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
	if agent.ServiceType != "" && agent.ServiceType != "mihomo" {
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
func (h *trafficHandler) getAgentTrafficHistory(c *httpx.Ctx) (*httpx.Response, error) {
	manager, err := h.deps.manager(c.Context())
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
	result, err := h.deps.trafficHistory(manager).QueryTraffic(c.Context(), c.Param("agent_id"), rangeKey, by, nil)
	if err != nil {
		return nil, err
	}
	data.TrafficQuery = result
	return httpx.JSON(map[string]any{"success": true, "data": data}), nil
}

// setAgentTrafficHistory is set_agent_traffic_history: switch the traffic
// history; switching off with clear: true also deletes what was recorded.
func (h *trafficHandler) setAgentTrafficHistory(c *httpx.Ctx) (*httpx.Response, error) {
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
	manager, err := h.deps.manager(c.Context())
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
		if err := h.deps.trafficHistory(manager).DeleteContext(c.Context(), c.Param("agent_id")); err != nil {
			return nil, err
		}
	}
	state, err := trafficHistoryState(c, manager, agent)
	if err != nil {
		return nil, err
	}
	return httpx.JSON(map[string]any{"success": true, "data": state}), nil
}

// setTrafficHistoryError maps a refused traffic-history switch to 400:
// validation errors, deployment conflicts (a profile in use) and repository
// rejections all answer 400 with their message; nil leaves the error to the
// 500 page.
func setTrafficHistoryError(err error) *httpx.Response {
	var invalid *agents.ValidationError
	var conflict *agents.DeploymentConflict
	switch {
	case errors.As(err, &invalid):
		return failure(400, invalid.Message)
	case errors.As(err, &conflict):
		return failure(400, conflict.Message)
	case errors.Is(err, repo.ErrRejected):
		return failure(400, err.Error())
	}
	return nil
}

// clearAgentTrafficHistory is clear_agent_traffic_history: delete the
// recorded traffic without changing the switch.
func (h *trafficHandler) clearAgentTrafficHistory(c *httpx.Ctx) (*httpx.Response, error) {
	manager, err := h.deps.manager(c.Context())
	if err != nil {
		return nil, err
	}
	if _, response, err := mihomoAgentOrError(c, manager); response != nil || err != nil {
		return response, err
	}
	if err := h.deps.trafficHistory(manager).DeleteContext(c.Context(), c.Param("agent_id")); err != nil {
		return nil, err
	}
	return httpx.JSON(map[string]any{"success": true}), nil
}

// setTrafficHistoryRetention is set_traffic_history_retention: the
// retention shared by every Agent.
func (h *trafficHandler) setTrafficHistoryRetention(c *httpx.Ctx) (*httpx.Response, error) {
	var days int64
	raw, _ := io.ReadAll(c.Stream())
	payload, decodeErr := jsondecode.Select(raw, "days")
	if !c.IsJSON() || decodeErr != nil || jsondecode.Unmarshal(payload["days"], &days) != nil {
		days = 0
	}
	manager, err := h.deps.manager(c.Context())
	if err != nil {
		return nil, err
	}
	stored, err := metrics.SetTrafficRetention(c.Context(), manager, days)
	if err != nil {
		if errors.Is(err, metrics.ErrRetentionInvalid) || errors.Is(err, repo.ErrRejected) {
			// Repository rejections (invalid profile, profile in use or
			// already present) answer 400 with their message too.
			return failure(400, err.Error()), nil
		}
		return nil, err
	}
	return httpx.JSON(map[string]any{"success": true, "data": map[string]any{"retention_days": stored}}), nil
}

// getAgentTrafficStats is get_agent_traffic_stats (period, default total).
func (h *trafficHandler) getAgentTrafficStats(c *httpx.Ctx) (*httpx.Response, error) {
	const prefix = "获取Agent流量统计失败"
	manager, _, response := h.metrics.agentForMetrics(c, prefix)
	if response != nil {
		return response, nil
	}
	period := c.ArgDefault("period", "total")
	stats, err := h.deps.metricsHistory(manager).TrafficStatistics(c.Param("agent_id"), period)
	if err != nil {
		return metricsFailure(prefix, err), nil
	}
	return httpx.JSON(map[string]any{"success": true, "data": map[string]any{"agent_id": c.Param("agent_id"), "stats": stats}}), nil
}

// getAgentTrafficTrend is get_agent_traffic_trend (hours default 24,
// interval default 5 minutes).
func (h *trafficHandler) getAgentTrafficTrend(c *httpx.Ctx) (*httpx.Response, error) {
	const prefix = "获取Agent流量趋势失败"
	manager, _, response := h.metrics.agentForMetrics(c, prefix)
	if response != nil {
		return response, nil
	}
	hours := argBigInt(c, "hours", 24)
	interval := argBigInt(c, "interval", 5)
	trend, err := h.deps.metricsHistory(manager).TrafficTrend(c.Param("agent_id"), hours, interval)
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
