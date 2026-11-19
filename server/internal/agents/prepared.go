package agents

import (
	"bytes"
	"configflow/internal/jsondecode"
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
	var identity struct {
		ProfileID   string `json:"profile_id"`
		ServiceType string `json:"service_type"`
	}
	if err := jsondecode.Unmarshal(prepared, &identity); err != nil {
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
		// Stored identities are ownership evidence: an originally numeric or
		// null member must not match through its model text projection.
		profile, profileOK := storedText(a.MemberJSON("profile_id"))
		if !profileOK && !storedAbsent(a.MemberJSON("profile_id")) {
			return &ValueError{Message: "Deployment ownership changed during preparation"}
		}
		if profile == "" {
			var err error
			profile, err = m.defaultProfileID(tx)
			if err != nil {
				return err
			}
		}
		stateProfile, stateOK := storedText(deploymentProfileJSON(&state))
		service, serviceOK := storedText(a.MemberJSON("service_type"))
		if !stateOK || !serviceOK || stateProfile != proof.ProfileID || profile != proof.ProfileID || service != proof.ServiceType || state.Submitted != nil && *state.Submitted || state.Status != "preparing" {
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

// deploymentProfileJSON returns the original stored profile_id of one journal
// entry. Only this preparing entry is encoded, never the deployment history.
func deploymentProfileJSON(state *model.Deployment) (json.RawMessage, bool, error) {
	data, err := model.Marshal(state)
	if err != nil {
		return nil, false, err
	}
	var members struct {
		ProfileID json.RawMessage `json:"profile_id"`
	}
	if err := jsondecode.Unmarshal(data, &members); err != nil {
		return nil, false, err
	}
	return members.ProfileID, members.ProfileID != nil, nil
}
