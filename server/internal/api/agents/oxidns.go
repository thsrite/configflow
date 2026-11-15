package agents

// DNS monitoring routes of backend/routes/agents.py (lines 2186–2198,
// 2216–2221, 2286–2325): _oxidns_monitor_response, get_oxidns_overview,
// reset_oxidns_dashboard, get_oxidns_clients, get_oxidns_queries,
// get_oxidns_query (A06). DNS monitoring and its local statistics baseline
// stay separate from deployment and lifecycle actions.

import (
	"errors"
	"io"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
)

func registerOxidns(r *httpx.Router) {
	r.GET("/api/agents/<agent_id>/oxidns/overview", "agents.get_oxidns_overview", auth.Require(getOxidnsOverview))
	r.POST("/api/agents/<agent_id>/oxidns/dashboard/reset", "agents.reset_oxidns_dashboard", auth.Require(resetOxidnsDashboard))
	r.GET("/api/agents/<agent_id>/oxidns/clients", "agents.get_oxidns_clients", auth.Require(getOxidnsClients))
	r.GET("/api/agents/<agent_id>/oxidns/queries", "agents.get_oxidns_queries", auth.Require(getOxidnsQueries))
	r.GET("/api/agents/<agent_id>/oxidns/queries/<record_id>", "agents.get_oxidns_query", auth.Require(getOxidnsQuery))
}

// MonitorErrorResponse is the MonitorError answer of the monitoring routes:
// {success: false, code, message} with the error's status, not stored.
func MonitorErrorResponse(err *monitor.Error) *httpx.Response {
	return httpx.JSONStatus(err.Status, jsonx.Obj("success", false, "code", err.Code, "message", err.Message)).NoStore()
}

// oxidnsMonitorResponse is _oxidns_monitor_response: the operation's
// result, or the MonitorError, both with Cache-Control: no-store; any other
// error is a 500.
func oxidnsMonitorResponse(c *httpx.Ctx, operation func(monitor.Manager) (*jsonx.Object, error)) (*httpx.Response, error) {
	manager, err := agents.Get(c.Context())
	if err != nil {
		return nil, err
	}
	result, err := operation(monitor.FromAgents(manager))
	if err != nil {
		var monitorErr *monitor.Error
		if errors.As(err, &monitorErr) {
			return MonitorErrorResponse(monitorErr), nil
		}
		return nil, err
	}
	return httpx.JSON(result).NoStore(), nil
}

func getOxidnsOverview(c *httpx.Ctx) (*httpx.Response, error) {
	return oxidnsMonitorResponse(c, func(m monitor.Manager) (*jsonx.Object, error) {
		return monitor.Overview(c.Context(), m, c.Param("agent_id"), c.Args())
	})
}

// resetOxidnsDashboard is reset_oxidns_dashboard: a bounded JSON object
// body (chunked requests too), then reset_dashboard.
func resetOxidnsDashboard(c *httpx.Ctx) (*httpx.Response, error) {
	invalidRequest := func() (*httpx.Response, error) {
		return httpx.JSONStatus(400, jsonx.Obj("success", false, "code", "INVALID_QUERY", "message", "重置请求格式无效。")).NoStore(), nil
	}
	if length, known := c.ContentLength(); c.HasArgs() || known && length > 2048 {
		return invalidRequest()
	}
	var raw []byte
	if stream := c.Stream(); stream != nil {
		var err error
		if raw, err = io.ReadAll(io.LimitReader(stream, 2049)); err != nil {
			return nil, err
		}
	}
	if len(raw) > 2048 || len(raw) > 0 && !c.IsJSON() {
		return invalidRequest()
	}
	var body any = jsonx.NewObject()
	if len(raw) > 0 {
		text, ok := agents.DecodeJSONBytes(raw)
		if !ok {
			return invalidRequest()
		}
		value, err := jsonx.ParseString(text)
		if err != nil || monitor.ExceedsDigitLimit(value) {
			return invalidRequest()
		}
		body = value
	}
	return oxidnsMonitorResponse(c, func(m monitor.Manager) (*jsonx.Object, error) {
		return monitor.ResetDashboard(c.Context(), m, c.Param("agent_id"), body)
	})
}

func getOxidnsClients(c *httpx.Ctx) (*httpx.Response, error) {
	return oxidnsMonitorResponse(c, func(m monitor.Manager) (*jsonx.Object, error) {
		return monitor.Clients(c.Context(), m, c.Param("agent_id"), c.Args())
	})
}

func getOxidnsQueries(c *httpx.Ctx) (*httpx.Response, error) {
	return oxidnsMonitorResponse(c, func(m monitor.Manager) (*jsonx.Object, error) {
		return monitor.Queries(c.Context(), m, c.Param("agent_id"), c.Args())
	})
}

func getOxidnsQuery(c *httpx.Ctx) (*httpx.Response, error) {
	return oxidnsMonitorResponse(c, func(m monitor.Manager) (*jsonx.Object, error) {
		return monitor.Query(c.Context(), m, c.Param("agent_id"), c.Param("record_id"), c.Args())
	})
}
