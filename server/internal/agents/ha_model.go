package agents

import (
	"bytes"
	"configflow/internal/jsondecode"
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
	raw, present, err := record.MemberJSON("oxidns_ha")
	var identity struct {
		GroupID string `json:"group_id"`
		PeerID  string `json:"peer_id"`
	}
	if err != nil || !present || jsondecode.Unmarshal(raw, &identity) != nil {
		return "", "", false
	}
	return identity.GroupID, identity.PeerID, identity.GroupID != "" && identity.PeerID != ""
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
