package agents

// DNS monitoring routes of backend/routes/agents.py (lines 2186–2198,
// 2216–2221, 2286–2325): _oxidns_monitor_response, get_oxidns_overview,
// reset_oxidns_dashboard, get_oxidns_clients, get_oxidns_queries,
// get_oxidns_query (A06). DNS monitoring and its local statistics baseline
// stay separate from deployment and lifecycle actions.

import (
	"encoding/json"
	"errors"
	"io"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/auth"
	"configflow/internal/httpx"
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
	return httpx.JSONStatus(err.Status, map[string]any{"success": false, "code": err.Code, "message": err.Message}).NoStore()
}

// oxidnsMonitorResponse is _oxidns_monitor_response: the operation's
// result, or the MonitorError, both with Cache-Control: no-store; any other
// error is a 500.
func oxidnsMonitorResponse(c *httpx.Ctx, operation func(monitor.Registry) (any, error)) (*httpx.Response, error) {
	manager, err := agents.Get(c.Context())
	if err != nil {
		return nil, err
	}
	result, err := operation(monitor.TypedFromAgents(manager))
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
	return oxidnsMonitorResponse(c, func(m monitor.Registry) (any, error) {
		return monitor.ReadOverview(c.Context(), m, c.Param("agent_id"), c.Args())
	})
}

// resetOxidnsDashboard is reset_oxidns_dashboard: a bounded JSON object
// body (chunked requests too), then reset_dashboard.
func resetOxidnsDashboard(c *httpx.Ctx) (*httpx.Response, error) {
	invalidRequest := func() (*httpx.Response, error) {
		return httpx.JSONStatus(400, map[string]any{"success": false, "code": "INVALID_QUERY", "message": msgResetRequest}).NoStore(), nil
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
	body := json.RawMessage(`{}`)
	if len(raw) > 0 {
		var ok bool
		body, ok = loadsBody(raw)
		if !ok {
			return invalidRequest()
		}
	}
	return oxidnsMonitorResponse(c, func(m monitor.Registry) (any, error) {
		return monitor.ResetStatistics(c.Context(), m, c.Param("agent_id"), body)
	})
}

func getOxidnsClients(c *httpx.Ctx) (*httpx.Response, error) {
	return oxidnsMonitorResponse(c, func(m monitor.Registry) (any, error) {
		return monitor.ReadClients(c.Context(), m, c.Param("agent_id"), c.Args())
	})
}

func getOxidnsQueries(c *httpx.Ctx) (*httpx.Response, error) {
	return oxidnsMonitorResponse(c, func(m monitor.Registry) (any, error) {
		return monitor.ReadQueries(c.Context(), m, c.Param("agent_id"), c.Args())
	})
}

func getOxidnsQuery(c *httpx.Ctx) (*httpx.Response, error) {
	return oxidnsMonitorResponse(c, func(m monitor.Registry) (any, error) {
		return monitor.ReadQuery(c.Context(), m, c.Param("agent_id"), c.Param("record_id"), c.Args())
	})
}
