// Package nodes serves shared node management, reviewed deletion and node probes.
package nodes

import (
	"errors"

	"configflow/internal/auth"
	"configflow/internal/health"
	"configflow/internal/httpx"
	"configflow/internal/jsonx"
	"configflow/internal/repo"
)

// Register declares the nodes blueprint routes.
func Register(r *httpx.Router) {
	registerCRUD(r)
	r.Handle([]string{"GET", "POST"}, "/api/nodes/latency", "nodes.node_latency", auth.Require(nodeLatency))
	r.Handle([]string{"GET", "POST"}, "/api/nodes/latency/settings", "nodes.node_latency_settings", auth.Require(nodeLatencySettings))
	r.POST("/api/nodes/latency/dns/check", "nodes.node_dns_check", auth.Require(nodeDNSCheck))
}

func failure(code int, message string) *httpx.Response {
	return httpx.JSONStatus(code, jsonx.Obj("success", false, "message", message))
}

func isValueError(err error) bool {
	var v *health.ValueError
	if errors.As(err, &v) {
		return true
	}
	return repo.IsValueError(err)
}

func stringList(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		text, ok := item.(string)
		if !ok {
			return nil, false
		}
		out = append(out, text)
	}
	return out, true
}

// nodeLatency is GET/POST /api/nodes/latency.
func nodeLatency(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	if c.Request.Method == "GET" {
		return httpx.JSON(jsonx.Obj("success", true, "results", health.LoadResults(ctx))), nil
	}
	payload := c.JSONSilent()
	if payload == nil {
		payload = jsonx.NewObject()
	}
	object, ok := payload.(*jsonx.Object)
	if !ok {
		return failure(400, "请求必须是 JSON 对象"), nil
	}
	var names, keys []string
	if value := object.Value("names"); value != nil {
		if names, ok = stringList(value); !ok {
			return failure(400, "names 必须是字符串数组"), nil
		}
	}
	if value := object.Value("keys"); value != nil {
		if keys, ok = stringList(value); !ok {
			return failure(400, "keys 必须是字符串数组"), nil
		}
	}
	if names != nil && keys != nil {
		return failure(400, "names 与 keys 不能同时指定"), nil
	}
	outcome, err := health.RunTests(ctx, names, keys)
	if err != nil {
		var busy *health.ProbeBusy
		var unavailable *health.ProbeUnavailable
		switch {
		case errors.As(err, &busy):
			return failure(409, err.Error()), nil
		case errors.As(err, &unavailable):
			return failure(503, err.Error()), nil
		case isValueError(err):
			return failure(400, err.Error()), nil
		}
		return nil, err
	}
	body := jsonx.Obj("success", true)
	body.Update(outcome)
	return httpx.JSON(body), nil
}

// nodeLatencySettings is GET/POST /api/nodes/latency/settings.
func nodeLatencySettings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	result, err := func() (*jsonx.Object, error) {
		if c.Request.Method == "GET" {
			return health.DescribeSettings(ctx, nil)
		}
		payload, ok := c.JSONSilent().(*jsonx.Object)
		if !ok || payload == nil {
			return nil, &health.ValueError{Message: "请提供测速 DNS 设置"}
		}
		var value any
		if payload.Has("dns") {
			value = payload.Value("dns")
		} else if payload.Has("dns_servers") {
			servers, err := health.NormalizeDNSServers(payload.Value("dns_servers"), false)
			if err != nil {
				return nil, err
			}
			mode := "system"
			if len(servers) > 0 {
				mode = "custom"
			}
			value = jsonx.Obj("mode", mode, "servers", servers)
		} else {
			return nil, &health.ValueError{Message: "请提供测速 DNS 设置"}
		}
		return health.SaveConfig(ctx, value)
	}()
	if err != nil {
		if isValueError(err) {
			return failure(400, err.Error()), nil
		}
		return nil, err
	}
	body := jsonx.Obj("success", true)
	body.Update(result)
	return httpx.JSON(body), nil
}

// nodeDNSCheck is POST /api/nodes/latency/dns/check.
func nodeDNSCheck(c *httpx.Ctx) (*httpx.Response, error) {
	payload, ok := c.JSONSilent().(*jsonx.Object)
	if !ok || payload == nil || !payload.Has("dns") {
		return failure(400, "请提供待检测的 DNS 设置"), nil
	}
	result, err := health.CheckSettings(c.Context(), payload.Value("dns"))
	if err != nil {
		var unavailable *health.ProbeUnavailable
		switch {
		case isValueError(err):
			return failure(400, err.Error()), nil
		case errors.As(err, &unavailable):
			return failure(503, err.Error()), nil
		}
		return nil, err
	}
	return httpx.JSON(result), nil
}
