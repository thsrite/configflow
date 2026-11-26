package agents

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"configflow/internal/agents/dnsha"
	"configflow/internal/auth"
	"configflow/internal/httpx"
)

type dnsHAHandler struct {
	deps Deps
}

func (h *dnsHAHandler) register(r *httpx.Router) {
	r.Handle([]string{http.MethodGet, http.MethodPut, http.MethodDelete}, "/api/agents/<agent_id>/oxidns-ha", "agents.agent_oxidns_ha", auth.Require(h.binding))
	r.GET("/api/agents/<agent_id>/oxidns-ha-environment", "agents.agent_oxidns_ha_environment", auth.Require(h.environment))
}
func (h *dnsHAHandler) binding(c *httpx.Ctx) (*httpx.Response, error) {
	s, e := h.deps.dnsHAService(c.Context())
	if e != nil {
		return nil, e
	}
	var result any
	code := 200
	switch c.Method() {
	case http.MethodPut:
		var body json.RawMessage
		c.BindSilent(&body)
		var v dnsha.ConfigureView
		v, e = s.Configure(c.Context(), c.Param("agent_id"), body, strings.TrimRight(c.HostURL(), "/"))
		result = v
		if v.Publication != nil {
			switch v.Publication.Status {
			case "succeeded", "failed", "rollback_failed":
			default:
				code = 202
			}
		}
	case http.MethodDelete:
		force := strings.ToLower(c.ArgDefault("force", ""))
		result, e = s.Disable(c.Context(), c.Param("agent_id"), force == "1" || force == "true")
	default:
		result, e = s.Status(c.Context(), c.Param("agent_id"))
	}
	if dnsha.IsUserError(e) {
		body := map[string]any{"success": false, "message": s.FailureMessage(c.Context(), e)}
		var ha *dnsha.Error
		if errors.As(e, &ha) {
			if ha.Blockers != nil {
				blockers := make([]string, len(ha.Blockers))
				for i, v := range ha.Blockers {
					blockers[i] = s.FailureMessage(c.Context(), errors.New(v))
				}
				body["blockers"] = blockers
			}
			if ha.CanForce {
				body["can_force"] = true
			}
		}
		return httpx.JSONStatus(409, body), nil
	}
	if e != nil {
		if failure, ok := deviceResponseFailure(e); ok {
			return failure, nil
		}
		return nil, e
	}
	return httpx.JSONStatus(code, result), nil
}
func (h *dnsHAHandler) environment(c *httpx.Ctx) (*httpx.Response, error) {
	s, e := h.deps.dnsHAService(c.Context())
	if e != nil {
		return nil, e
	}
	v, status, e := s.Environment(c.Context(), c.Param("agent_id"))
	if e != nil {
		return nil, e
	}
	return httpx.JSONStatus(status, v), nil
}
