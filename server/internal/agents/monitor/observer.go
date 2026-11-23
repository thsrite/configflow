package monitor

import (
	"context"

	"configflow/internal/agents"
)

type publicationObserver struct{}

func (publicationObserver) PublicationComplete(ctx context.Context, m *agents.Manager, agentID, deploymentID string, status any) error {
	return completeHook(ctx, m, agentID, deploymentID, status)
}

func (publicationObserver) PublicationCheckpoint(ctx context.Context, m *agents.Manager, agentID, deploymentID string, configSHA256 any) error {
	return checkpointHook(ctx, m, agentID, deploymentID, configSHA256)
}

// PublicationObserver returns an agents.PublicationObserver backed by the
// OxiDNS publication statistics owner.
func PublicationObserver() agents.PublicationObserver { return publicationObserver{} }
