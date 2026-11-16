package agents

// Monitoring routes of backend/routes/agents.py (lines 2007–2099):
// get_agent_metrics, get_agent_metrics_history, get_agent_metrics_summary
// (A04).

import (
	"math/big"

	"configflow/internal/agents"
	"configflow/internal/agents/metrics"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/pystr"
)

func registerMetrics(r *httpx.Router) {
	r.GET("/api/agents/<agent_id>/metrics", "agents.get_agent_metrics", auth.Require(getAgentMetrics))
	r.GET("/api/agents/<agent_id>/metrics/history", "agents.get_agent_metrics_history", auth.Require(getAgentMetricsHistory))
	r.GET("/api/agents/<agent_id>/metrics/summary", "agents.get_agent_metrics_summary", auth.Require(getAgentMetricsSummary))
}

// argBigInt is request.args.get(name, type=int, default=def) with ints of
// any size: Werkzeug's int(value), CPython 3.11's int() of a str (its
// Unicode 14 digits and white space, underscores, the 4300 digit limit),
// the default for its ValueError.
func argBigInt(c *httpx.Ctx, name string, def int64) *big.Int {
	text, ok := c.Arg(name)
	if !ok {
		return big.NewInt(def)
	}
	return pyArgInt(text, def)
}

// pyArgInt is `int(text)`, def on ValueError.
func pyArgInt(text string, def int64) *big.Int {
	value, err := pystr.Int(text, 10)
	if err != nil {
		return big.NewInt(def)
	}
	return value
}

// agentForMetrics is the common start of the three routes: the manager and
// the Agent; a response when the Agent is unknown or the lookup failed.
func agentForMetrics(c *httpx.Ctx, logPrefix string) (*agents.Manager, *jsonx.Object, *httpx.Response) {
	ctx := c.Context()
	manager, err := agents.Get(ctx)
	var agent *jsonx.Object
	if err == nil {
		agent, err = manager.GetAgentByID(ctx, c.Param("agent_id"), true)
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
	value, present := agent.Get("system_metrics")
	if !present {
		value = jsonx.NewObject()
	}
	object, ok := value.(*jsonx.Object)
	if !ok || object == nil {
		return metricsFailure(prefix, &metrics.AttributeError{Message: "'" + metrics.PyTypeName(value) + "' object has no attribute 'get'"}), nil
	}
	collected, present := object.Get("collected_at")
	if !present {
		collected = nil
	}
	return httpx.JSON(jsonx.Obj("success", true, "data", jsonx.Obj(
		"agent_id", c.Param("agent_id"), "metrics", value, "collected_at", collected))), nil
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
	return httpx.JSON(jsonx.Obj("success", true, "data", jsonx.Obj(
		"agent_id", c.Param("agent_id"), "history", history, "hours", jsonx.BigInt(hours), "data_points", len(history)))), nil
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
	return httpx.JSON(jsonx.Obj("success", true, "data", jsonx.Obj(
		"agent_id", c.Param("agent_id"), "summary", summary, "hours", jsonx.BigInt(hours)))), nil
}
