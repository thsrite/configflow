package monitor

import (
	"configflow/internal/agents"
	"configflow/internal/lifecycle"
	"context"
)

// Wire installs monitor hooks and the ranking collection loop on the default runtime.
func Wire() { WireOn(lifecycle.Current()) }

// WireOn installs monitor hooks and the ranking collection loop on rt.
func WireOn(rt *lifecycle.Runtime) {
	if rt == nil {
		rt = lifecycle.Current()
	}
	agents.RegisterHooks(agents.Hooks{PublicationComplete: completeHook, PublicationCheckpoint: checkpointHook})
	rt.RegisterPeriodic(rankingPeriodic())
}

func checkpointHook(ctx context.Context, m *agents.Manager, id, deploymentID string, value any) error {
	revision, _ := value.(string)
	return CheckpointStatistics(ctx, TypedFromAgents(m), id, deploymentID, revision)
}
func completeHook(ctx context.Context, m *agents.Manager, id, deploymentID string, value any) error {
	status, _ := value.(string)
	return CompleteStatistics(ctx, TypedFromAgents(m), id, deploymentID, status)
}
