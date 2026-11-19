package agents

import (
	"bytes"
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
	if err := json.Unmarshal(prepared, &proof); err != nil {
		return err
	}
	profileID, profileOK := storedText(proof.MemberJSON("profile_id"))
	serviceType, serviceOK := storedText(proof.MemberJSON("service_type"))
	if !profileOK || !serviceOK || profileID == "" || serviceType == "" || len(model.Issues(&proof)) != 0 {
		return &ValueError{Message: msgPreparationOwnershipChanged}
	}
	var validation *model.OxiDNSValidationReceipt
	if len(receipt) > 0 {
		validation = new(model.OxiDNSValidationReceipt)
		if err := json.Unmarshal(receipt, validation); err != nil {
			return err
		}
		if len(model.Issues(validation)) != 0 {
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
		// Stored identities are ownership evidence: an originally numeric or
		// null member must not match through its model text projection.
		profile, profileOK := storedText(a.MemberJSON("profile_id"))
		if !profileOK && !storedAbsent(a.MemberJSON("profile_id")) {
			return &ValueError{Message: msgPreparationOwnershipChanged}
		}
		if profile == "" {
			var err error
			profile, err = m.defaultProfileID(tx)
			if err != nil {
				return err
			}
		}
		stateProfile, stateOK := storedText(state.MemberJSON("profile_id"))
		service, serviceOK := storedText(a.MemberJSON("service_type"))
		status, statusOK := storedText(state.MemberJSON("status"))
		if !stateOK || !serviceOK || !statusOK || stateProfile != proof.ProfileID || profile != proof.ProfileID || service != proof.ServiceType || !preparationUnsubmitted(&state) || status != "preparing" {
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

// storedText accepts only an original JSON string member.
func storedText(raw json.RawMessage, present bool, err error) (string, bool) {
	raw = bytes.TrimSpace(raw)
	if err != nil || !present || len(raw) == 0 || raw[0] != '"' {
		return "", false
	}
	var text string
	return text, json.Unmarshal(raw, &text) == nil
}

// storedAbsent reports a missing or explicit null member.
func storedAbsent(raw json.RawMessage, present bool, err error) bool {
	return err == nil && (!present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")))
}

// preparationUnsubmitted accepts only absent/null or an original JSON false.
// A wrong-shaped R5 zero cannot authorize replacing server-owned evidence.
func preparationUnsubmitted(state *model.Deployment) bool {
	raw, present, err := state.MemberJSON("submitted")
	if err != nil {
		return false
	}
	if storedAbsent(raw, present, nil) {
		return true
	}
	var submitted *bool
	return json.Unmarshal(raw, &submitted) == nil && submitted != nil && !*submitted
}
