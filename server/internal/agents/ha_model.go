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
	switch record.OxiDNSHAPublication.Status {
	case "succeeded", "failed", "rollback_failed":
		return false
	default:
		return true
	}
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
	ha := record.OxiDNSHA
	peer := registryAgent(records, ha.PeerID)
	if !HAEnabledModel(peer) || ha.GroupID == "" || peer.OxiDNSHA.GroupID != ha.GroupID || peer.OxiDNSHA.PeerID != id || peer.ProfileID != record.ProfileID {
		return nil, ErrPairIncomplete
	}
	return []model.Agent{*record, *peer}, nil
}
