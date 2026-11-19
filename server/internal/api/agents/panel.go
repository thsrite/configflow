package agents

// Mihomo panel, OxiDNS rule update and cache routes of
// backend/routes/agents.py (lines 2199–2215, 2222–2250, 2251–2285):
// mihomo_panel, update_oxidns_rules, oxidns_cache (A07). The browser names
// only whitelisted operations of one registered Agent; core addresses,
// credentials, plugin tags and download URLs never come from the request.

import (
	"encoding/json"
	"errors"
	"io"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsondecode"
)

func registerPanel(r *httpx.Router) {
	r.Handle([]string{"GET", "PUT", "PATCH", "DELETE", "POST"}, "/api/agents/<agent_id>/mihomo/<path:resource>",
		"agents.mihomo_panel", auth.Require(mihomoPanel))
	r.Handle([]string{"GET", "POST"}, "/api/agents/<agent_id>/oxidns/rules/update", "agents.update_oxidns_rules",
		auth.Require(updateOxidnsRules))
	r.GET("/api/agents/<agent_id>/oxidns/cache", "agents.oxidns_cache", auth.Require(oxidnsCache))
	r.POST("/api/agents/<agent_id>/oxidns/cache/flush", "agents.oxidns_cache", auth.Require(oxidnsCache))
}

// mihomoPanel is mihomo_panel: the resource/method whitelist lives in
// monitor.CallPanel.
func mihomoPanel(c *httpx.Ctx) (*httpx.Response, error) {
	method := c.Request.Method
	var body json.RawMessage
	if method == "PUT" || method == "PATCH" {
		c.BindSilent(&body)
	}
	return oxidnsMonitorResponse(c, func(m monitor.Registry) (any, error) {
		// The core update check fetches version.txt with request_rule, which
		// reads flask.request.host_url.
		return monitor.CallPanel(monitor.WithHostURL(c.Context(), c.HostURL()), m, c.Param("agent_id"), c.Param("resource"),
			method, c.Args(), body, "")
	})
}

// readBounded is request.stream.read(limit).
func readBounded(c *httpx.Ctx, limit int64) ([]byte, error) {
	stream := c.Stream()
	if stream == nil {
		return nil, nil
	}
	return io.ReadAll(io.LimitReader(stream, limit))
}

// loadsBody decodes native JSON bytes without imposing an interpreter digit cap.
func loadsBody(raw []byte) (json.RawMessage, bool) {
	text, ok := agents.DecodeJSONBytes(raw)
	if !ok {
		return nil, false
	}
	var value json.RawMessage
	if jsondecode.Unmarshal([]byte(text), &value) != nil {
		return nil, false
	}
	return value, true
}

// monitorAnswer is the JSON answer of a MonitorError: {success, code,
// message} with its status (and `flushed` of a partial cache flush).
func monitorAnswer(err error) (any, int, bool) {
	var flushErr *monitor.CacheOperationError
	if errors.As(err, &flushErr) {
		return map[string]any{"success": false, "code": flushErr.Err.Code, "message": flushErr.Err.Message, "flushed": flushErr.Flushed}, flushErr.Err.Status, true
	}
	var monitorErr *monitor.Error
	if errors.As(err, &monitorErr) {
		return map[string]any{"success": false, "code": monitorErr.Code, "message": monitorErr.Message}, monitorErr.Status, true
	}
	return nil, 0, false
}

// updateOxidnsRules is update_oxidns_rules: only the operation itself is
// exposed (an empty JSON object body at most).
func updateOxidnsRules(c *httpx.Ctx) (*httpx.Response, error) {
	data, status, err := func() (any, int, error) {
		invalid := func() (any, int, error) { return nil, 0, monitor.RuleUpdateFail("INVALID_REQUEST", 400) }
		if length, _ := c.ContentLength(); c.HasArgs() || length > 1024 {
			return invalid()
		}
		raw, err := readBounded(c, 1025)
		if err != nil {
			return nil, 0, err
		}
		if len(raw) > 1024 || c.Request.Method == "GET" && len(raw) > 0 {
			return invalid()
		}
		if len(raw) > 0 {
			value, ok := loadsBody(raw)
			var object map[string]json.RawMessage
			if !c.IsJSON() || !ok || jsondecode.Unmarshal(value, &object) != nil || object == nil || len(object) != 0 {
				return invalid()
			}
		}
		manager, err := agents.Get(c.Context())
		if err != nil {
			return nil, 0, err
		}
		return monitor.CallRuleUpdate(c.Context(), monitor.TypedFromAgents(manager), c.Param("agent_id"), c.Request.Method, nil)
	}()
	if err != nil {
		answer, errStatus, ok := monitorAnswer(err)
		if !ok {
			return nil, err
		}
		data, status = answer, errStatus
	}
	return httpx.JSONStatus(status, data).NoStore(), nil
}

// oxidnsCache is oxidns_cache: the browser names only cache tags of this
// one Agent; the Agent rechecks them against its own deployed
// configuration.
func oxidnsCache(c *httpx.Ctx) (*httpx.Response, error) {
	const limit = 16 * 1024
	data, err := func() (any, error) {
		invalid := func() (any, error) { return nil, monitor.CacheFail("INVALID_REQUEST", 400) }
		if length, _ := c.ContentLength(); c.HasArgs() || length > limit {
			return invalid()
		}
		raw, err := readBounded(c, limit+1)
		if err != nil {
			return nil, err
		}
		var tags []string
		if c.Request.Method == "GET" {
			if len(raw) > 0 {
				return invalid()
			}
		} else {
			var body json.RawMessage
			if c.IsJSON() && len(raw) <= limit {
				body, _ = loadsBody(raw)
			}
			var object map[string]json.RawMessage
			if jsondecode.Unmarshal(body, &object) != nil || object == nil || len(object) != 1 {
				return invalid()
			}
			raw, present := object["tags"]
			if !present || jsondecode.Unmarshal(raw, &tags) != nil {
				return invalid()
			}
		}
		manager, err := agents.Get(c.Context())
		if err != nil {
			return nil, err
		}
		return monitor.CallCache(c.Context(), monitor.TypedFromAgents(manager), c.Param("agent_id"), c.Request.Method, tags)
	}()
	status := 200
	if err != nil {
		answer, errStatus, ok := monitorAnswer(err)
		if !ok {
			return nil, err
		}
		data, status = answer, errStatus
	}
	return httpx.JSONStatus(status, data).NoStore(), nil
}
