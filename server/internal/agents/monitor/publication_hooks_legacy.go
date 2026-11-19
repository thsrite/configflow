package monitor

import (
	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"context"
	"errors"
)

// Checkpoint is the legacy hook codec; invalid optional revisions are ignored.
func Checkpoint(ctx context.Context, m Manager, id, deploymentID string, value any) error {
	revision, _ := value.(string)
	return CheckpointStatistics(ctx, registryFor(m), id, deploymentID, revision)
}

// Complete is the legacy terminal-status codec.
func Complete(ctx context.Context, m Manager, id, deploymentID string, value any) error {
	switch value.(type) {
	case []any, *jsonx.Object:
		return errors.New(msgPublicationDamaged)
	}
	status, _ := value.(string)
	return CompleteStatistics(ctx, registryFor(m), id, deploymentID, status)
}
func checkpointHook(ctx context.Context, m *agents.Manager, id, deploymentID string, value any) error {
	revision, _ := value.(string)
	return CheckpointStatistics(ctx, TypedFromAgents(m), id, deploymentID, revision)
}
func completeHook(ctx context.Context, m *agents.Manager, id, deploymentID string, value any) error {
	status, _ := value.(string)
	return CompleteStatistics(ctx, TypedFromAgents(m), id, deploymentID, status)
}

// verifyPublication is the legacy read-snapshot codec used by boundary tests.
func verifyPublication(ctx context.Context, m Manager, id, digest string, raw *jsonx.Object) error {
	var pending PendingPublication
	if err := metricJSON(raw, &pending); err != nil {
		return err
	}
	return verifyNativePublication(ctx, registryFor(m), id, digest, &pending)
}
