// Package nodes serves shared node management, reviewed deletion and node probes.
package nodes

import (
	"encoding/json"
	"errors"

	"configflow/internal/auth"
	"configflow/internal/health"
	"configflow/internal/httpx"
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
	return httpx.Message(code, false, message)
}

func isValueError(err error) bool {
	var v *health.ValueError
	if errors.As(err, &v) {
		return true
	}
	return repo.IsValueError(err)
}

const (
	msgLatencyObject = "请求必须是 JSON 对象"
	msgLatencyNames  = "names 必须是字符串数组"
	msgLatencyKeys   = "keys 必须是字符串数组"
	msgLatencyBoth   = "names 与 keys 不能同时指定"
	msgLatencyDNS    = "请提供测速 DNS 设置"
	msgCheckDNS      = "请提供待检测的 DNS 设置"
)

func stringList(value json.RawMessage) ([]string, bool) {
	var raw []json.RawMessage
	if json.Unmarshal(value, &raw) != nil || raw == nil {
		return nil, false
	}
	out := make([]string, len(raw))
	for i, value := range raw {
		if string(value) == "null" || json.Unmarshal(value, &out[i]) != nil {
			return nil, false
		}
	}
	return out, true
}

// nodeLatency is GET/POST /api/nodes/latency.
func nodeLatency(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	if c.Request.Method == "GET" {
		return nativeJSON(200, struct {
			Success bool `json:"success"`
			Results any  `json:"results"`
		}{true, health.LoadLatencyResults(ctx)}), nil
	}
	object, err := readObject(c, true)
	if err != nil {
		return nil, err
	}
	if object == nil {
		// Missing, malformed and null bodies select every candidate, as before.
		var raw json.RawMessage
		if c.BindSilent(&raw) && string(raw) != "null" {
			return failure(400, msgLatencyObject), nil
		}
		object = record{}
	}
	var ok bool
	var names, keys []string
	if value := object["names"]; len(value) > 0 && string(value) != "null" {
		if names, ok = stringList(value); !ok {
			return failure(400, msgLatencyNames), nil
		}
	}
	if value := object["keys"]; len(value) > 0 && string(value) != "null" {
		if keys, ok = stringList(value); !ok {
			return failure(400, msgLatencyKeys), nil
		}
	}
	if names != nil && keys != nil {
		return failure(400, msgLatencyBoth), nil
	}
	outcome, err := health.RunLatencyTests(ctx, names, keys)
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
	return nativeJSON(200, struct {
		Success bool `json:"success"`
		health.TestOutcome
	}{true, outcome}), nil
}

// nodeLatencySettings is GET/POST /api/nodes/latency/settings.
func nodeLatencySettings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := c.Context()
	result, err := func() (health.DNSSettingsView, error) {
		if c.Request.Method == "GET" {
			return health.DescribeDNSSettingsJSON(ctx, nil)
		}
		payload, err := readObject(c, true)
		if err != nil {
			return health.DNSSettingsView{}, err
		}
		if payload == nil {
			return health.DNSSettingsView{}, &health.ValueError{Message: msgLatencyDNS}
		}
		var value json.RawMessage
		if raw, present := payload["dns"]; present {
			value = raw
		} else if raw, present := payload["dns_servers"]; present {
			servers, err := health.NormalizeDNSServersJSON(raw, false)
			if err != nil {
				return health.DNSSettingsView{}, err
			}
			mode := "system"
			if len(servers) > 0 {
				mode = "custom"
			}
			value, err = json.Marshal(health.DNSConfig{Mode: mode, Servers: servers, Bootstrap: []string{}})
			if err != nil {
				return health.DNSSettingsView{}, err
			}
		} else {
			return health.DNSSettingsView{}, &health.ValueError{Message: msgLatencyDNS}
		}
		return health.SaveDNSConfigJSON(ctx, value)
	}()
	if err != nil {
		if isValueError(err) {
			return failure(400, err.Error()), nil
		}
		return nil, err
	}
	return nativeJSON(200, struct {
		Success bool `json:"success"`
		health.DNSSettingsView
	}{true, result}), nil
}

// nodeDNSCheck is POST /api/nodes/latency/dns/check.
func nodeDNSCheck(c *httpx.Ctx) (*httpx.Response, error) {
	payload, err := readObject(c, true)
	if err != nil {
		return nil, err
	}
	if _, present := payload["dns"]; !present {
		return failure(400, msgCheckDNS), nil
	}
	result, err := health.CheckDNSSettingsJSON(c.Context(), payload["dns"])
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
	return nativeJSON(200, result), nil
}
