package agents

import (
	"configflow/internal/jsonx"
	"configflow/internal/lifecycle"
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
	value, err := jsonx.Parse(prepared)
	if err != nil {
		return err
	}
	proof, ok := value.(*jsonx.Object)
	if !ok || proof.GetString("profile_id") == "" || proof.GetString("service_type") == "" {
		return &ValueError{Message: "Deployment ownership changed during preparation"}
	}
	var validation any
	if len(receipt) > 0 {
		validation, err = jsonx.Parse(receipt)
		if err != nil {
			return err
		}
	}
	_, err = m.UpdateAgents(ctx, "", func(ctx context.Context, list *[]any) (any, error) {
		a, _ := findAgent(*list, agentID)
		state := taskOf(a, deploymentID)
		if a == nil || state == nil {
			return nil, &ValueError{Message: "Deployment ownership changed during preparation"}
		}
		profile, e := m.profileOf(ctx, a)
		if e != nil {
			return nil, e
		}
		if state.GetString("profile_id") != proof.GetString("profile_id") || profile != proof.GetString("profile_id") || a.GetString("service_type") != proof.GetString("service_type") {
			return nil, &ValueError{Message: "Deployment ownership changed during preparation"}
		}
		// Another poll can finish an interrupted preparation. Never revive it.
		if state.GetBool("submitted") || state.GetString("status") != "preparing" {
			return nil, &ValueError{Message: "Deployment ownership changed during preparation"}
		}
		state.Set("prepared_content", proof.DeepCopy())
		if len(receipt) > 0 {
			state.Set("oxidns_validation_receipt", jsonx.DeepCopy(validation))
		}
		return nil, nil
	})
	return err
}
