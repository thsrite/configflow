package agents

// Monitoring routes of backend/routes/agents.py (lines 2007–2099):
// get_agent_metrics, get_agent_metrics_history, get_agent_metrics_summary
// (A04).

import (
	"math/big"
	"strings"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
	"configflow/internal/model"
	"configflow/internal/taskstatus"
	"encoding/json"
	"errors"
)

func registerMetrics(r *httpx.Router) {
	r.GET("/api/agents/<agent_id>/metrics", "agents.get_agent_metrics", auth.Require(getAgentMetrics))
	r.GET("/api/agents/<agent_id>/metrics/history", "agents.get_agent_metrics_history", auth.Require(getAgentMetricsHistory))
	r.GET("/api/agents/<agent_id>/metrics/summary", "agents.get_agent_metrics_summary", auth.Require(getAgentMetricsSummary))
}

// argBigInt reads the shared native decimal counter grammar, preserving large
// ASCII integers and using the route default for invalid query text.
func argBigInt(c *httpx.Ctx, name string, def int64) *big.Int {
	text, ok := c.Arg(name)
	if !ok {
		return big.NewInt(def)
	}
	return pyArgInt(text, def)
}

// pyArgInt retains the query converter name for the existing callers.
func pyArgInt(text string, def int64) *big.Int {
	if strings.TrimSpace(text) == "" {
		return big.NewInt(def)
	}
	raw, err := json.Marshal(text)
	if err != nil {
		return big.NewInt(def)
	}
	value, err := taskstatus.Count(raw)
	if err != nil {
		return big.NewInt(def)
	}
	return value
}

// agentForMetrics is the common start of the three routes: the manager and
// the Agent; a response when the Agent is unknown or the lookup failed.
func agentForMetrics(c *httpx.Ctx, logPrefix string) (*agents.Manager, *model.Agent, *httpx.Response) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	var agent *model.Agent
	if err == nil {
		agent, err = manager.ReadAgent(ctx, c.Param("agent_id"), true)
	}
	if err != nil {
		return nil, nil, metricsFailure(logPrefix, err)
	}
	if agent == nil {
		return nil, nil, failure(404, "Agent not found")
	}
	return manager, agent, nil
}

func metricsFailure(logPrefix string, err error) *httpx.Response {
	logger.Error(logPrefix + ": " + err.Error())
	return failure(500, err.Error())
}

// getAgentMetrics is get_agent_metrics: the latest metrics of the Agent's
// last heartbeat.
func getAgentMetrics(c *httpx.Ctx) (*httpx.Response, error) {
	const prefix = "获取Agent监控数据失败"
	_, agent, response := agentForMetrics(c, prefix)
	if response != nil {
		return response, nil
	}
	value, present, err := agent.MemberJSON("system_metrics")
	if err != nil {
		return metricsFailure(prefix, err), nil
	}
	if !present {
		value = json.RawMessage(`{}`)
	}
	fields, err := jsondecode.Select(value, "collected_at")
	if err != nil {
		return metricsFailure(prefix, errors.New("system_metrics must be an object")), nil
	}
	collected := fields["collected_at"]
	if len(collected) == 0 {
		collected = json.RawMessage("null")
	}
	return httpx.JSON(map[string]any{"success": true, "data": map[string]any{"agent_id": c.Param("agent_id"), "metrics": json.RawMessage(value), "collected_at": collected}}), nil
}

// getAgentMetricsHistory is get_agent_metrics_history (hours, default 24).
func getAgentMetricsHistory(c *httpx.Ctx) (*httpx.Response, error) {
	const prefix = "获取Agent监控历史数据失败"
	manager, _, response := agentForMetrics(c, prefix)
	if response != nil {
		return response, nil
	}
	hours := argBigInt(c, "hours", 24)
	history, err := metrics.ForManager(manager).GetMetrics(c.Param("agent_id"), hours)
	if err != nil {
		return metricsFailure(prefix, err), nil
	}
	return httpx.JSON(map[string]any{"success": true, "data": map[string]any{"agent_id": c.Param("agent_id"), "history": history, "hours": hours, "data_points": len(history)}}), nil
}

// getAgentMetricsSummary is get_agent_metrics_summary (hours, default 1).
func getAgentMetricsSummary(c *httpx.Ctx) (*httpx.Response, error) {
	const prefix = "获取Agent监控统计摘要失败"
	manager, _, response := agentForMetrics(c, prefix)
	if response != nil {
		return response, nil
	}
	hours := argBigInt(c, "hours", 1)
	summary, err := metrics.ForManager(manager).GetMetricsSummary(c.Param("agent_id"), hours)
	if err != nil {
		return metricsFailure(prefix, err), nil
	}
	return httpx.JSON(map[string]any{"success": true, "data": map[string]any{"agent_id": c.Param("agent_id"), "summary": summary, "hours": hours}}), nil
}
