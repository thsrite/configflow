package monitor

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

var terminal = map[string]bool{"succeeded": true, "failed": true, "rolled_back": true, "rollback_failed": true}

// ErrPublicationPending keeps an unresolved publication checkpoint intact.
var ErrPublicationPending = errors.New("PublicationPending")

var tokenHex = func() string { b := make([]byte, 16); _, _ = rand.Read(b); return hex.EncodeToString(b) }

// SnapshotIdentity pins a metrics/rankings snapshot to one runtime/configuration.
type SnapshotIdentity struct {
	CollectedAtMS   int64  `json:"collected_at_ms"`
	SourceRevision  string `json:"source_revision"`
	RuntimeRevision string `json:"runtime_revision"`
}

// CoreStatus contains only the core fields exposed in the monitoring dashboard.
type CoreStatus struct {
	Version    string        `json:"version"`
	Status     string        `json:"status"`
	UptimeMS   *MetricNumber `json:"uptime_ms"`
	InstanceID string        `json:"instance_id"`
}

func newDashboardState(snapshot *SnapshotIdentity, core *CoreStatus, observations *Observations, target, reason string) *DashboardState {
	return &DashboardState{Schema: 1, Generation: tokenHex(), StartedAtMS: snapshot.CollectedAtMS, Reason: reason, Target: target,
		InstanceID: core.InstanceID, SourceRevision: snapshot.SourceRevision, RuntimeRevision: snapshot.RuntimeRevision, Baseline: observations}
}

func storedObject(raw json.RawMessage, present bool, err error) error {
	if err != nil {
		return err
	}
	if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var fields model.Fields
	if jsondecode.Unmarshal(raw, &fields) != nil {
		return &StateError{msgPublicationDamaged}
	}
	return nil
}
func statePublication(agent *model.Agent, pending *PendingPublication) (*model.Deployment, error) {
	if agent == nil {
		return nil, nil
	}
	if err := storedObject(agent.MemberJSON("deployments")); err != nil {
		return nil, err
	}
	task, exists := agent.Deployments.Get(pending.DeploymentID)
	if exists && !agent.Deployments.EntryValid(pending.DeploymentID) {
		return nil, &StateError{msgPublicationDamaged}
	}
	if err := storedObject(agent.MemberJSON("latest_deployment")); err != nil {
		return nil, err
	}
	latest := agent.LatestDeployment
	if !exists || latest == nil {
		return nil, nil
	}
	profile, present := originalString(agent.MemberJSON("profile_id"))
	raw, stored, err := agent.MemberJSON("profile_id")
	if err != nil {
		return nil, err
	}
	if !stored || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) || present && profile == "" {
		profile = "default"
	} else if !present {
		return nil, &StateError{msgPublicationDamaged}
	}
	for _, member := range []struct {
		record *model.Deployment
		name   string
	}{{latest, "deployment_id"}, {&task, "profile_id"}, {&task, "service_type"}} {
		raw, present, err := member.record.MemberJSON(member.name)
		if err != nil {
			return nil, err
		}
		if present {
			if _, valid := originalString(raw, present, nil); !valid {
				return nil, &StateError{msgPublicationDamaged}
			}
		}
	}
	latestID := originalText(latest.MemberJSON("deployment_id"))
	status := originalText(task.MemberJSON("status"))
	latestStatus := originalText(latest.MemberJSON("status"))
	superseded := latestID != pending.DeploymentID && !((status == "succeeded" || status == "rolled_back") && (latestStatus == "preparing" || latestStatus == "uploading" || latestStatus == "ready"))
	if superseded || originalText(task.MemberJSON("service_type")) != "oxidns" || originalText(task.MemberJSON("profile_id")) != profile {
		return nil, nil
	}
	return &task, nil
}

// AdvanceState computes one immutable statistics-boundary transition. Its
// caller holds the repository snapshot through the state-file generation CAS.
func AdvanceState(previous *DashboardState, snapshot *SnapshotIdentity, core *CoreStatus, observations *Observations, target string, agent *model.Agent, manual bool) (*DashboardState, error) {
	reason := ""
	switch {
	case manual:
		reason = "manual"
	case previous == nil:
		reason = "initial"
	case previous.Target != target:
		reason = "target_changed"
	case previous.InstanceID != core.InstanceID:
		reason = "core_changed"
	case previous.SourceRevision != snapshot.SourceRevision:
		reason = "configuration_changed"
	case previous.RuntimeRevision != snapshot.RuntimeRevision:
		reason = "core_changed"
	case CountersRestarted(observations, previous.Baseline):
		reason = "counters_changed"
	}
	if previous != nil && previous.Pending != nil && !manual && previous.Target == target {
		pending := previous.Pending
		task, err := statePublication(agent, pending)
		if err != nil {
			return nil, err
		}
		if snapshot.CollectedAtMS < pending.CollectedAtMS {
			return nil, ErrPublicationPending
		}
		status := ""
		if task != nil {
			raw, present, err := task.MemberJSON("status")
			if err != nil {
				return nil, err
			}
			if present && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				var valid bool
				status, valid = originalString(raw, present, nil)
				if !valid {
					return nil, &StateError{msgPublicationDamaged}
				}
			}
		}
		if status != "" && !terminal[status] {
			if reason != "" {
				return nil, ErrPublicationPending
			}
			return previous, nil
		}
		expected := ""
		switch status {
		case "succeeded":
			expected = pending.ExpectedRevision
		case "rolled_back":
			expected = previous.SourceRevision
		}
		if task != nil {
			raw, present, err := task.MemberJSON("manual_rollback")
			if err != nil {
				return nil, err
			}
			if present && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
				var flag *bool
				if jsondecode.Unmarshal(raw, &flag) != nil || flag == nil {
					return nil, &StateError{msgPublicationDamaged}
				}
			}
		}
		manualRollback := task != nil && task.ManualRollback != nil && *task.ManualRollback
		if reason != "" && expected != "" && expected == snapshot.SourceRevision && !manualRollback {
			state := *previous
			state.Generation = tokenHex()
			state.Reason = "publication"
			state.InstanceID = core.InstanceID
			state.SourceRevision = snapshot.SourceRevision
			state.RuntimeRevision = snapshot.RuntimeRevision
			state.Baseline = ZeroObservations(observations)
			state.Carried = pending.Totals
			state.CarriedPartial = pending.Partial
			state.Pending = nil
			state.PublicationCount++
			return &state, nil
		}
		if reason == "" {
			state := *previous
			state.Generation = tokenHex()
			state.Pending = nil
			return &state, nil
		}
	}
	if reason != "" {
		return newDashboardState(snapshot, core, observations, target, reason), nil
	}
	return previous, nil
}

// StateTotals computes the counters in one dashboard period.
func StateTotals(state *DashboardState, observations *Observations) (*Observations, bool, error) {
	adjusted, partial, err := ApplyBaseline(observations, state.Baseline)
	if err != nil {
		return nil, false, err
	}
	if state.Schema == 2 {
		adjusted = CarryObservations(adjusted, state.Carried)
		partial = partial || state.CarriedPartial
	}
	return adjusted, partial, nil
}
