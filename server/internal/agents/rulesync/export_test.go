package rulesync

import (
	"configflow/internal/agents"
	"configflow/internal/deploy"
	"configflow/internal/model"
	"configflow/internal/redact"
	"context"
)

// Presence uses the shared transition owner, with only its log-facing fields
// projected and sanitized. No metrics/history or deployment graph is copied.
func observePresence(ctx context.Context, m *agents.Manager, records []*model.Agent) {
	secrets, err := systemSecrets(ctx, m)
	if err != nil {
		hidden := make([]agents.PresenceObservation, 0, len(records))
		for _, a := range records {
			if !agents.AgentMatchesID(a, a.ID) {
				continue
			}
			hidden = append(hidden, agents.PresenceObservation{ID: a.ID, Status: a.Status, Name: redact.Redacted, LastHeartbeat: redact.Redacted})
		}
		agents.ObserveAgentPresence(hidden, nil)
		return
	}
	for _, a := range records {
		secrets = agents.CollectAgentCredentials(a, secrets)
	}
	agents.ObserveRegistryPresence(records, secrets)
}

// PrepareCandidate generates only opted-in rule artifacts; all other delivered
// files and the main document's non-rule fields come from the verified device.
func PrepareCandidate(ctx context.Context, target deploy.Target, source *deploy.Source, baseline Baseline, settings Settings, deploymentID string, forceRefresh bool) (bundle *deploy.Bundle, err error) {
	return PrepareCandidateWith(ctx, nil, target, source, baseline, settings, deploymentID, forceRefresh)
}
