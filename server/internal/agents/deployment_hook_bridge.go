package agents

// Hook bridges for deploy/monitor. Candidate binding and state transitions live
// in deployment_actions.go.
import (
	"context"
	"encoding/json"

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
		raw, present, err := prepared.MemberJSON("deployed_config_sha256")
		if err != nil {
			return err
		}
		if present {
			if err := json.Unmarshal(raw, &sha); err != nil {
				return err
			}
		}
	}
	return hook(ctx, m, agentID, id, sha)
}
