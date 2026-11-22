package agents

// Hook bridges for deploy/monitor. Candidate binding and state transitions live
// in deployment_actions.go.
import (
	"context"

	"configflow/internal/model"
)

func (m *Manager) candidateReceiptMatches(a *model.Agent, state *model.Deployment) (bool, error) {
	hook := currentHooks().ReceiptMatches
	if hook == nil {
		return false, nil
	}
	return hook(a, state), nil
}

func (m *Manager) checkpointCandidate(ctx context.Context, agentID, id string, prepared *model.PreparedContent) error {
	hook := currentHooks().PublicationCheckpoint
	if hook == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var sha any
	if prepared != nil {
		if prepared.DeployedConfigSHA256 != nil {
			sha = *prepared.DeployedConfigSHA256
		}
	}
	return hook(ctx, m, agentID, id, sha)
}
