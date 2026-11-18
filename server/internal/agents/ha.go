package agents

import (
	"context"
	"errors"
	"reflect"
)

// ErrPairIncomplete means saved HA members no longer form a reciprocal pair.
var ErrPairIncomplete = errors.New(msgHAPairIncomplete)

// HAEnabled reads the registry's strict boolean HA setting.
func HAEnabled(agent *Record) bool { return haEnabled(agent) }

// HAPair reads a reciprocal HA pair from one lightweight registry snapshot.
// A disabled member has no pair; incomplete identity proofs fail closed.
func (m *Manager) HAPair(ctx context.Context, agentID string) ([]*Record, error) {
	records, err := m.GetAllAgents(ctx, false)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]*Record, len(records))
	for _, r := range records {
		byID[r.GetString("id")] = r
	}
	agent := byID[agentID]
	if agent == nil || !HAEnabled(agent) {
		return nil, nil
	}
	ha := agent.GetObject("oxidns_ha")
	peer := byID[ha.GetString("peer_id")]
	other := peer.GetObject("oxidns_ha")
	if peer == nil || !HAEnabled(peer) || ha.GetString("group_id") == "" || ha.GetString("group_id") != other.GetString("group_id") || other.GetString("peer_id") != agentID || !reflect.DeepEqual(peer.Value("profile_id"), agent.Value("profile_id")) {
		return nil, ErrPairIncomplete
	}
	return []*Record{agent, peer}, nil
}
