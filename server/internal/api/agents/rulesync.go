package agents

import (
	"configflow/internal/agents"
	"configflow/internal/agents/rulesync"
	"configflow/internal/auth"
	"configflow/internal/httpx"
	"encoding/json"
	"errors"
)

const msgRuleSyncRequest = "规则同步设置无效"
const msgRuleSyncNotFound = "未找到这台 Agent"

func registerRuleSync(r *httpx.Router) {
	r.Handle([]string{"GET", "PUT"}, "/api/agents/<agent_id>/rule-sync", "agents.agent_rule_sync", auth.Require(agentRuleSync))
}
func agentRuleSync(c *httpx.Ctx) (*httpx.Response, error) {
	s, e := rulesync.Get(c.Context())
	if e != nil {
		return nil, e
	}
	var view rulesync.StatusView
	if c.Request.Method == "PUT" {
		if c.HasArgs() || c.Request.ContentLength > 1024 {
			return failure(400, msgRuleSyncRequest), nil
		}
		raw, e := c.Data()
		if e != nil {
			return nil, e
		}
		if !c.IsJSON() {
			raw = nil
		}
		view, e = s.Configure(c.Context(), c.Param("agent_id"), json.RawMessage(raw))
		if e != nil {
			return ruleSyncError(e)
		}
	} else {
		view, e = s.Status(c.Context(), c.Param("agent_id"))
		if e != nil {
			return ruleSyncError(e)
		}
	}
	response := httpx.JSON(view)
	response.Header().Set("Cache-Control", "no-store")
	return response, nil
}
func ruleSyncError(err error) (*httpx.Response, error) {
	if errors.Is(err, rulesync.ErrAgentNotFound) {
		return failure(404, msgRuleSyncNotFound), nil
	}
	var refusal *rulesync.Error
	if errors.As(err, &refusal) {
		return failure(400, refusal.Message), nil
	}
	if errors.Is(err, agents.ErrPairIncomplete) {
		return failure(400, agents.ErrPairIncomplete.Error()), nil
	}
	return nil, err
}
