package monitor

import (
	"context"
	"errors"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
)

var publicationLogger = logx.Get("backend.agents.oxidns_publication_stats")
var expectedRevisionRE = hex64

func publicationHandled(err error) bool {
	var monitor *Error
	var state *StateError
	return errors.As(err, &monitor) || errors.As(err, &state) || errors.Is(err, ErrStateConflict) || errors.Is(err, ErrPublicationPending)
}

// CheckpointStatistics captures a best-effort boundary before activation.
// Monitoring failure never blocks publication; shutdown cancellation still propagates.
func CheckpointStatistics(ctx context.Context, m Registry, id, deploymentID, expectedRevision string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	a, err := m.ReadAgent(ctx, id, true)
	if err != nil {
		return err
	}
	if a == nil || a.ServiceType != "oxidns" {
		return nil
	}
	err = checkpointStatistics(ctx, m, id, deploymentID, expectedRevision)
	if err != nil && publicationHandled(err) {
		publicationLogger.Warning(msgCheckpointUnavailable)
		return nil
	}
	return err
}
func checkpointStatistics(ctx context.Context, m Registry, id, deploymentID, expectedRevision string) error {
	a, err := readDNSAgent(ctx, m, id, MinDashboardAgentVersion, true)
	if err != nil {
		return err
	}
	if !expectedRevisionRE.MatchString(expectedRevision) {
		return nil
	}
	store := NewStateStore(m.DataDir())
	previous, err := store.ReadState(ctx, id)
	if err != nil {
		return err
	}
	if previous == nil {
		return nil
	}
	if previous.Pending != nil && previous.Pending.DeploymentID == deploymentID {
		status, err := storedDeploymentStatus(a, deploymentID)
		if err != nil {
			return err
		}
		if status != "ready" {
			return nil
		}
	}
	snapshot, err := readOverviewSnapshot(ctx, m, a, true)
	if err != nil {
		return err
	}
	digest, err := TargetIdentity(a)
	if err != nil {
		return err
	}
	if err := verifyNativeTarget(ctx, m, id, digest); err != nil {
		return err
	}
	state, err := AdvanceState(previous, &snapshot.Identity, &snapshot.Core, snapshot.Observations, digest, a, false)
	if err != nil {
		return err
	}
	if snapshot.Identity.CollectedAtMS < state.StartedAtMS {
		return ErrPublicationPending
	}
	if state.Pending != nil && state.Pending.DeploymentID != deploymentID {
		return ErrPublicationPending
	}
	totals, partial, err := StateTotals(state, snapshot.Observations)
	if err != nil {
		return err
	}
	pending := &PendingPublication{DeploymentID: deploymentID, ExpectedRevision: expectedRevision, CollectedAtMS: snapshot.Identity.CollectedAtMS, Totals: totals, Partial: partial}
	task, err := statePublication(a, pending)
	if err != nil {
		return err
	}
	if task == nil {
		return nil
	}
	next := *state
	next.Schema = 2
	next.Generation = tokenHex()
	next.Pending = pending
	if next.Carried == nil {
		next.Carried = observationsOf([3][]metricSeries{})
	}
	return compareDashboardState(ctx, m, store, id, previous.Generation, &next, func(held context.Context) error { return verifyNativePublication(held, m, id, digest, pending) })
}
func storedDeploymentStatus(a *model.Agent, id string) (string, error) {
	task, _ := a.Deployments.Get(id)
	return task.Status, nil
}
func verifyNativePublication(ctx context.Context, m Registry, id, digest string, pending *PendingPublication) error {
	if err := verifyNativeTarget(ctx, m, id, digest); err != nil {
		return err
	}
	a, err := m.ReadAgent(ctx, id, true)
	if err != nil {
		return err
	}
	task, err := statePublication(a, pending)
	if err != nil {
		return err
	}
	if task == nil {
		return ErrStateConflict
	}
	status := task.Status
	if status != "uploading" && status != "ready" {
		return ErrStateConflict
	}
	return nil
}

// CompleteStatistics reconciles a confirmed terminal publication with its checkpoint.
func CompleteStatistics(ctx context.Context, m Registry, id, deploymentID, status string) error {
	ctx = lifecycle.Detach(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}
	if !terminal[status] {
		return nil
	}
	a, err := m.ReadAgent(ctx, id, true)
	if err != nil {
		return err
	}
	if a == nil || a.ServiceType != "oxidns" {
		return nil
	}
	err = completeStatistics(ctx, m, a, id, deploymentID)
	if err != nil && publicationHandled(err) {
		publicationLogger.Warning(msgCompletionUnavailable)
		return nil
	}
	return err
}
func completeStatistics(ctx context.Context, m Registry, a *model.Agent, id, deploymentID string) error {
	store := NewStateStore(m.DataDir())
	previous, err := store.ReadState(ctx, id)
	if err != nil {
		return err
	}
	if previous == nil || previous.Pending == nil || previous.Pending.DeploymentID != deploymentID {
		return nil
	}
	snapshot, err := readOverviewSnapshot(ctx, m, a, true)
	if err != nil {
		return err
	}
	digest, err := TargetIdentity(a)
	if err != nil {
		return err
	}
	state, err := AdvanceState(previous, &snapshot.Identity, &snapshot.Core, snapshot.Observations, digest, a, false)
	if err != nil {
		return err
	}
	var replacement *DashboardState
	if state != previous {
		replacement = state
	}
	return compareDashboardState(ctx, m, store, id, previous.Generation, replacement, func(held context.Context) error { return verifyNativeTarget(held, m, id, digest) })
}
