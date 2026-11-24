// Package nodes serves shared node management, reviewed deletion and node probes.
package nodes

import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/auth"
	"configflow/internal/health"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"configflow/internal/substore"
)

// Handler serves the /api/nodes CRUD and latency routes backed by explicit
// Repository and Sub-Store services.
type Handler struct {
	Repo     *repo.Repository
	SubStore *substore.Service
}

// NewHandler constructs a nodes Handler. Nil dependencies fall back to
// context/package defaults until W11.
func NewHandler(r *repo.Repository, sub *substore.Service) *Handler {
	return &Handler{Repo: r, SubStore: sub}
}

func (h *Handler) ctx(c *httpx.Ctx) context.Context {
	ctx := c.Context()
	if h != nil && h.Repo != nil {
		ctx = repo.WithRepository(ctx, h.Repo)
	}
	return ctx
}

// Register declares the nodes blueprint routes on r.
func (h *Handler) Register(r *httpx.Router) {
	h.registerCRUD(r)
	r.Handle([]string{"GET", "POST"}, "/api/nodes/latency", "nodes.node_latency", auth.Require(h.nodeLatency))
	r.Handle([]string{"GET", "POST"}, "/api/nodes/latency/settings", "nodes.node_latency_settings", auth.Require(h.nodeLatencySettings))
	r.POST("/api/nodes/latency/dns/check", "nodes.node_dns_check", auth.Require(h.nodeDNSCheck))
}

// Register declares the nodes blueprint routes.
func Register(r *httpx.Router) { NewHandler(nil, nil).Register(r) }

func failure(code int, message string) *httpx.Response {
	return httpx.Message(code, false, message)
}

func isValueError(err error) bool {
	var v *health.ValidationError
	if errors.As(err, &v) {
		return true
	}
	return errors.Is(err, repo.ErrRejected)
}

const (
	msgLatencyObject = "请求必须是 JSON 对象"
	msgLatencyNames  = "names 必须是字符串数组"
	msgLatencyKeys   = "keys 必须是字符串数组"
	msgLatencyBoth   = "names 与 keys 不能同时指定"
	msgLatencyDNS    = "请提供测速 DNS 设置"
	msgCheckDNS      = "请提供待检测的 DNS 设置"
)

// latencyRequest selects test candidates by name or key (neither: all).
type latencyRequest struct {
	Names member[[]*string] `json:"names"`
	Keys  member[[]*string] `json:"keys"`
}

// dnsSettingsRequest carries a DNS setting for the health owner to validate:
// dns as stored, or (older clients) a dns_servers list.
type dnsSettingsRequest struct {
	DNS        json.RawMessage `json:"dns"`
	DNSServers json.RawMessage `json:"dns_servers"`
}

// stringList is a list of text values, none null.
func stringList(sent member[[]*string]) ([]string, bool) {
	if !sent.OK {
		return nil, false
	}
	out := make([]string, len(sent.Value))
	for i, value := range sent.Value {
		if value == nil {
			return nil, false
		}
		out[i] = *value
	}
	return out, true
}

// nodeLatency is GET/POST /api/nodes/latency.
func (h *Handler) nodeLatency(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	r, err := h.repo(ctx)
	if err != nil {
		return nil, err
	}
	if c.Request.Method == "GET" {
		return nativeJSON(200, struct {
			Success bool `json:"success"`
			Results any  `json:"results"`
		}{true, health.LoadLatencyResults(ctx, r)}), nil
	}
	body, err := decodeObject[latencyRequest](c, true)
	if err != nil {
		return nil, err
	}
	if body == nil {
		// Missing, malformed and null bodies select every candidate, as before.
		var raw json.RawMessage
		if c.BindSilent(&raw) && string(raw) != "null" {
			return failure(400, msgLatencyObject), nil
		}
		body = &latencyRequest{}
	}
	var ok bool
	var names, keys []string
	if !body.Names.Null() {
		if names, ok = stringList(body.Names); !ok {
			return failure(400, msgLatencyNames), nil
		}
	}
	if !body.Keys.Null() {
		if keys, ok = stringList(body.Keys); !ok {
			return failure(400, msgLatencyKeys), nil
		}
	}
	if names != nil && keys != nil {
		return failure(400, msgLatencyBoth), nil
	}
	outcome, err := health.RunLatencyTests(ctx, r, names, keys)
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
func (h *Handler) nodeLatencySettings(c *httpx.Ctx) (*httpx.Response, error) {
	ctx := h.ctx(c)
	r, err := h.repo(ctx)
	if err != nil {
		return nil, err
	}
	result, err := func() (health.DNSSettingsView, error) {
		if c.Request.Method == "GET" {
			return health.DescribeDNSSettingsJSON(ctx, r, nil)
		}
		payload, err := decodeObject[dnsSettingsRequest](c, true)
		if err != nil {
			return health.DNSSettingsView{}, err
		}
		if payload == nil {
			return health.DNSSettingsView{}, &health.ValidationError{Message: msgLatencyDNS}
		}
		var value json.RawMessage
		if payload.DNS != nil {
			value = payload.DNS
		} else if payload.DNSServers != nil {
			servers, err := health.NormalizeDNSServersJSON(payload.DNSServers, false)
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
			return health.DNSSettingsView{}, &health.ValidationError{Message: msgLatencyDNS}
		}
		return health.SaveDNSConfigJSON(ctx, r, value)
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
func (h *Handler) nodeDNSCheck(c *httpx.Ctx) (*httpx.Response, error) {
	payload, err := decodeObject[dnsSettingsRequest](c, true)
	if err != nil {
		return nil, err
	}
	if payload == nil || payload.DNS == nil {
		return failure(400, msgCheckDNS), nil
	}
	ctx := h.ctx(c)
	r, err := h.repo(ctx)
	if err != nil {
		return nil, err
	}
	result, err := health.CheckDNSSettingsJSON(ctx, r, payload.DNS)
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
