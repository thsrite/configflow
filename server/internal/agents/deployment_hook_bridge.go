package agents

// Hook bridges for deploy/monitor. Candidate binding and state transitions live
// in deployment_actions.go.
import (
	"context"

	"configflow/internal/model"
)

func (m *Manager) candidateReceiptMatches(a *model.Agent, state *model.Deployment) (bool, error) {
	if m.receiptMatches != nil {
		return m.receiptMatches(a, state), nil
	}
	return false, nil
}

func (m *Manager) checkpointCandidate(ctx context.Context, agentID, id string, prepared *model.PreparedContent) error {
	var sha any
	if prepared != nil {
		if prepared.DeployedConfigSHA256 != nil {
			sha = *prepared.DeployedConfigSHA256
		}
	}
	if m.publication != nil {
		if err := ctx.Err(); err != nil {
			return err
		}
		return m.publication.PublicationCheckpoint(ctx, m, agentID, id, sha)
	}
	return nil
}
