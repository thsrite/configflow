package agents

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	registry "configflow/internal/agents"
	"configflow/internal/agents/monitor"
	"configflow/internal/auth"
	"configflow/internal/gateway/host"
	"configflow/internal/httpx"
	"configflow/internal/logx"
	"configflow/internal/repo"
	"configflow/internal/setupflow"
)

var gatewayLogger = logx.Get("backend.routes.linux_gateway_agents")

func registerGateway(r *httpx.Router) {
	r.Handle([]string{"GET", "POST"}, "/api/agents/<agent_id>/linux-gateway/<operation>", "agents.linux_gateway_operation", auth.Require(gatewayOperation))
	r.Handle([]string{"GET", "POST"}, "/api/agents/gateway-forwarding", "agents.agent_gateway_forwarding", auth.Require(gatewayForwarding))
}
func gatewayBridge(c *httpx.Ctx) (*host.Bridge, error) {
	r, err := repo.GetRepository(c.Context())
	if err != nil {
		return nil, err
	}
	m, err := registry.Get(c.Context())
	if err != nil {
		return nil, err
	}
	return &host.Bridge{Repository: r, Manager: m}, nil
}
func gatewayFailure(code string, status int, message string) (*httpx.Response, error) {
	return MonitorErrorResponse(monitor.NewError(code, status, message)), nil
}
func gatewayResult(value any, err error) (*httpx.Response, error) {
	if err != nil {
		if errors.Is(err, setupflow.ErrReadSession) {
			return httpx.Message(500, false, setupflow.ErrReadSession.Error()).NoStore(), nil
		}
		var e *monitor.Error
		if errors.As(err, &e) {
			return MonitorErrorResponse(e), nil
		}
		return nil, err
	}
	return httpx.JSON(value).NoStore(), nil
}
func gatewayOperation(c *httpx.Ctx) (*httpx.Response, error) {
	profile := c.HeaderDefault("X-ConfigFlow-Profile", "")
	if profile == "" {
		profile = c.ArgDefault("profile_id", "")
	}
	if profile == "" {
		return gatewayFailure("PROFILE_REQUIRED", 400, msgGatewayProfile)
	}
	profile, err := repo.ResolveProfileID(c.Context(), profile)
	if err != nil {
		return nil, err
	}
	var body json.RawMessage
	if c.Request.Method == "POST" {
		if length, _ := c.ContentLength(); length > 4096 {
			return gatewayFailure("INVALID_REQUEST", 400, msgGatewayBodySize)
		}
		// Bound unknown-length streams too, then pass the same bytes through Bind.
		raw, err := io.ReadAll(io.LimitReader(c.Stream(), 4097))
		if err != nil {
			return nil, err
		}
		if len(raw) > 4096 {
			return gatewayFailure("INVALID_REQUEST", 400, msgGatewayBodySize)
		}
		if len(raw) == 0 {
			body = json.RawMessage(`{}`)
		} else {
			c.Request.Body = io.NopCloser(bytes.NewReader(raw))
			c.Request.Header.Set("Content-Type", "application/json")
			if !c.BindSilent(&body) {
				return gatewayFailure("INVALID_REQUEST", 400, msgGatewayBodyJSON)
			}
		}
	}
	bridge, err := gatewayBridge(c)
	if err != nil {
		return nil, err
	}
	value, err := bridge.Call(c.Context(), c.Param("agent_id"), c.Param("operation"), c.Request.Method, profile, body)
	return gatewayResult(value, err)
}
func gatewayForwarding(c *httpx.Ctx) (*httpx.Response, error) {
	bridge, err := gatewayBridge(c)
	if err != nil {
		return nil, err
	}
	linked, err := bridge.ProxyGatewaysAllProfiles(c.Context())
	if err != nil {
		return nil, err
	}
	// HEAD is served by this GET route and stays read-only. Python took every
	// method but GET as the POST branch, so a HEAD with a JSON body persisted
	// forwarding on the device (A20 review).
	if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
		entries := []host.ForwardingEntry{}
		for _, link := range linked {
			entry, err := bridge.Forwarding(c.Context(), link.AgentID, link.Routers)
			if err != nil {
				return nil, err
			}
			entries = append(entries, entry)
		}
		return gatewayResult(struct {
			Success  bool                   `json:"success"`
			Gateways []host.ForwardingEntry `json:"gateways"`
		}{true, entries}, nil)
	}
	var body map[string]json.RawMessage
	if !c.BindSilent(&body) || len(body) != 1 {
		return gatewayFailure("INVALID_REQUEST", 400, msgGatewayAgentID)
	}
	var id string
	if json.Unmarshal(body["agent_id"], &id) != nil || string(body["agent_id"]) == "null" {
		return gatewayFailure("INVALID_REQUEST", 400, msgGatewayAgentID)
	}
	for _, link := range linked {
		if id == link.AgentID {
			value, err := bridge.PersistForwarding(c.Context(), id, link.Routers)
			if err == nil && string(value["success"]) == "true" {
				gatewayLogger.Infof(msgGatewayForwarded, logx.LogText(id, 120))
			}
			return gatewayResult(value, err)
		}
	}
	return gatewayFailure("NOT_LINKED", 404, msgGatewayNotLinked)
}
