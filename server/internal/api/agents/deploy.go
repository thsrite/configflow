package agents

import (
	core "configflow/internal/agents"
	"configflow/internal/auth"
	"configflow/internal/deploy"
	"configflow/internal/httpx"
	"configflow/internal/repo"
	"configflow/internal/rules"
	"errors"
	"strings"
)

func registerDeployRoutes(r *httpx.Router) {
	r.GET("/api/agents/<agent_id>/config", "agents.get_agent_config", getAgentConfig)
}
func getAgentConfig(c *httpx.Ctx) (*httpx.Response, error) {
	token, _ := c.Arg("token")
	if !auth.IsTokenWithinLength(token) {
		return failure(401, "Token required"), nil
	}
	m, err := core.Get(c.Context())
	if err != nil {
		return nil, err
	}
	a, err := m.GetAgentByToken(c.Context(), token)
	if err != nil {
		return nil, err
	}
	if a == nil || a.GetString("id") != c.Param("agent_id") {
		return failure(401, "Invalid token"), nil
	}
	source, err := loadContentSource(c.Context(), m, a, nil)
	if err != nil {
		return failure(409, "Agent profile unavailable: "+err.Error()), nil
	}
	result, err := deploy.GenerateAgentConfig(c.Context(), source, deploy.NewTarget(a), strings.TrimRight(c.URLRoot(), "/"))
	if err != nil {
		var strategy *rules.StrategyReferenceError
		if errors.As(err, &strategy) {
			return failure(400, err.Error()), nil
		}
		if errors.Is(err, repo.ErrRepository) {
			return failure(409, "Agent profile unavailable: "+err.Error()), nil
		}
		return failure(500, err.Error()), nil
	}
	return httpx.JSON(map[string]any{"success": true, "profile_id": source.ProfileID(), "content": result.Content, "md5": result.MD5, "version": result.Version}), nil
}
