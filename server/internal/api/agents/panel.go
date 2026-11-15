package agents

// Mihomo panel, OxiDNS rule update and cache routes of
// backend/routes/agents.py (lines 2199–2215, 2222–2250, 2251–2285):
// mihomo_panel, update_oxidns_rules, oxidns_cache (A07). The browser names
// only whitelisted operations of one registered Agent; core addresses,
// credentials, plugin tags and download URLs never come from the request.

import (
	"errors"
	"io"

	"configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
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
// monitor.PanelCall.
func mihomoPanel(c *httpx.Ctx) (*httpx.Response, error) {
	method := c.Request.Method
	var body any
	if method == "PUT" || method == "PATCH" {
		body = c.JSONSilent()
	}
	return oxidnsMonitorResponse(c, func(m monitor.Manager) (*jsonx.Object, error) {
		// The core update check fetches version.txt with request_rule, which
		// reads flask.request.host_url.
		return monitor.PanelCall(monitor.WithHostURL(c.Context(), c.HostURL()), m, c.Param("agent_id"), c.Param("resource"),
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

// loadsBody is json.loads(raw) for the route bodies (ValueError,
// UnicodeError and RecursionError → ok false).
func loadsBody(raw []byte) (any, bool) {
	text, ok := agents.DecodeJSONBytes(raw)
	if !ok {
		return nil, false
	}
	value, err := jsonx.ParseString(text)
	if err != nil || monitor.ExceedsDigitLimit(value) {
		return nil, false
	}
	return value, true
}

// monitorAnswer is the JSON answer of a MonitorError: {success, code,
// message} with its status (and `flushed` of a partial cache flush).
func monitorAnswer(err error) (*jsonx.Object, int, bool) {
	var flushErr *monitor.FlushError
	if errors.As(err, &flushErr) {
		return jsonx.Obj("success", false, "code", flushErr.Err.Code, "message", flushErr.Err.Message,
			"flushed", flushErr.Flushed), flushErr.Err.Status, true
	}
	var monitorErr *monitor.Error
	if errors.As(err, &monitorErr) {
		return jsonx.Obj("success", false, "code", monitorErr.Code, "message", monitorErr.Message), monitorErr.Status, true
	}
	return nil, 0, false
}

// updateOxidnsRules is update_oxidns_rules: only the operation itself is
// exposed (an empty JSON object body at most).
func updateOxidnsRules(c *httpx.Ctx) (*httpx.Response, error) {
	data, status, err := func() (*jsonx.Object, int, error) {
		invalid := func() (*jsonx.Object, int, error) { return nil, 0, monitor.RuleUpdateFail("INVALID_REQUEST", 400) }
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
			object, isDict := value.(*jsonx.Object)
			if !c.IsJSON() || !ok || !isDict || object == nil || object.Len() != 0 {
				return invalid()
			}
		}
		manager, err := agents.Get(c.Context())
		if err != nil {
			return nil, 0, err
		}
		return monitor.RuleUpdateCall(c.Context(), monitor.FromAgents(manager), c.Param("agent_id"), c.Request.Method, nil)
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
	data, err := func() (*jsonx.Object, error) {
		invalid := func() (*jsonx.Object, error) { return nil, monitor.CacheFail("INVALID_REQUEST", 400) }
		if length, _ := c.ContentLength(); c.HasArgs() || length > limit {
			return invalid()
		}
		raw, err := readBounded(c, limit+1)
		if err != nil {
			return nil, err
		}
		var tags any
		if c.Request.Method == "GET" {
			if len(raw) > 0 {
				return invalid()
			}
		} else {
			var body any
			if c.IsJSON() && len(raw) <= limit {
				body, _ = loadsBody(raw)
			}
			object, ok := body.(*jsonx.Object)
			if !ok || object == nil || object.Len() != 1 {
				return invalid()
			}
			value, present := object.Get("tags")
			if !present {
				return invalid()
			}
			valid, err := monitor.ValidRequestTags(value)
			if err != nil {
				return nil, err
			}
			if !valid {
				return invalid()
			}
			tags = value
		}
		manager, err := agents.Get(c.Context())
		if err != nil {
			return nil, err
		}
		return monitor.CacheCall(c.Context(), monitor.FromAgents(manager), c.Param("agent_id"), c.Request.Method, tags)
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
