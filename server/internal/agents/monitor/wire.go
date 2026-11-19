package monitor

import (
	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"context"
)

// Wire is A07's entry in internal/server wirePackages: the ranking history
// store read by the overview (get_history), the publication checkpoint /
// completion hooks of the registry, and the ranking collection loop
// (oxidns_ranking_history.start_loop, which app.py starts in the service
// process; once per process).
func Wire() {
	SetRankingHistory(rankingStoreFactory)
	agents.RegisterHooks(agents.Hooks{PublicationComplete: completeHook, PublicationCheckpoint: checkpointHook})
	lifecycle.RegisterPeriodic(rankingPeriodic())
}

func checkpointHook(ctx context.Context, m *agents.Manager, id, deploymentID string, value any) error {
	revision, _ := value.(string)
	return CheckpointStatistics(ctx, TypedFromAgents(m), id, deploymentID, revision)
}
func completeHook(ctx context.Context, m *agents.Manager, id, deploymentID string, value any) error {
	status, _ := value.(string)
	return CompleteStatistics(ctx, TypedFromAgents(m), id, deploymentID, status)
}
