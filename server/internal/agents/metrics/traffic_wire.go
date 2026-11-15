package metrics

import (
	"context"

	"configflow/internal/agents"
	"configflow/internal/agents/trafficstore"
	"configflow/internal/jsonx"
)

// flushTraffic is the traffic-history half of metrics_flush.run():
// retain_agents(agent_ids) and flush(retention_days(system))['rows'].
func flushTraffic(_ context.Context, m *agents.Manager, agentIDs []string, system *jsonx.Object) (int64, error) {
	history := TrafficForManager(m)
	if _, err := history.RetainAgents(agentIDs); err != nil {
		return 0, err
	}
	retention, err := RetentionDays(system)
	if err != nil {
		return 0, err
	}
	result, err := history.Flush(retention, nil)
	if err != nil {
		return 0, err
	}
	return result.Rows, nil
}

func deleteTrafficHistory(m *agents.Manager, agentID string) error {
	return TrafficForManager(m).Delete(agentID)
}

// deleteTrafficStore is get_traffic_store().delete(agent_id), run by the
// DELETE /api/agents/<id> route after the Agent is gone.
func deleteTrafficStore(ctx context.Context, agentID string) error {
	store, err := trafficstore.Get(ctx)
	if err != nil {
		return err
	}
	return store.Delete(agentID)
}

// WireTraffic is A05's entry in internal/server wirePackages: the registry
// hook that drops an Agent's traffic history (AgentManager.delete_agent),
// the per-Agent traffic store the DELETE route clears, and the traffic
// history save inside 「监控历史保存」 (metrics_flush.run, which also runs at
// shutdown). The traffic history has no loop or task row of its own.
func WireTraffic() {
	agents.RegisterHooks(agents.Hooks{DeleteTrafficHistory: deleteTrafficHistory})
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
