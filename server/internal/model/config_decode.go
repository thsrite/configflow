package model

import (
	"encoding/json"
	"fmt"
)

// DecodeConfig decodes a captured resolved configuration, assigning each member
// to exactly one owning record. Live repository readers should use repo.Config
// or repo.View instead; this codec is for immutable receipts/export snapshots,
// which are stored data: members are decoded leniently (DecodeStoredMember)
// and members no record declares are dropped.
func DecodeConfig(data []byte) (*Config, error) {
	var members Fields
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, fmt.Errorf("decode captured configuration: %w", err)
	}
	var profile Profile
	var shared Shared
	var system System
	var id String
	hasAgents := false
	set := func(_ string, rec any, key string, raw []byte) error {
		_, _, err := DecodeStoredMember(rec, key, raw, key)
		return err
	}
	for key, raw := range members.All() {
		var err error
		switch key {
		case "profile_id":
			err = json.Unmarshal(raw, &id)
		case "nodes", "subscriptions", "subscription_aggregations", "rule_library":
			err = set("shared", &shared, key, raw)
		case "system_config", "backup", "agents":
			err = set("system", &system, key, raw)
			if key == "agents" {
				hasAgents = true
			}
		default:
			err = set("profile", &profile, key, raw)
		}
		if err != nil {
			return nil, fmt.Errorf("decode captured configuration member %s: %w", key, err)
		}
	}
	return &Config{Profile: &profile, ProfileID: string(id), SystemConfig: system.SystemConfig, Backup: system.Backup, Agents: system.Agents, HasAgents: hasAgents, Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}, nil
}
