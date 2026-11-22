package agents

import (
	"context"

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

// HAEnablingModel reports an active pair journal that carries an enabling
// intent, without walking captured configuration payloads.
func HAEnablingModel(record *model.Agent) bool {
	if !HAActiveModel(record) {
		return false
	}
	job := record.OxiDNSHAPublication
	// Any stored enable object is an enabling intent: it locks the pair.
	return job.Enable != nil
}

// ReadHAPair returns a borrowed (see "Borrowed records"), reciprocal pair from
// one live registry generation, without histories. The pair checks read stored
// members that the live heartbeat overlay never changes.
func (m *Manager) ReadHAPair(ctx context.Context, id string) ([]model.Agent, error) {
	records, shared, err := m.repository.BorrowAgents(ctx)
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
	pair := make([]model.Agent, 0, 2)
	for _, source := range []*model.Agent{record, peer} {
		member, err := readerAgent(source, false, shared)
		if err != nil {
			return nil, err
		}
		pair = append(pair, member)
	}
	return m.runtime.overlayModels(pair), nil
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
	group, peer = record.OxiDNSHA.GroupID, record.OxiDNSHA.PeerID
	return group, peer, group != "" && peer != ""
}

func haProfilesMatch(left, right *model.Agent) bool {
	return left.ProfileID == right.ProfileID
}

// HASummaryModel selects only public journal members, without visiting captured
// configurations, receipts or private children. Every selected value is owned.
func HASummaryModel(job *model.HAJournal) (*model.HAPublicationSummary, error) {
	var result model.HAPublicationSummary
	if job == nil {
		return &result, nil
	}
	selected := model.HAPublicationSummary{
		ID: job.ID, Status: job.Status, Phase: job.Phase, Message: job.Message, OwnerAgentID: job.OwnerAgentID,
		OwnerDeploymentID: job.OwnerDeploymentID, ActiveAgentID: job.ActiveAgentID, Members: job.Members,
		UpdatedAt: job.UpdatedAt, Warnings: job.Warnings, Routers: job.Routers, AutoComplete: job.AutoComplete,
	}
	owned, err := model.Clone(selected)
	if err != nil {
		return nil, err
	}
	result = owned
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
	return haAggregate(result, public)
}

// HAAggregateView is HAAggregateModel for display: the result is a shallow
// copy of task (its edits are copy-on-write), sharing every other nested value
// with task and public, so neither may be changed in depth afterwards.
func HAAggregateView(task *model.Deployment, public *model.HAPublicationSummary) (*model.Deployment, error) {
	if task == nil {
		task = new(model.Deployment)
	}
	if public == nil {
		public = new(model.HAPublicationSummary)
	}
	copied := *task
	return haAggregate(&copied, public)
}

func haAggregate(result *model.Deployment, public *model.HAPublicationSummary) (*model.Deployment, error) {
	summary, err := model.Clone(public)
	if err != nil {
		return nil, err
	}
	result.HAPublication = summary
	result.Status = public.Status
	result.Message = new(public.Message)
	return editDeployment(result).flag("success", public.Status != "failed" && public.Status != "rollback_failed").remove("http_status", "pending").result()
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
	child, ok := a.OxiDNSHAPublication.Children.Get(a.ID)
	return ok && child == id
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

// HAProfilesMatchModel compares optional original string bindings for a pair.
func HAProfilesMatchModel(left, right *model.Agent) bool {
	return left != nil && right != nil && haProfilesMatch(left, right)
}

// HAPairMatchesModel verifies reciprocal original string pair identities.
func HAPairMatchesModel(left, right *model.Agent) bool {
	if left == nil || right == nil || !AgentMatchesID(left, left.ID) || !AgentMatchesID(right, right.ID) || !HAEnabledModel(left) || !HAEnabledModel(right) {
		return false
	}
	group, peer, ok := haPairIdentity(left)
	otherGroup, otherPeer, otherOK := haPairIdentity(right)
	return ok && otherOK && peer == right.ID && otherPeer == left.ID && group == otherGroup && haProfilesMatch(left, right)
}
