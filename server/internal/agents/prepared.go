package agents

import (
	"context"
	"encoding/json"

	"configflow/internal/lifecycle"
	"configflow/internal/model"
)

// RememberPreparedContent stores server-owned evidence after checking task ownership.
// Nil receipt means absent, so it never clears evidence already recorded.
func (m *Manager) RememberPreparedContent(ctx context.Context, agentID, deploymentID string, prepared, receipt json.RawMessage) error {
	ctx = lifecycle.Detach(ctx)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	var proof model.PreparedContent
	issues, err := model.DecodeStored(prepared, &proof, "")
	if err != nil {
		return err
	}
	if proof.ProfileID == "" || proof.ServiceType == "" || hasTypeIssue(issues) {
		return &ValueError{Message: msgPreparationOwnershipChanged}
	}
	var validation *model.OxiDNSValidationReceipt
	if len(receipt) > 0 {
		validation = new(model.OxiDNSValidationReceipt)
		issues, err := model.DecodeStored(receipt, validation, "")
		if err != nil {
			return err
		}
		if hasTypeIssue(issues) {
			return &ValueError{Message: msgInvalidValidationReceipt}
		}
	}
	return m.EditAgents(ctx, "", func(tx context.Context, list *[]model.Agent) error {
		a := registryAgent(*list, agentID)
		if a == nil {
			return &ValueError{Message: msgPreparationOwnershipChanged}
		}
		state, ok := a.Deployments.Get(deploymentID)
		if !ok {
			return &ValueError{Message: msgPreparationOwnershipChanged}
		}
		profile := a.ProfileID
		if profile == "" {
			var err error
			profile, err = m.defaultProfileID(tx)
			if err != nil {
				return err
			}
		}
		if state.ProfileID != proof.ProfileID || profile != proof.ProfileID || a.ServiceType != proof.ServiceType || !preparationUnsubmitted(&state) || state.Status != "preparing" {
			return &ValueError{Message: msgPreparationOwnershipChanged}
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

// hasTypeIssue reports a value of the wrong type in server-built evidence.
func hasTypeIssue(issues []model.FieldIssue) bool {
	for _, issue := range issues {
		if issue.Kind == model.IssueType {
			return true
		}
	}
	return false
}

// preparationUnsubmitted reports a deployment not yet submitted to the Agent.
func preparationUnsubmitted(state *model.Deployment) bool {
	return state.Submitted == nil || !*state.Submitted
}
