package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// AgentEdits carries only editable registry members, strictly decoded.
type AgentEdits struct {
	record  model.Agent
	members []string
}

// DecodeAgentEdits selects exact protocol keys and ignores all other fields.
// A member of the wrong JSON type is a validation error.
func DecodeAgentEdits(data json.RawMessage) (AgentEdits, error) {
	var edits AgentEdits
	if err := requireJSONObject(data); err != nil {
		return edits, err
	}
	var members model.OrderedMap[json.RawMessage]
	if err := json.Unmarshal(data, &members); err != nil {
		return edits, err
	}
	for _, name := range editableFields {
		value, present := members.Get(name)
		if !present {
			continue
		}
		if name == "profile_id" && (len(bytes.TrimSpace(value)) == 0 || bytes.TrimSpace(value)[0] != '"') {
			return AgentEdits{}, repo.NewValidationError("Invalid profile id")
		}
		if err := model.DecodeInputMember("agent", &edits.record, name, value); err != nil {
			return AgentEdits{}, repo.NewValidationError(fmt.Sprintf(msgMemberType, name))
		}
		edits.members = append(edits.members, name)
	}
	if edits.has("profile_id") {
		if err := repo.ValidateProfileID(edits.record.ProfileID); err != nil {
			return AgentEdits{}, err
		}
	}
	return edits, nil
}

func (e AgentEdits) has(name string) bool { return slices.Contains(e.members, name) }

// changes reports whether the edits change one of the guarded members.
func (e AgentEdits) changes(a *model.Agent) bool {
	for _, name := range guardedFields {
		if !e.has(name) {
			continue
		}
		var same bool
		switch name {
		case "host":
			same = e.record.Host == a.Host
		case "port":
			same = e.record.Port == a.Port
		case "enabled":
			same = sameFlag(e.record.Enabled, a.Enabled)
		case "service_type":
			same = e.record.ServiceType == a.ServiceType
		case "profile_id":
			same = e.record.ProfileID == a.ProfileID
		}
		if !same {
			return true
		}
	}
	return false
}

// apply copies the edited members into a.
func (e AgentEdits) apply(a *model.Agent) {
	for _, name := range e.members {
		switch name {
		case "name":
			a.Name = e.record.Name
		case "host":
			a.Host = e.record.Host
		case "port":
			a.Port = e.record.Port
		case "enabled":
			// An explicit null switch is off (flag-truthiness ruling).
			a.Enabled = e.record.Enabled
			if a.Enabled == nil {
				a.Enabled = new(false)
			}
		case "service_type":
			a.ServiceType = e.record.ServiceType
		case "profile_id":
			a.ProfileID = e.record.ProfileID
		}
	}
}

func sameFlag(a, b *bool) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// EditAgent applies reviewed editable fields in one typed registry transaction.
func (m *Manager) EditAgent(ctx context.Context, id string, edits AgentEdits) (bool, error) {
	updated := false
	err := m.EditAgents(ctx, edits.record.ProfileID, func(tx context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, id)
		if a == nil {
			return nil
		}
		if upgradeBusyModel(a) {
			return &DeploymentConflict{Message: msgAgentEditingUpgrade}
		}
		changed := edits.changes(a)
		if dnsFollowBusyModel(*records, id) && changed {
			return &DeploymentConflict{Message: msgAgentEditingDNSFollow}
		}
		if (HAEnabledModel(a) || HAActiveModel(a)) && changed {
			return &DeploymentConflict{Message: OxidnsHALocked}
		}
		if edits.has("profile_id") && unfinishedDeploymentModel(a) {
			current, err := m.agentProfileID(tx, a)
			if err != nil || current != edits.record.ProfileID {
				return &DeploymentConflict{Message: msgAgentEditingDeployment}
			}
		}
		edits.apply(a)
		a.UpdatedAt = new(nowISO())
		updated = true
		return tx.Err()
	})
	return updated && err == nil, err
}

// agentProfileID uses the default only for an absent or empty binding.
func (m *Manager) agentProfileID(ctx context.Context, a *model.Agent) (string, error) {
	if a.ProfileID == "" {
		return m.defaultProfileID(ctx)
	}
	return a.ProfileID, nil
}

// ResolveAgentProfiles resolves optional bindings with one default lookup for
// the entire registry snapshot. It never reloads the document per Agent.
func (m *Manager) ResolveAgentProfiles(ctx context.Context, rows []model.Agent) ([]string, error) {
	result := make([]string, len(rows))
	var fallback string
	loaded := false
	for i := range rows {
		profile := rows[i].ProfileID
		if profile == "" {
			if !loaded {
				var err error
				fallback, err = m.defaultProfileID(ctx)
				if err != nil {
					return nil, err
				}
				loaded = true
			}
			profile = fallback
		}
		result[i] = profile
	}
	return result, ctx.Err()
}

func deploymentTerminalModel(status string) bool {
	switch status {
	case "succeeded", "failed", "rolled_back":
		return true
	default:
		return false
	}
}

func upgradeBusyModel(a *model.Agent) bool {
	if a == nil {
		return false
	}
	return a.LatestUpgrade.HasMembers() && !deploymentTerminalModel(a.LatestUpgrade.Status)
}

func unfinishedDeploymentModel(a *model.Agent) bool {
	for _, task := range a.Deployments.All() {
		if !deploymentTerminalModel(task.Status) {
			return true
		}
	}
	return false
}

func dnsFollowBusyModel(records []model.Agent, id string) bool {
	for i := range records {
		a := &records[i]
		if !a.DNSFollowState.Busy() {
			continue
		}
		if a.ID == id && agentIDProven(a) {
			return true
		}
		if a.DNSFollow != nil && a.DNSFollow.SourceAgentID != "" && a.DNSFollow.SourceAgentID == id {
			return true
		}
	}
	return false
}

// SetDomainDiscoveryModel changes the management switch and returns live state.
func (m *Manager) SetDomainDiscoveryModel(ctx context.Context, id string, enabled bool) (*model.Agent, error) {
	return m.setAgentSwitch(ctx, id, enabled, false)
}

// SetTrafficHistoryModel changes traffic collection, retaining its original
// start time when collection is already enabled, and returns live state.
func (m *Manager) SetTrafficHistoryModel(ctx context.Context, id string, enabled bool) (*model.Agent, error) {
	return m.setAgentSwitch(ctx, id, enabled, true)
}

func (m *Manager) setAgentSwitch(ctx context.Context, id string, enabled, traffic bool) (*model.Agent, error) {
	ctx = lifecycle.Detach(ctx)
	var result *model.Agent
	err := m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, id)
		if a == nil {
			return nil
		}
		if enabled && a.ServiceType != "mihomo" {
			if traffic {
				return &ValueError{Message: msgTrafficHistoryMihomo}
			}
			return &ValueError{Message: msgDomainDiscoveryMihomo}
		}
		if traffic {
			if enabled && (a.TrafficHistoryEnabled == nil || !*a.TrafficHistoryEnabled) {
				a.TrafficHistorySince = new(utcNowISO())
			}
			a.TrafficHistoryEnabled = new(enabled)
		} else {
			a.DomainDiscoveryEnabled = new(enabled)
		}
		a.UpdatedAt = new(nowISO())
		var err error
		result, err = copyAgent(a, true)
		return err
	})
	if err != nil || result == nil {
		return nil, err
	}
	return &m.runtime.overlayModels([]model.Agent{*result})[0], nil
}

// DeleteAgent removes a registry identity and its volatile/history projections.
func (m *Manager) DeleteAgent(ctx context.Context, id string) (bool, error) {
	ctx = lifecycle.Detach(ctx)
	var deleted *model.Agent
	err := m.EditAgents(ctx, "", func(tx context.Context, records *[]model.Agent) error {
		a := registryAgent(*records, id)
		if upgradeBusyModel(a) {
			return &DeploymentConflict{Message: msgAgentDeletingUpgrade}
		}
		if dnsFollowBusyModel(*records, id) {
			return &DeploymentConflict{Message: msgAgentDeletingDNSFollow}
		}
		if HAEnabledModel(a) || HAActiveModel(a) {
			return &DeploymentConflict{Message: OxidnsHALocked}
		}
		if a == nil {
			return nil
		}
		var err error
		deleted, err = copyAgent(a, true)
		if err != nil {
			return err
		}
		kept := make([]model.Agent, 0, len(*records)-1)
		for i := range *records {
			if (*records)[i].ID != id || !agentIDProven(&(*records)[i]) {
				kept = append(kept, (*records)[i])
			}
		}
		*records = kept
		return tx.Err()
	})
	if err != nil || deleted == nil {
		return false, err
	}
	name := deleted.Name
	if name == "" {
		name = id
	}
	logger.Infof(msgAgentDeleted, registryDiagnostic(deleted, name, 120))
	if err := ctx.Err(); err != nil {
		return true, nil
	}
	if err := m.runtime.deleteContext(ctx, id); err != nil {
		return true, nil
	}
	h := currentHooks()
	if h.DeleteMetricsHistoryContext != nil {
		_ = h.DeleteMetricsHistoryContext(ctx, m, id) // The history owner logs ordinary file failures.
	}
	var cleanupErr error
	if h.DeleteTrafficHistoryContext != nil {
		cleanupErr = h.DeleteTrafficHistoryContext(ctx, m, id)
	}
	if cleanupErr != nil && ctx.Err() == nil {
		logger.Warningf(msgTrafficHistoryDeleteFailure, registryDiagnostic(deleted, id, 120), registryDiagnostic(deleted, cleanupErr.Error(), DetailLimit))
	}
	return true, nil
}

// registryDiagnostic routes credential removal through the common owner before
// truncating log text. It runs only on a log path, never each heartbeat/read.
func registryDiagnostic(a *model.Agent, text string, limit int) string {
	raw, err := model.Marshal(a)
	if err != nil {
		return redact.Redacted
	}
	secrets := redact.CollectCredentials(json.RawMessage(raw), nil)
	value, err := json.Marshal(text)
	if err != nil {
		return redact.Redacted
	}
	var result string
	if json.Unmarshal(redact.SanitizeCredentials(json.RawMessage(value), secrets), &result) != nil {
		return redact.Redacted
	}
	return logx.LogText(result, limit)
}
