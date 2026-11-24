package agents

// HA owner hook dispatch. Pair evidence, summaries and default transitions live
// in ha_model.go.
import (
	"context"
	"errors"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// ErrPairIncomplete means saved HA members no longer form a reciprocal pair.
var ErrPairIncomplete = errors.New(msgHAPairIncomplete)

const haField = "oxidns_ha_publication"

// HAField is the persisted authoritative pair journal member.
const HAField = haField

// GetDeploymentModel dispatches the HA owner or reconciles a standalone task.
func (m *Manager) GetDeploymentModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pair := m.pairPublisher(); pair != nil {
		return pair.PairDeployment(ctx, agentID, id)
	}
	return m.defaultHADeploymentModel(ctx, agentID, id)
}

// ActivateDeploymentModel dispatches the HA owner before activating a candidate.
func (m *Manager) ActivateDeploymentModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if pair := m.pairPublisher(); pair != nil {
		return pair.ActivatePair(ctx, agentID, id)
	}
	return m.defaultHAActivateModel(ctx, agentID, id)
}

// ErrHAOwnerMissing refuses active HA operations without their registered owner.
var ErrHAOwnerMissing = errors.New("agents: DNS HA owner is not wired")
