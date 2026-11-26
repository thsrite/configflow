package model

import "fmt"

// Config is the resolved view of one profile that generators, pages and
// Agents read (repo.Config; Python's compat config): the profile's own
// members, its id, the system sections and the shared resources its
// composition selects, with every ruleset carrying the source of the rule
// library item it references. repo builds it from one generation; it is never
// stored, and changing it changes nothing stored.
type Config struct {
	// Profile is a copy of the stored profile; rulesets in RuleConfigs are
	// hydrated from the rule library.
	Profile      *Profile
	ProfileID    string
	SystemConfig SystemConfig
	Backup       Backup
	// Agents is every registered Agent when HasAgents is set; views built
	// for publishing leave it out.
	Agents    []Agent
	HasAgents bool
	// Subscriptions, Nodes and Aggregations are the selected shared
	// resources, in the shared catalogs' order.
	Subscriptions []Subscription
	Nodes         []Node
	Aggregations  []Aggregation
	// RuleLibrary is the whole shared rule library.
	RuleLibrary []RuleLibraryItem
}

// MarshalJSON writes the profile's members in stored order followed by
// profile_id, system_config, backup, agents (when present), subscriptions,
// nodes, subscription_aggregations and rule_library; a member of the same
// name in the profile keeps its position and takes the resolved value.
func (c Config) MarshalJSON() ([]byte, error) {
	if c.Profile == nil {
		return nil, fmt.Errorf("%w: config without profile", ErrType)
	}
	encoded, err := encodeUnit(c.Profile)
	if err != nil {
		return nil, err
	}
	var members Fields
	if err := members.UnmarshalJSON(encoded); err != nil {
		return nil, err
	}
	set := func(name string, value any) error {
		data, err := encodeUnit(value)
		if err != nil {
			return err
		}
		members.set(name, data)
		return nil
	}
	type member struct {
		name  string
		value any
	}
	list := []member{{"profile_id", c.ProfileID}, {"system_config", c.SystemConfig}, {"backup", c.Backup}}
	if c.HasAgents {
		list = append(list, member{"agents", c.Agents})
	}
	list = append(list, member{"subscriptions", c.Subscriptions}, member{"nodes", c.Nodes},
		member{"subscription_aggregations", c.Aggregations}, member{"rule_library", c.RuleLibrary})
	for _, item := range list {
		if err := set(item.name, item.value); err != nil {
			return nil, err
		}
	}
	return members.MarshalJSON()
}
