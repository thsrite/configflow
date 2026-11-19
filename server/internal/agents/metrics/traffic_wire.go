package metrics

import (
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"configflow/internal/repo"
	"context"
	"encoding/json"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/jsonx"
)

// flushTraffic is the traffic-history half of metrics_flush.run():
// retain_agents(agent_ids) and flush(retention_days(system))['rows'].
func flushTraffic(ctx context.Context, m *agents.Manager, agentIDs []string, system *jsonx.Object) (int64, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	history := TrafficForManager(m)
	if _, err := history.RetainAgentsContext(ctx, agentIDs); err != nil {
		return 0, err
	}
	raw, err := json.Marshal(system.Value("system_config"))
	if err != nil {
		return 0, err
	}
	retention, err := retentionDaysJSON(raw)
	if err != nil {
		return 0, err
	}
	result, err := history.FlushContext(ctx, retention, nil)
	if err != nil {
		return 0, err
	}
	return result.Rows, nil
}

func deleteTrafficHistory(m *agents.Manager, agentID string) error {
	return TrafficForManager(m).Delete(agentID)
}

func deleteTrafficHistoryContext(ctx context.Context, m *agents.Manager, id string) error {
	return TrafficForManager(m).DeleteContext(ctx, id)
}

func retentionDaysJSON(raw json.RawMessage) (int64, error) {
	present, err := model.JSONHasContent(raw)
	if err != nil {
		return 0, err
	}
	if !present {
		return DefaultRetention, nil
	}
	var fields model.Fields
	if json.Unmarshal(raw, &fields) != nil {
		return 0, repo.NewValidationError(msgTrafficSettingsShape)
	}
	value, present := fields.Get(RetentionSetting)
	if !present {
		return DefaultRetention, nil
	}
	var days int64
	if json.Unmarshal(value, &days) != nil {
		return DefaultRetention, nil
	}
	for _, choice := range RetentionChoices {
		if days == choice {
			return days, nil
		}
	}
	return DefaultRetention, nil
}

// deleteTrafficStore is get_traffic_store().delete(agent_id), run by the
// DELETE /api/agents/<id> route after the Agent is gone.
func deleteTrafficStore(ctx context.Context, agentID string) error {
	store, err := trafficstore.Get(ctx)
	if err != nil {
		return err
	}
	return store.DeleteContext(ctx, agentID)
}

// WireTraffic is A05's entry in internal/server wirePackages: the registry
// hook that drops an Agent's traffic history (AgentManager.delete_agent),
// the per-Agent traffic store the DELETE route clears, and the traffic
// history save inside 「监控历史保存」 (metrics_flush.run, which also runs at
// shutdown). The traffic history has no loop or task row of its own.
func WireTraffic() {
	agents.RegisterHooks(agents.Hooks{DeleteTrafficHistory: deleteTrafficHistory, DeleteTrafficHistoryContext: deleteTrafficHistoryContext})
	agents.OnAgentDeleted("traffic_store", deleteTrafficStore)
	SetTrafficFlush(flushTraffic)
}

// TrafficFlushSet reports whether the traffic history save is installed
// in run() (tests).
func TrafficFlushSet() bool {
	trafficMu.Lock()
	defer trafficMu.Unlock()
	return trafficFlush != nil
}
