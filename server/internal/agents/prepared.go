package agents

import (
	"configflow/internal/lifecycle"
	"configflow/internal/model"
	"context"
	"encoding/json"
)

// RememberPreparedContent stores server-owned evidence after checking task ownership.
// Nil receipt means absent, so it never clears evidence already recorded.
func (m *Manager) RememberPreparedContent(ctx context.Context, agentID, deploymentID string, prepared, receipt json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var identity struct {
		ProfileID   string `json:"profile_id"`
		ServiceType string `json:"service_type"`
	}
	if err := json.Unmarshal(prepared, &identity); err != nil {
		return err
	}
	var proof model.PreparedContent
	if err := json.Unmarshal(prepared, &proof); err != nil {
		return err
	}
	if identity.ProfileID == "" || identity.ServiceType == "" || len(model.Issues(&proof)) != 0 {
		return &ValueError{Message: "Deployment ownership changed during preparation"}
	}
	var validation *model.OxiDNSValidationReceipt
	if len(receipt) > 0 {
		validation = new(model.OxiDNSValidationReceipt)
		if err := json.Unmarshal(receipt, validation); err != nil {
			return err
		}
		if len(model.Issues(validation)) != 0 {
			return &ValueError{Message: "Invalid deployment validation receipt"}
		}
	}
	return m.EditAgents(ctx, "", func(tx context.Context, list *[]model.Agent) error {
		a := registryAgent(*list, agentID)
		if a == nil {
			return &ValueError{Message: "Deployment ownership changed during preparation"}
		}
		state, ok := a.Deployments.Get(deploymentID)
		if !ok {
			return &ValueError{Message: "Deployment ownership changed during preparation"}
		}
		profile := a.ProfileID
		if profile == "" {
			var err error
			profile, err = m.defaultProfileID(tx)
			if err != nil {
				return err
			}
		}
		if state.ProfileID != proof.ProfileID || profile != proof.ProfileID || a.ServiceType != proof.ServiceType || state.Submitted != nil && *state.Submitted || state.Status != "preparing" {
			return &ValueError{Message: "Deployment ownership changed during preparation"}
		}
		if err := tx.Err(); err != nil {
			return err
		}
		state.PreparedContent = &proof
		if validation != nil {
			state.OxiDNSValidationReceipt = validation
		}
		a.Deployments.Set(deploymentID, state)
		return tx.Err()
	})
}
