package monitor

// checkpoint and complete of backend/agents/oxidns_publication_stats.py
// (A07): event-driven dashboard checkpoints, stored only on the ConfigFlow
// server. No polling thread and no Agent changes: capture immediately
// before submitting an activation, then reconcile its confirmed result or
// the next overview GET. Registered as the agents hooks
// PublicationCheckpoint / PublicationComplete by Wire.

import (
	"context"
	"errors"
	"regexp"

	"configflow/internal/agents"
	"configflow/internal/jsonx"
	"configflow/internal/logx"
)

var publicationLogger = logx.Get("backend.agents.oxidns_publication_stats")

var expectedRevisionRE = regexp.MustCompile(`^[0-9a-f]{64}$`)

// publicationHandled reports the exceptions checkpoint/complete catch and
// log (MonitorError, DashboardStateError, DashboardStateConflict,
// PublicationPending); anything else propagates.
func publicationHandled(err error) bool {
	var monitorErr *Error
	var stateErr *StateError
	return errors.As(err, &monitorErr) || errors.As(err, &stateErr) || errors.Is(err, ErrStateConflict) ||
		errors.Is(err, ErrPublicationPending)
}

// deploymentStatus is (agent.get('deployments') or {}).get(ident, {}).get('status').
func deploymentStatus(agent *jsonx.Object, ident string) (any, error) {
	deployments, err := orDict(agent.Value("deployments"))
	if err != nil {
		return nil, err
	}
	entry, present := deployments.Get(ident)
	if !present {
		return nil, nil
	}
	task, ok := entry.(*jsonx.Object)
	if !ok || task == nil {
		return nil, attributeError(entry)
	}
	return task.Value("status"), nil
}

// Checkpoint is checkpoint(manager, agent_id, deployment_id,
// expected_revision): a best-effort snapshot before an activation;
// monitoring failure never blocks publication.
func Checkpoint(ctx context.Context, m Manager, agentID, deploymentID string, expectedRevision any) error {
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return err
	}
	if agent == nil || agent.Len() == 0 || !pyEqualStr(agent.Value("service_type"), "oxidns") {
		return nil
	}
	err = checkpoint(ctx, m, agentID, deploymentID, expectedRevision)
	if err != nil && publicationHandled(err) {
		// Never include upstream exceptions: they can contain credentials.
		publicationLogger.Warning("OxiDNS 发布前统计快照不可用，本次发布的统计可能重新开始")
		return nil
	}
	return err
}

func checkpoint(ctx context.Context, m Manager, agentID, deploymentID string, expectedRevision any) error {
	// The checkpoint is matched against this device's publication records.
	agent, err := agentFor(ctx, m, agentID, MinDashboardAgentVersion, true)
	if err != nil {
		return err
	}
	if !strMatches(expectedRevision, expectedRevisionRE) {
		return nil
	}
	store := NewStateStore(m.DataDir())
	previous, err := store.Read(agentID)
	if err != nil {
		return err
	}
	// No dashboard period has been started for this device, so there is
	// nothing to preserve. Do not enable monitoring as a side effect of push.
	if previous == nil {
		return nil
	}
	if pending, _ := previous.Value("pending").(*jsonx.Object); pending != nil && pending.Len() > 0 &&
		pyEqualStr(pending.Value("deployment_id"), deploymentID) {
		status, err := deploymentStatus(agent, deploymentID)
		if err != nil {
			return err
		}
		if !pyEqualStr(status, "ready") {
			return nil
		}
	}
	snap, err := overviewSnapshot(ctx, m, agent, true)
	if err != nil {
		return err
	}
	digest, err := target(agent)
	if err != nil {
		return err
	}
	if err := verifyTarget(ctx, m, agentID, digest); err != nil {
		return err
	}
	snapshot := snap.data
	state, err := Advance(previous, snapshot, snap.core, snap.observations, digest, agent, false)
	if err != nil {
		return err
	}
	if collectedLess(snapshot.Value("collected_at_ms"), state.Value("started_at_ms")) {
		return ErrPublicationPending
	}
	if pending, _ := state.Value("pending").(*jsonx.Object); pending != nil && pending.Len() > 0 &&
		!pyEqualStr(pending.Value("deployment_id"), deploymentID) {
		return ErrPublicationPending
	}
	adjusted, partial, err := Totals(state, snap.observations)
	if err != nil {
		return err
	}
	pending := jsonx.Obj("deployment_id", deploymentID, "expected_revision", expectedRevision,
		"collected_at_ms", snapshot.Value("collected_at_ms"), "totals", adjusted, "partial", partial)
	task, err := publicationTask(agent, pending)
	if err != nil {
		return err
	}
	if task.Len() == 0 {
		return nil
	}
	next := state.Clone()
	next.Set("schema", 2)
	next.Set("generation", tokenHex())
	next.Set("pending", pending)
	if _, ok := state.Get("carried"); !ok {
		next.Set("carried", jsonx.Obj("servers", []any{}, "caches", []any{}, "upstreams", []any{}))
	}
	if _, ok := state.Get("carried_partial"); !ok {
		next.Set("carried_partial", false)
	}
	if _, ok := state.Get("publication_count"); !ok {
		next.Set("publication_count", 0)
	}
	verify := func() error {
		if err := verifyTarget(ctx, m, agentID, digest); err != nil {
			return err
		}
		current, err := m.GetAgentByID(ctx, agentID, true)
		if err != nil {
			return err
		}
		task, err := publicationTask(current, pending)
		if err != nil {
			return err
		}
		if status := task.Value("status"); !pyEqualStr(status, "uploading") && !pyEqualStr(status, "ready") {
			return ErrStateConflict
		}
		return nil
	}
	expected, _ := previous.Value("generation").(string)
	_, err = store.CompareAndSet(agentID, expected, next, verify)
	return err
}

// Complete is complete(manager, agent_id, deployment_id, status):
// reconcile once a transaction settles; retries are idempotent after the
// compare-and-set.
func Complete(ctx context.Context, m Manager, agentID, deploymentID string, status any) error {
	if unhashable(status) {
		return typeErrorUnhashable(status)
	}
	if text, ok := status.(string); !ok || !terminal[text] {
		return nil
	}
	agent, err := m.GetAgentByID(ctx, agentID, true)
	if err != nil {
		return err
	}
	if agent == nil || agent.Len() == 0 || !pyEqualStr(agent.Value("service_type"), "oxidns") {
		return nil
	}
	err = complete(ctx, m, agent, agentID, deploymentID)
	if err != nil && publicationHandled(err) {
		publicationLogger.Warning("OxiDNS 发布后统计暂未接续，将在下次查看时重试")
		return nil
	}
	return err
}

func complete(ctx context.Context, m Manager, agent *jsonx.Object, agentID, deploymentID string) error {
	store := NewStateStore(m.DataDir())
	previous, err := store.Read(agentID)
	if err != nil {
		return err
	}
	var pending *jsonx.Object
	if previous != nil {
		pending, _ = previous.Value("pending").(*jsonx.Object)
	}
	if pending == nil || pending.Len() == 0 || !pyEqualStr(pending.Value("deployment_id"), deploymentID) {
		return nil
	}
	snap, err := overviewSnapshot(ctx, m, agent, true)
	if err != nil {
		return err
	}
	digest, err := target(agent)
	if err != nil {
		return err
	}
	state, err := Advance(previous, snap.data, snap.core, snap.observations, digest, agent, false)
	if err != nil {
		return err
	}
	var replacement *jsonx.Object
	if !jsonx.Equal(state, previous) {
		replacement = state
	}
	expected, _ := previous.Value("generation").(string)
	_, err = store.CompareAndSet(agentID, expected, replacement, func() error {
		return verifyTarget(ctx, m, agentID, digest)
	})
	return err
}

// The agents hooks (registered by Wire).
func checkpointHook(ctx context.Context, m *agents.Manager, agentID, deploymentID string, configSHA256 any) error {
	return Checkpoint(ctx, FromAgents(m), agentID, deploymentID, configSHA256)
}

func completeHook(ctx context.Context, m *agents.Manager, agentID, deploymentID string, status any) error {
	return Complete(ctx, FromAgents(m), agentID, deploymentID, status)
}
