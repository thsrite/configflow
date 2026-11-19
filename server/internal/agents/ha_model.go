package agents

import (
	"bytes"
	"context"
	"encoding/json"

	"configflow/internal/model"
)

// HAEnabledModel reads the strict boolean from a typed stored record.
func HAEnabledModel(record *model.Agent) bool {
	return record != nil && record.OxiDNSHA != nil && record.OxiDNSHA.Enabled != nil && *record.OxiDNSHA.Enabled
}

// HAActiveModel reports a nonterminal authoritative pair publication.
func HAActiveModel(record *model.Agent) bool {
	if record == nil || record.OxiDNSHAPublication == nil {
		return false
	}
	// An empty journal was never an operation. Unknown nonempty journals remain
	// active and fail closed, rather than relying on zero decoded identities.
	if !record.OxiDNSHAPublication.HasMembers() {
		return false
	}
	switch record.OxiDNSHAPublication.Status {
	case "succeeded", "failed", "rollback_failed":
		return false
	default:
		return true
	}
}

// HAEnablingModel preserves the distinction between an empty enable object and
// a nonempty enabling intent, without walking captured configuration payloads.
func HAEnablingModel(record *model.Agent) bool {
	if !HAActiveModel(record) {
		return false
	}
	job := record.OxiDNSHAPublication
	if job.Enable != nil {
		return job.Enable.HasMembers()
	}
	// A malformed stored intent cannot authorize edits to a reserved pair.
	for _, issue := range model.Issues(job) {
		if issue.Path == "enable" {
			return true
		}
	}
	return false
}

// ReadHAPair returns an owned, reciprocal pair from one live registry view.
func (m *Manager) ReadHAPair(ctx context.Context, id string) ([]model.Agent, error) {
	records, err := m.ReadAgents(ctx, false, nil)
	if err != nil {
		return nil, err
	}
	record := registryAgent(records, id)
	if !HAEnabledModel(record) {
		return nil, nil
	}
	group, peerID, valid := haPairIdentity(record)
	peer := registryAgent(records, peerID)
	otherGroup, otherID, otherValid := haPairIdentity(peer)
	if !valid || !otherValid || !HAEnabledModel(peer) || group != otherGroup || otherID != id || !haProfilesMatch(record, peer) {
		return nil, ErrPairIncomplete
	}
	return []model.Agent{*record, *peer}, nil
}

// Pair identities are ownership evidence, not shared text coercions. Read the
// original JSON token types so numeric R5 projections cannot form a pair.
func haPairIdentity(record *model.Agent) (group, peer string, valid bool) {
	if record == nil {
		return "", "", false
	}
	if record.OxiDNSHA == nil {
		return "", "", false
	}
	group, groupOK := storedText(record.OxiDNSHA.MemberJSON("group_id"))
	peer, peerOK := storedText(record.OxiDNSHA.MemberJSON("peer_id"))
	return group, peer, groupOK && peerOK && group != "" && peer != ""
}

func haProfilesMatch(left, right *model.Agent) bool {
	read := func(record *model.Agent) (*string, bool) {
		raw, present, err := record.MemberJSON("profile_id")
		if err != nil {
			return nil, false
		}
		if !present || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, true
		}
		var profile string
		if json.Unmarshal(raw, &profile) != nil {
			return nil, false
		}
		return &profile, true
	}
	a, aOK := read(left)
	b, bOK := read(right)
	return aOK && bOK && (a == nil && b == nil || a != nil && b != nil && *a == *b)
}

// HASummaryModel selects only public journal members, without visiting captured
// configurations, receipts or private children. Every selected value is owned.
func HASummaryModel(job *model.HAJournal) (*model.HAPublicationSummary, error) {
	var result model.HAPublicationSummary
	if job == nil {
		return &result, nil
	}
	for _, key := range []string{"id", "status", "phase", "message", "owner_agent_id", "owner_deployment_id", "active_agent_id", "members", "updated_at", "warnings", "routers"} {
		raw, present, err := job.MemberJSON(key)
		if err != nil {
			return nil, err
		}
		if present {
			if err := result.SetJSON(key, raw); err != nil {
				return nil, err
			}
		}
	}
	return &result, nil
}

// HAAggregateModel projects authoritative pair status over one owned task.
// The caller and source summary retain independent nested values.
func HAAggregateModel(task *model.Deployment, public *model.HAPublicationSummary) (*model.Deployment, error) {
	if task == nil {
		task = new(model.Deployment)
	}
	if public == nil {
		public = new(model.HAPublicationSummary)
	}
	result, err := copyDeployment(task)
	if err != nil {
		return nil, err
	}
	raw, err := model.Marshal(public)
	if err != nil {
		return nil, err
	}
	edit := editDeployment(result).raw("ha_publication", raw)
	status := ""
	for _, key := range []string{"status", "message"} {
		raw, present, err := public.MemberJSON(key)
		if err != nil {
			return nil, err
		}
		if !present {
			raw = json.RawMessage(`null`)
		}
		edit.raw(key, raw)
		if key == "status" {
			status, _ = storedText(raw, present, nil)
		}
	}
	return edit.flag("success", status != "failed" && status != "rollback_failed").remove("http_status", "pending").result()
}

func haOperationConflict(a *model.Agent) (*model.Deployment, error) {
	if !HAActiveModel(a) {
		return nil, nil
	}
	result, err := deploymentFailure(msgHAConflict, 409)
	if err != nil {
		return nil, err
	}
	result.HAPublication, err = HASummaryModel(a.OxiDNSHAPublication)
	return result, err
}
func ownsHAChild(a *model.Agent, id string) bool {
	if a == nil || a.OxiDNSHAPublication == nil {
		return false
	}
	child, valid := storedText(a.OxiDNSHAPublication.Children.MemberJSON(a.ID))
	return valid && child == id
}

func (m *Manager) defaultHADeploymentModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if ownsHAChild(a, id) {
		job := a.OxiDNSHAPublication
		switch job.Status {
		case "succeeded", "failed", "ready":
			record := deploymentOf(a, id)
			if record == nil {
				record = &model.Deployment{DeploymentID: id}
			}
			summary, err := HASummaryModel(job)
			if err != nil {
				return nil, err
			}
			return HAAggregateModel(record, summary)
		default:
			return nil, ErrHAOwnerMissing
		}
	}
	record, err := m.ReadDeploymentModel(ctx, agentID, id)
	if err != nil {
		return nil, err
	}
	if record.HAPublication != nil && record.HAPublication.HasMembers() {
		return HAAggregateModel(record, record.HAPublication)
	}
	return record, nil
}
func (m *Manager) defaultHAActivateModel(ctx context.Context, agentID, id string) (*model.Deployment, error) {
	a, err := m.ReadAgent(ctx, agentID, true)
	if err != nil {
		return nil, err
	}
	if ownsHAChild(a, id) {
		return nil, ErrHAOwnerMissing
	}
	if rejection, err := haOperationConflict(a); rejection != nil || err != nil {
		return rejection, err
	}
	return m.ActivateCandidateModel(ctx, agentID, id)
}
