package model

import (
	"encoding/json"
	"fmt"
)

// DecodeConfig decodes a captured resolved configuration, assigning each member
// to exactly one owning record. Live repository readers should use repo.Config
// or repo.View instead; this codec is for immutable receipts/export snapshots.
func DecodeConfig(data []byte) (*Config, error) {
	var members Fields
	if err := json.Unmarshal(data, &members); err != nil {
		return nil, fmt.Errorf("decode captured configuration: %w", err)
	}
	var profileFields, sharedFields, systemFields Fields
	var id String
	hasAgents := false
	for key, raw := range members.All() {
		var err error
		switch key {
		case "profile_id":
			err = json.Unmarshal(raw, &id)
		case "nodes", "subscriptions", "subscription_aggregations", "rule_library":
			err = sharedFields.Set(key, raw)
		case "system_config", "backup", "agents":
			err = systemFields.Set(key, raw)
			if key == "agents" {
				hasAgents = true
			}
		default:
			err = profileFields.Set(key, raw)
		}
		if err != nil {
			return nil, fmt.Errorf("decode captured configuration member %s: %w", key, err)
		}
	}
	p, err := profileFields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	sh, err := sharedFields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	sy, err := systemFields.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var profile Profile
	var shared Shared
	var system System
	if err = json.Unmarshal(p, &profile); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(sh, &shared); err != nil {
		return nil, err
	}
	if err = json.Unmarshal(sy, &system); err != nil {
		return nil, err
	}
	return &Config{Profile: &profile, ProfileID: string(id), SystemConfig: system.SystemConfig, Backup: system.Backup, Agents: system.Agents, HasAgents: hasAgents, Nodes: shared.Nodes, Subscriptions: shared.Subscriptions, Aggregations: shared.Aggregations, RuleLibrary: shared.RuleLibrary}, nil
}
