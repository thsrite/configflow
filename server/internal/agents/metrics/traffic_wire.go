package metrics

import (
	"configflow/internal/jsondecode"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"context"
	"encoding/json"
	"path/filepath"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/repo"
)

// flushTraffic is the traffic-history half of metrics_flush.run():
// retain_agents(agent_ids) and flush(retention_days(system))['rows'].
func flushTraffic(ctx context.Context, m *agents.Manager, agentIDs []string, system *model.System) (int64, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	history := TrafficForManager(m)
	if _, err := history.RetainAgentsContext(ctx, agentIDs); err != nil {
		return 0, err
	}
	retention, err := TrafficRetention(system)
	if err != nil {
		return 0, err
	}
	result, err := history.FlushContext(ctx, retention, nil)
	if err != nil {
		return 0, err
	}
	return result.Rows, nil
}

func deleteTrafficHistoryContext(ctx context.Context, m *agents.Manager, id string) error {
	return TrafficForManager(m).DeleteContext(ctx, id)
}

func retentionDaysJSON(raw json.RawMessage) (int64, error) {
	present, err := model.Truthy(raw)
	if err != nil {
		return 0, err
	}
	if !present {
		return DefaultRetention, nil
	}
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil {
		return 0, ErrTrafficSettingsShape
	}
	value, present := fields.Get(RetentionSetting)
	if !present {
		return DefaultRetention, nil
	}
	var days int64
	if jsondecode.Unmarshal(value, &days) != nil {
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
	r, err := repo.GetRepository(ctx)
	if err != nil {
		return err
	}
	return trafficstore.NewNative(filepath.Join(r.DataDir(), "traffic")).DeleteContext(ctx, agentID)
}

// WireTraffic is A05's entry in the server wiring (server.New): the registry
// hook that drops an Agent's traffic history (AgentManager.delete_agent) and
// the per-Agent traffic store the DELETE route clears. The traffic history has
// no loop or task row of its own.
func WireTraffic() {
	agents.RegisterHooks(agents.Hooks{DeleteTrafficHistoryContext: deleteTrafficHistoryContext})
	agents.OnAgentDeleted("traffic_store", deleteTrafficStore)
}
