package installer

import (
	"context"

	"configflow/internal/jsondecode"
	"configflow/internal/jsonx"
	"configflow/internal/model"
)

// IssueEnrollment adapts the remaining legacy HTTP renderer to IssueCapability.
func IssueEnrollment(ctx context.Context, dataDir string, body *jsonx.Object, profileID string) (*Enrollment, error) {
	raw, err := jsonx.Default(body)
	if err != nil {
		return nil, err
	}
	capability, err := IssueCapability(ctx, dataDir, raw, profileID)
	if err != nil {
		return nil, err
	}
	return &Enrollment{URL: capability.URL, Token: capability.Token, ExpiresAt: jsonx.Number(capability.ExpiresAt)}, nil
}

// AgentUpdater is the legacy registry callback used by unmigrated callers.
// Production managers implement AgentEditor and never take this codec path.
type AgentUpdater interface {
	UpdateAgents(context.Context, string, func(context.Context, *[]any) (any, error)) (any, error)
}

type legacyAgentEditor struct{ updater AgentUpdater }

func (e legacyAgentEditor) EditAgents(ctx context.Context, profileID string, edit func(context.Context, *[]model.Agent) error) error {
	_, err := e.updater.UpdateAgents(ctx, profileID, func(tx context.Context, records *[]any) (any, error) {
		raw, err := jsonx.Default(*records)
		if err != nil {
			return nil, err
		}
		var agents []model.Agent
		if err := jsondecode.Unmarshal(raw, &agents); err != nil {
			return nil, err
		}
		if err := edit(tx, &agents); err != nil {
			return nil, err
		}
		raw, err = model.Marshal(agents)
		if err != nil {
			return nil, err
		}
		value, err := jsonx.Parse(raw)
		if err != nil {
			return nil, err
		}
		*records = value.([]any)
		// Legacy test callbacks compare this result as well as the records.
		return nil, nil
	})
	return err
}

// Enroll adapts the legacy HTTP response; all identity checks are typed.
func Enroll(ctx context.Context, dataDir string, manager AgentUpdater, sessionID, token string, present bool) (*jsonx.Object, error) {
	editor, ok := manager.(AgentEditor)
	if !ok {
		editor = legacyAgentEditor{updater: manager}
	}
	result, err := EnrollIdentity(ctx, dataDir, editor, sessionID, token, present)
	if err != nil {
		return nil, err
	}
	return jsonx.Obj("agent_id", result.AgentID, "token", result.Token), nil
}
