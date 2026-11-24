package monitor

import (
	"context"

	"configflow/internal/agents"
)

type publicationObserver struct{}

func (publicationObserver) PublicationComplete(ctx context.Context, m *agents.Manager, agentID, deploymentID string, status any) error {
	value, _ := status.(string)
	return CompleteStatistics(ctx, TypedFromAgents(m), agentID, deploymentID, value)
}

func (publicationObserver) PublicationCheckpoint(ctx context.Context, m *agents.Manager, agentID, deploymentID string, configSHA256 any) error {
	revision, _ := configSHA256.(string)
	return CheckpointStatistics(ctx, TypedFromAgents(m), agentID, deploymentID, revision)
}

// PublicationObserver returns an agents.PublicationObserver backed by the
// OxiDNS publication statistics owner.
func PublicationObserver() agents.PublicationObserver { return publicationObserver{} }
