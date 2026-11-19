package agents

// Legacy HA codecs and owner hook dispatch. Pair evidence, summaries and default
// transitions live in ha_model.go and never inspect a dynamic document.
import (
	"context"
	"encoding/json"
	"errors"

	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// ErrPairIncomplete means saved HA members no longer form a reciprocal pair.
var ErrPairIncomplete = errors.New(msgHAPairIncomplete)

// HAEnabled adapts an old registry record to the strict typed HA setting.
func HAEnabled(a *Record) bool {
	record, err := agentModel(a)
	return err == nil && HAEnabledModel(record)
}

// HAPair returns owned legacy projections of one typed reciprocal pair.
func (m *Manager) HAPair(ctx context.Context, id string) ([]*Record, error) {
	pair, err := m.ReadHAPair(ctx, id)
	if err != nil {
		return nil, err
	}
	if pair == nil {
		return nil, nil
	}
	result := make([]*Record, len(pair))
	for i := range pair {
		result[i], err = agentObject(&pair[i])
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

const haField = "oxidns_ha_publication"

// HAField is the persisted authoritative pair journal member.
const HAField = haField

func haActive(a *jsonx.Object) bool {
	record, err := agentModel(a)
	return err == nil && HAActiveModel(record)
}

// HASummary adapts one journal to the shared typed public projection.
func HASummary(job *jsonx.Object) *jsonx.Object {
	var journal model.HAJournal
	raw, err := json.Marshal(job)
	if err != nil || json.Unmarshal(raw, &journal) != nil {
		return jsonx.NewObject()
	}
	summary, err := HASummaryModel(&journal)
	if err != nil {
		return jsonx.NewObject()
	}
	raw, err = model.Marshal(summary)
	if err != nil {
		return jsonx.NewObject()
	}
	var result Record
	if json.Unmarshal(raw, &result) != nil {
		return jsonx.NewObject()
	}
	return &result
}

// HAAggregate adapts old public journals to one typed aggregate operation.
func HAAggregate(record, public *jsonx.Object) *jsonx.Object {
	var task model.Deployment
	var summary model.HAPublicationSummary
	raw, err := json.Marshal(record)
	if err != nil || json.Unmarshal(raw, &task) != nil {
		return jsonx.NewObject()
	}
	raw, err = json.Marshal(public)
	if err != nil || json.Unmarshal(raw, &summary) != nil {
		return jsonx.NewObject()
	}
	result, err := HAAggregateModel(&task, &summary)
	if err != nil {
		return jsonx.NewObject()
	}
	out, err := deploymentObject(result)
	if err != nil {
		return jsonx.NewObject()
	}
	return out
}

func defaultServiceOperation(ctx context.Context, m *Manager, id string, operation func() (*jsonx.Object, error), uninstall bool) (*jsonx.Object, error) {
	a, err := m.ReadAgent(ctx, id, true)
	if err != nil {
		return nil, err
	}
	if rejection, err := haOperationConflict(a); rejection != nil || err != nil {
		if err != nil {
			return nil, err
		}
		return deploymentObject(rejection)
	}
	if uninstall && HAEnabledModel(a) {
		rejection, err := deploymentFailure(msgHAUninstall, 409)
		if err != nil {
			return nil, err
		}
		return deploymentObject(rejection)
	}
	return operation()
}

// GetDeploymentModel dispatches the HA owner or reconciles a standalone task.
func (m *Manager) GetDeploymentModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hook := currentHooks().HADeployment; hook != nil {
		result, err := hook(ctx, m, agentID, id)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		var task model.Deployment
		if err := json.Unmarshal(raw, &task); err != nil {
			return nil, err
		}
		return &task, nil
	}
	return m.defaultHADeploymentModel(ctx, agentID, id)
}

// ActivateDeploymentModel dispatches the HA owner before activating a candidate.
func (m *Manager) ActivateDeploymentModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if hook := currentHooks().HAActivate; hook != nil {
		result, err := hook(ctx, m, agentID, id)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		var task model.Deployment
		if err := json.Unmarshal(raw, &task); err != nil {
			return nil, err
		}
		return &task, nil
	}
	return m.defaultHAActivateModel(ctx, agentID, id)
}

// HAActive reports whether an authoritative pair journal is unfinished.
func HAActive(a *Record) bool { return haActive(a) }

// ErrHAOwnerMissing refuses active HA operations without their registered owner.
var ErrHAOwnerMissing = errors.New("agents: DNS HA owner is not wired")
