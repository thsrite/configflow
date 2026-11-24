package agents

import (
	"encoding/json"
	"net/http"

	"configflow/internal/agents/dnsfollow"
	"configflow/internal/auth"
	"configflow/internal/httpx"
)

type dnsFollowHandler struct {
	deps Deps
}

func (h *dnsFollowHandler) register(r *httpx.Router) {
	r.Handle([]string{http.MethodGet, http.MethodPut, http.MethodDelete}, "/api/agents/<agent_id>/dns-follow", "agents.agent_dns_follow", auth.Require(h.binding))
	r.POST("/api/agents/<agent_id>/dns-follow/retry", "agents.retry_agent_dns_follow", auth.Require(h.retry))
}

func (h *dnsFollowHandler) binding(c *httpx.Ctx) (*httpx.Response, error) {
	owner, err := h.deps.dnsFollowService(c.Context())
	if err != nil {
		return nil, err
	}
	var result any
	switch c.Method() {
	case http.MethodPut:
		var body json.RawMessage
		c.BindSilent(&body)
		result, err = owner.Configure(c.Context(), c.Param("agent_id"), body)
	case http.MethodDelete:
		result, err = owner.Disable(c.Context(), c.Param("agent_id"))
	default:
		result, err = owner.Options(c.Context(), c.Param("agent_id"))
	}
	if dnsfollow.IsUserError(err) {
		return failure(409, owner.FailureMessage(c.Context(), err)), nil
	}
	if err != nil {
		return nil, err
	}
	return httpx.JSON(result), nil
}

func (h *dnsFollowHandler) retry(c *httpx.Ctx) (*httpx.Response, error) {
	owner, err := h.deps.dnsFollowService(c.Context())
	if err != nil {
		return nil, err
	}
	result, err := owner.Retry(c.Context(), c.Param("agent_id"))
	if dnsfollow.IsUserError(err) {
		return failure(409, owner.FailureMessage(c.Context(), err)), nil
	}
	if err != nil {
		return nil, err
	}
	return httpx.JSONStatus(202, result), nil
}
