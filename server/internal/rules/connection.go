package rules

import (
	"configflow/internal/model"
	"configflow/internal/yamlx"
	"net/netip"
	"slices"
)

// RuleReference is one effective policy reference in the bound profile snapshot.
type RuleReference struct {
	Enabled    bool   `json:"enabled"`
	AfterMatch bool   `json:"after_match"`
	Policy     string `json:"policy"`
}

// RulesetInfo is the consumed editable source and its ordered references.
type RulesetInfo struct {
	ID         string          `json:"id"`
	Name       string          `json:"name"`
	Behavior   string          `json:"behavior"`
	References []RuleReference `json:"references"`
}

// IPDeps supplies pure snapshot lookup and source-preserving payload edits.
type IPDeps struct {
	Describe      func(string) (RulesetInfo, error)
	ReadPayload   func(string) ([]string, *yamlx.Node, error)
	AppendPayload func(string, string, *yamlx.Node) (string, error)
}

// IPTarget describes an editable source's allowed policy targets.
type IPTarget struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Behavior string   `json:"behavior"`
	Problem  *string  `json:"problem"`
	Active   bool     `json:"active"`
	Targets  []string `json:"targets"`
}

// IPTargets resolves the first applicable reference's policy without mutation.
func IPTargets(library []model.RuleLibraryItem, deps IPDeps) ([]IPTarget, error) {
	var result []IPTarget
	if deps.Describe == nil {
		return nil, &ProfileValidationError{Message: msgRuleInfo}
	}
	for _, item := range library {
		if valueOr(item.SourceType, "") != "content" {
			continue
		}
		behavior := valueOr(item.Behavior, "classical")
		if behavior != "classical" && behavior != "ipcidr" {
			continue
		}
		info, err := deps.Describe(item.ID)
		if err != nil {
			return nil, err
		}
		var effective []RuleReference
		for _, ref := range info.References {
			if ref.Enabled && !ref.AfterMatch {
				effective = append(effective, ref)
			}
		}
		pool := effective
		if len(pool) == 0 {
			pool = info.References
		}
		targets := []string{}
		if len(pool) == 0 {
			targets = []string{"direct", "proxy"}
		} else {
			policy := pool[0].Policy
			if policy == "DIRECT" {
				targets = []string{"direct"}
			} else if policy != "" && !slices.Contains([]string{"REJECT", "REJECT-DROP", "PASS"}, policy) {
				targets = []string{"proxy"}
			}
		}
		result = append(result, IPTarget{ID: info.ID, Name: info.Name, Behavior: info.Behavior, Active: len(effective) > 0 && enabledResource(&item, item.Enabled), Targets: targets})
	}
	return result, nil
}

// Destination is a validated connection target network.
type Destination struct{ network netip.Prefix }

// String is the canonical CIDR text.
func (d *Destination) String() string { return d.network.String() }

// IsIPv4 reports the address family.
func (d *Destination) IsIPv4() bool { return d.network.Addr().Is4() }

// ParseDestination reads the value as a client address or range whose family
// must agree with the rule type (IP-CIDR for IPv4, IP-CIDR6 for IPv6).
func ParseDestination(value string, ruleType string) (*Destination, error) {
	invalid := &ProfileValidationError{Message: msgDestination}
	text, err := NormalizeClientRange(value)
	if err != nil {
		return nil, invalid
	}
	network, ok := parseNetwork(text)
	if !ok {
		return nil, invalid
	}
	want := "IP-CIDR"
	if network.Addr().Is6() {
		want = "IP-CIDR6"
	}
	if ruleType != want {
		return nil, invalid
	}
	return &Destination{network}, nil
}

// IPAdded records a newly appended destination.
type IPAdded struct {
	Value     string `json:"value"`
	RuleType  string `json:"rule_type"`
	Target    string `json:"target"`
	Ruleset   string `json:"ruleset"`
	RulesetID string `json:"ruleset_id"`
}

// IPSkipped records an existing network covering the destination.
type IPSkipped struct {
	Value  string `json:"value"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// IPResult is the mutation outcome; Updated declarations are refreshed by the owner.
type IPResult struct {
	Added    []IPAdded
	Skipped  []IPSkipped
	Warnings []string
	Updated  []model.RuleLibraryItem
}

// AppendIP validates policy against the bound snapshot and updates a shared
// declaration in the caller's transaction. It performs no repository/cache I/O.
func AppendIP(deps IPDeps, library []model.RuleLibraryItem, id, target string, destination *Destination, ruleType string) (*IPResult, error) {
	result := &IPResult{Added: []IPAdded{}, Skipped: []IPSkipped{}, Warnings: []string{}}
	candidates, err := IPTargets(library, deps)
	if err != nil {
		return nil, err
	}
	var info *IPTarget
	for i := range candidates {
		candidate := &candidates[i]
		if candidate.ID == id && slices.Contains(candidate.Targets, target) {
			info = candidate
			break
		}
	}
	if info == nil || destination == nil {
		return nil, &ProfileValidationError{Message: msgIPTargetChanged}
	}
	if !info.Active {
		result.Warnings = append(result.Warnings, msgInactiveIPRule)
	}
	var item *model.RuleLibraryItem
	for i := range library {
		if library[i].ID == id {
			item = &library[i]
			break
		}
	}
	if item == nil {
		return nil, &ProfileValidationError{Message: msgIPTargetChanged}
	}
	content := valueOr(item.Content, "")
	if deps.ReadPayload == nil || deps.AppendPayload == nil {
		return nil, &ProfileValidationError{Message: msgRuleInfo}
	}
	lines, payload, err := deps.ReadPayload(content)
	if err != nil {
		return nil, err
	}
	for _, raw := range lines {
		kind, value, ok := ParseRuleLine(raw)
		if !ok || !slices.Contains([]string{"IP", "IP-CIDR", "IP-CIDR6"}, kind) {
			continue
		}
		network, ok := parseNetwork(value)
		if !ok {
			continue
		}
		covered := destination.network
		if network.Addr().Is4() == covered.Addr().Is4() && covered.Bits() >= network.Bits() && network.Contains(covered.Addr()) {
			result.Skipped = append(result.Skipped, IPSkipped{destination.String(), target, "exists"})
			return result, nil
		}
	}
	cidr := destination.String()
	line := ruleType + "," + cidr + ",no-resolve"
	if info.Behavior == "ipcidr" {
		line = cidr
	}
	updated, err := deps.AppendPayload(content, line, payload)
	if err != nil {
		return nil, err
	}
	item.Content = &updated
	result.Updated = append(result.Updated, *item)
	result.Added = append(result.Added, IPAdded{cidr, ruleType, target, item.Name, id})
	return result, nil
}
