package installer

import (
	"context"
	"encoding/json"
)

// IssueEnrollment adapts test inputs to IssueCapability.
func IssueEnrollment(ctx context.Context, dataDir string, body map[string]any, profileID string) (*Enrollment, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	capability, err := IssueCapability(ctx, dataDir, raw, profileID)
	if err != nil {
		return nil, err
	}
	return &Enrollment{URL: capability.URL, Token: capability.Token, ExpiresAt: capability.ExpiresAt}, nil
}

// Enroll adapts test inputs to EnrollIdentity.
func Enroll(ctx context.Context, dataDir string, manager AgentEditor, sessionID, token string, present bool) (map[string]any, error) {
	result, err := EnrollIdentity(ctx, dataDir, manager, sessionID, token, present)
	if err != nil {
		return nil, err
	}
	return map[string]any{"agent_id": result.AgentID, "token": result.Token}, nil
}
