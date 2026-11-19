package agents

// Legacy hook codecs for deploy/monitor, whose own S steps own these hooks.
// Candidate binding and state transitions live in deployment_actions.go.
import (
	"context"
	"encoding/json"

	"configflow/internal/jsonx"
	"configflow/internal/model"
)

func deploymentObject(state *model.Deployment) (*Record, error) {
	if state == nil {
		return nil, nil
	}
	raw, err := model.Marshal(state)
	if err != nil {
		return nil, err
	}
	var result Record
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
func (m *Manager) candidateReceiptMatches(a *model.Agent, state *model.Deployment) (bool, error) {
	hook := currentHooks().ReceiptMatches
	if hook == nil {
		return false, nil
	}
	agent, err := agentObject(a)
	if err != nil {
		return false, err
	}
	record, err := deploymentObject(state)
	if err != nil {
		return false, err
	}
	return hook(agent, record), nil
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
			sha, err = jsonx.Parse(raw)
			if err != nil {
				return err
			}
		}
	}
	return hook(ctx, m, agentID, id, sha)
}
