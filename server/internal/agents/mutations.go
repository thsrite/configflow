package agents

import (
	"context"
	"encoding/json"

	"configflow/internal/lifecycle"
	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/redact"
	"configflow/internal/repo"
)

// AgentEdits carries only editable registry members. The shared model retains
// explicit nulls and R5 imported values; presence is independent of Go zeroes.
type AgentEdits struct {
	record  model.Agent
	members []string
}

// DecodeAgentEdits selects exact protocol keys and ignores all other fields.
func DecodeAgentEdits(data json.RawMessage) (AgentEdits, error) {
	var members model.Fields
	var edits AgentEdits
	if err := json.Unmarshal(data, &members); err != nil {
		return edits, err
	}
	for _, name := range editableFields {
		if value, present := members.Get(name); present {
			if err := edits.record.SetJSON(name, value); err != nil {
				return AgentEdits{}, err
			}
			edits.members = append(edits.members, name)
		}
	}
	if raw, present, err := edits.record.MemberJSON("profile_id"); err != nil {
		return AgentEdits{}, err
	} else if present {
		profile, valid := storedText(raw, true, nil)
		if !valid {
			return AgentEdits{}, repo.NewValidationError("Invalid profile id")
		}
		if err := repo.ValidateProfileID(profile); err != nil {
			return AgentEdits{}, err
		}
	}
	return edits, nil
}

// EditAgent applies reviewed editable fields in one typed registry transaction.
// Stored identity members are checked from original JSON, not R5 projections.
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
		changed := false
		for _, name := range guardedFields {
			raw, present, err := edits.record.MemberJSON(name)
			if err != nil {
				return err
			}
			if present && !sameAgentMember(a, name, raw) {
				changed = true
			}
		}
		if dnsFollowBusyModel(*records, id) && changed {
			return &DeploymentConflict{Message: msgAgentEditingDNSFollow}
		}
		if (HAEnabledModel(a) || HAActiveModel(a)) && changed {
			return &DeploymentConflict{Message: OxidnsHALocked}
		}
		if _, present, err := edits.record.MemberJSON("profile_id"); err != nil {
			return err
		} else if present && unfinishedDeploymentModel(a) {
			current, err := m.agentProfileID(tx, a)
			if err != nil || current != edits.record.ProfileID {
				return &DeploymentConflict{Message: msgAgentEditingDeployment}
			}
		}
		for _, name := range edits.members {
			raw, _, err := edits.record.MemberJSON(name)
			if err != nil {
				return err
			}
			if err := a.SetJSON(name, raw); err != nil {
				return err
			}
		}
		a.UpdatedAt = new(nowISO())
		updated = true
		return tx.Err()
	})
	return updated && err == nil, err
}

// agentProfileID uses the default only for an absent, null or empty binding.
// An R5 projection cannot authorize moving a deployment owned by another type.
func (m *Manager) agentProfileID(ctx context.Context, a *model.Agent) (string, error) {
	profile, err := agentProfileBinding(a)
	if err != nil {
		return "", err
	}
	if profile == "" {
		return m.defaultProfileID(ctx)
	}
	return profile, nil
}

func agentProfileBinding(a *model.Agent) (string, error) {
	raw, present, err := a.MemberJSON("profile_id")
	if err != nil {
		return "", err
	}
	profile, valid := storedText(raw, present, nil)
	if !valid && !storedAbsent(raw, present, nil) {
		return "", repo.NewValidationError("Invalid profile id")
	}
	return profile, nil
}

// ResolveAgentProfiles resolves optional bindings with one default lookup for
// the entire registry snapshot. It never reloads the document per Agent.
func (m *Manager) ResolveAgentProfiles(ctx context.Context, rows []model.Agent) ([]string, error) {
	result := make([]string, len(rows))
	var fallback string
	loaded := false
	for i := range rows {
		profile, err := agentProfileBinding(&rows[i])
		if err != nil {
			return nil, err
		}
		if profile == "" {
			if !loaded {
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

// sameAgentMember compares protocol scalars with their stored type evidence.
// Integral numeric port notation is equivalent; a quoted number or bool is not.
func sameAgentMember(a *model.Agent, name string, reported json.RawMessage) bool {
	stored, present, err := a.MemberJSON(name)
	if err != nil {
		return false
	}
	if name != "port" || !present {
		return sameStoredJSON(stored, present, reported)
	}
	var left, right *float64
	if json.Unmarshal(stored, &left) == nil && json.Unmarshal(reported, &right) == nil && left != nil && right != nil {
		var l, r model.Int
		if json.Unmarshal(stored, &l) == nil && json.Unmarshal(reported, &r) == nil {
			return l == r
		}
	}
	return sameStoredJSON(stored, true, reported)
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
	if a.LatestUpgrade != nil {
		return a.LatestUpgrade.HasMembers() && !deploymentTerminalModel(a.LatestUpgrade.Status)
	}
	// Malformed operation evidence must never unlock edits. Null is no task.
	raw, present, err := a.MemberJSON("latest_upgrade")
	return err != nil || !storedAbsent(raw, present, nil)
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
		if a.DNSFollowState == nil || a.DNSFollowState.Status != "preparing" && a.DNSFollowState.Status != "publishing" {
			continue
		}
		if a.ID == id && agentIDProven(a) {
			return true
		}
		raw, present, err := a.MemberJSON("dns_follow")
		if err != nil || !present {
			continue
		}
		var members map[string]json.RawMessage
		if json.Unmarshal(raw, &members) != nil {
			continue
		}
		if source, valid := storedText(members["source_agent_id"], members["source_agent_id"] != nil, nil); valid && source == id {
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
		service, valid := storedText(a.MemberJSON("service_type"))
		if enabled && (!valid || service != "mihomo") {
			if traffic {
				return &ValueError{Message: msgTrafficHistoryMihomo}
			}
			return &ValueError{Message: msgDomainDiscoveryMihomo}
		}
		flag, err := json.Marshal(enabled)
		if err != nil {
			return err
		}
		if traffic {
			if enabled && (a.TrafficHistoryEnabled == nil || !*a.TrafficHistoryEnabled) {
				since, err := json.Marshal(utcNowISO())
				if err != nil {
					return err
				}
				if err := a.SetJSON("traffic_history_since", since); err != nil {
					return err
				}
			}
			if err := a.SetJSON("traffic_history_enabled", flag); err != nil {
				return err
			}
		} else {
			if err := a.SetJSON("domain_discovery_enabled", flag); err != nil {
				return err
			}
		}
		updated, err := json.Marshal(nowISO())
		if err != nil {
			return err
		}
		if err := a.SetJSON("updated_at", updated); err != nil {
			return err
		}
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
