package rules

import (
	"net/netip"

	"configflow/internal/jsonx"
)

// A connection destination (or its selected subnet) is appended to a shared
// IP rule set (backend/utils/connection_rules.py). The repository
// transaction and the cache write stay with the caller: ApplyIPRule runs
// inside the shared-config write transaction, and the caller then refreshes
// the caches of Result.Updated.

// ConnectionDeps are the pieces of other packages the connection rules call.
type ConnectionDeps struct {
	// RulesetInfo describes a rule set of config: an object with id, name,
	// behavior and references (each with enabled, after_match and policy).
	RulesetInfo func(config *jsonx.Object, rulesetID string) *jsonx.Object
	// ReadRulePayload returns the entries of a rule set plus an opaque
	// handle for a lossless append.
	ReadRulePayload func(content string) (lines []string, payload any, err error)
	// AppendRulePayload appends one entry to rule set content.
	AppendRulePayload func(content, line string, payload any) string
}

// IPRuleTargets lists the editable content rule sets that can
// take IP rules, with the targets ("direct", "proxy") their current policy
// allows. Entries are objects with id, name, behavior, problem, active and
// targets.
func IPRuleTargets(config *jsonx.Object, deps ConnectionDeps) []*jsonx.Object {
	var result []*jsonx.Object
	for _, entry := range config.GetList("rule_library") {
		item, ok := entry.(*jsonx.Object)
		if !ok {
			continue
		}
		if item.Value("source_type") != "content" {
			continue
		}
		if behavior := item.GetOr("behavior", "classical"); behavior != "classical" && behavior != "ipcidr" {
			continue
		}
		info := deps.RulesetInfo(config, item.GetString("id"))
		references := info.GetList("references")
		var effective []*jsonx.Object
		var all []*jsonx.Object
		for _, ref := range references {
			object, _ := ref.(*jsonx.Object)
			all = append(all, object)
			if jsonx.Truthy(object.Value("enabled")) && !jsonx.Truthy(object.Value("after_match")) {
				effective = append(effective, object)
			}
		}
		// A rule set's first applicable reference determines its policy.
		pool := effective
		if len(pool) == 0 {
			pool = all
		}
		targets := []any{}
		if len(pool) == 0 {
			targets = []any{"direct", "proxy"}
		} else {
			policy := pool[0].Value("policy")
			switch {
			case policy == "DIRECT":
				targets = []any{"direct"}
			case jsonx.Truthy(policy) && policy != "REJECT" && policy != "REJECT-DROP" && policy != "PASS":
				targets = []any{"proxy"}
			}
		}
		var active any = false
		if len(effective) > 0 {
			active = item.GetOr("enabled", true)
		}
		result = append(result, jsonx.Obj(
			"id", info.Value("id"), "name", info.Value("name"), "behavior", info.Value("behavior"),
			"problem", nil, "active", active, "targets", targets,
		))
	}
	return result
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
	invalid := &ProfileValidationError{Message: "请选择有效的目标 IP 或网段，IPv4 使用 IP-CIDR，IPv6 使用 IP-CIDR6。"}
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

// IPRuleResult is what an IP rule request reports: Added and Skipped entries, the
// warnings, and the library items whose content changed (their cache files
// must be refreshed by the caller).
type IPRuleResult struct {
	Added, Skipped []any
	Warnings       []any
	Updated        []*jsonx.Object
}

// ApplyIPRule appends the destination to the rule set. config is the bound
// profile's compatibility config, whose rule_library is replaced by the
// shared one; shared is the shared document, modified in place. Binding,
// format and current policy are checked here again, under the caller's write
// lock.
func ApplyIPRule(deps ConnectionDeps, config, shared *jsonx.Object, rulesetID, target string, destination *Destination, ruleType string) (*IPRuleResult, error) {
	result := &IPRuleResult{Added: []any{}, Skipped: []any{}, Warnings: []any{}}
	config.Set("rule_library", shared.GetOr("rule_library", []any{}))
	var info *jsonx.Object
	for _, candidate := range IPRuleTargets(config, deps) {
		if candidate.Value("id") != rulesetID {
			continue
		}
		for _, allowed := range candidate.GetList("targets") {
			if allowed == target {
				info = candidate
			}
		}
		if info != nil {
			break
		}
	}
	if info == nil {
		return nil, &ProfileValidationError{Message: "所选规则集不支持 IP、不可编辑或策略已改变，请重新打开后选择。"}
	}
	// The warning applies to skipped requests too.
	if !jsonx.Truthy(info.Value("active")) {
		result.Warnings = append(result.Warnings, "此规则集尚未在当前配置中启用，或位于兜底规则之后；请在策略规则中设置并发布后生效。")
	}
	var item *jsonx.Object
	for _, entry := range shared.GetList("rule_library") {
		if candidate, ok := entry.(*jsonx.Object); ok && candidate.Value("id") == rulesetID {
			item = candidate
			break
		}
	}
	content := ""
	if text, ok := item.Value("content").(string); ok {
		content = text
	}
	lines, payload, err := deps.ReadRulePayload(content)
	if err != nil {
		return nil, err
	}
	for _, raw := range lines {
		kind, value, ok := ParseRuleLine(raw)
		if !ok || (kind != "IP" && kind != "IP-CIDR" && kind != "IP-CIDR6") {
			continue
		}
		network, ok := parseNetwork(value)
		if !ok {
			continue
		}
		if covered := destination.network; network.Addr().Is4() == covered.Addr().Is4() &&
			covered.Bits() >= network.Bits() && network.Contains(covered.Addr()) {
			result.Skipped = append(result.Skipped, jsonx.Obj("value", destination.String(), "target", target, "reason", "exists"))
			return result, nil
		}
	}
	cidr := destination.String()
	line := ruleType + "," + cidr + ",no-resolve"
	if info.Value("behavior") == "ipcidr" {
		line = cidr
	}
	item.Set("content", deps.AppendRulePayload(content, line, payload))
	result.Updated = append(result.Updated, item)
	result.Added = append(result.Added, jsonx.Obj(
		"value", cidr, "rule_type", ruleType, "target", target,
		"ruleset", item.Value("name"), "ruleset_id", rulesetID,
	))
	return result, nil
}
