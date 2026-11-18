package agents

import (
	"encoding/json"
	"net/http"

	core "configflow/internal/agents"
	"configflow/internal/agents/dnsfollow"
	"configflow/internal/auth"
	"configflow/internal/httpx"
)

func registerDNSFollowRoutes(r *httpx.Router) {
	r.Handle([]string{http.MethodGet, http.MethodPut, http.MethodDelete}, "/api/agents/<agent_id>/dns-follow", "agents.agent_dns_follow", auth.Require(dnsFollowBinding))
	r.POST("/api/agents/<agent_id>/dns-follow/retry", "agents.retry_agent_dns_follow", auth.Require(dnsFollowRetry))
}

func dnsFollowBinding(c *httpx.Ctx) (*httpx.Response, error) {
	m, err := core.Get(c.Context())
	if err != nil {
		return nil, err
	}
	owner := dnsfollow.ForManager(m)
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

func dnsFollowRetry(c *httpx.Ctx) (*httpx.Response, error) {
	m, err := core.Get(c.Context())
	if err != nil {
		return nil, err
	}
	owner := dnsfollow.ForManager(m)
	result, err := owner.Retry(c.Context(), c.Param("agent_id"))
	if dnsfollow.IsUserError(err) {
		return failure(409, owner.FailureMessage(c.Context(), err)), nil
	}
	if err != nil {
		return nil, err
	}
	return httpx.JSONStatus(202, result), nil
}
