package onboarding

import (
	"encoding/json"
	"net/url"
	"slices"
	"sort"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

// Document is a profile snapshot with unknown sections preserved verbatim.
// Known settings are read through model and edited only in candidate copies.
type Document map[string]json.RawMessage

func encode[T any](value T) json.RawMessage { data, _ := json.Marshal(value); return data }
func object(raw json.RawMessage) Document {
	var result Document
	_ = json.Unmarshal(raw, &result)
	return result
}
func clone(config Document) Document {
	out := make(Document, len(config))
	for key, value := range config {
		out[key] = slices.Clone(value)
	}
	return out
}
func stringValue(raw json.RawMessage) string { var s string; _ = json.Unmarshal(raw, &s); return s }
func stringList(n *yamlx.Node) []string {
	out := []string{}
	if n != nil {
		for _, v := range n.Content {
			if s := text(v); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}
func on(value *bool) bool { return value == nil || *value }
func policy(rule model.RuleConfig) string {
	if rule.Policy != nil {
		return *rule.Policy
	}
	return ""
}
func optional(value *string, fallback string) string {
	if value != nil {
		return *value
	}
	return fallback
}
func readConfig(config Document) (model.Profile, model.Shared, error) {
	var profile model.Profile
	var shared model.Shared
	raw, err := json.Marshal(config)
	if err == nil {
		err = json.Unmarshal(raw, &profile)
	}
	if err == nil {
		err = json.Unmarshal(raw, &shared)
	}

	if err == nil {
		for i, f := range storedFlags(config["rule_configs"], "enabled", len(profile.RuleConfigs)) {
			profile.RuleConfigs[i].Enabled = f
		}
		for i, f := range storedFlags(config["rule_configs"], "library_enabled", len(profile.RuleConfigs)) {
			profile.RuleConfigs[i].LibraryEnabled = f
		}
		for i, f := range storedFlags(config["proxy_groups"], "enabled", len(profile.ProxyGroups)) {
			profile.ProxyGroups[i].Enabled = f
		}
		for i, f := range storedFlags(config["rule_library"], "enabled", len(shared.RuleLibrary)) {
			shared.RuleLibrary[i].Enabled = f
		}
		for i, f := range storedFlags(config["nodes"], "enabled", len(shared.Nodes)) {
			shared.Nodes[i].Enabled = f
		}
		for i, f := range storedFlags(config["subscriptions"], "enabled", len(shared.Subscriptions)) {
			shared.Subscriptions[i].Enabled = f
		}
		for i, f := range storedFlags(config["subscription_aggregations"], "enabled", len(shared.Aggregations)) {
			shared.Aggregations[i].Enabled = f
		}
	}
	return profile, shared, err
}

// Stored enablement is fail-closed: an absent switch uses the schema default;
// null or an invalid typed switch cannot prove that a source is enabled. R5's
// nil fallback alone cannot distinguish those states.
func storedFlags(raw json.RawMessage, key string, count int) []*bool {
	var rows []json.RawMessage
	if json.Unmarshal(raw, &rows) != nil {
		return nil
	}
	out := make([]*bool, count)
	for i, row := range rows {
		if i >= len(out) {
			break
		}
		if flag, exists := object(row)[key]; exists {
			enabled := string(flag) == "true"
			out[i] = &enabled
		}
	}
	return out
}

// DomainCandidate exposes DNS selections without the IP editor's selected field.
type DomainCandidate struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Behavior    string `json:"behavior"`
	Policy      string `json:"policy"`
	Enabled     bool   `json:"enabled"`
	Available   bool   `json:"available"`
	Reason      string `json:"reason"`
	Description string `json:"description,omitempty"`
}

func domainCandidates(config Document, selections []string) ([]DomainCandidate, error) {
	profile, shared, err := readConfig(config)
	if err != nil {
		return nil, err
	}
	library := map[string]model.RuleLibraryItem{}
	for _, item := range shared.RuleLibrary {
		if _, exists := library[item.ID]; !exists {
			library[item.ID] = item
		}
	}
	result := []DomainCandidate{}
	seen := map[string]bool{}
	for _, rule := range profile.RuleConfigs {
		if rule.ItemType != "ruleset" {
			continue
		}
		behavior := optional(rule.Behavior, "domain")
		ruleFields := object(encode(rule))
		if _, present := ruleFields["behavior"]; present && rule.Behavior == nil {
			behavior = ""
		}
		name := rule.Name
		if _, present := ruleFields["name"]; !present {
			name = rule.ID
		}
		format := optional(rule.Format, "")
		rawURL := optional(rule.URL, "")
		enabled := on(rule.Enabled) && on(rule.LibraryEnabled)
		source, found := library[rule.LibraryRuleID]
		if found {
			behavior = optional(source.Behavior, behavior)
			sourceFields := object(encode(source))
			if _, present := sourceFields["behavior"]; present && source.Behavior == nil {
				behavior = ""
			}
			if _, present := sourceFields["name"]; present {
				name = source.Name
			}
			format = optional(source.Format, "")
			rawURL = optional(source.URL, "")
			enabled = enabled && on(source.Enabled)
		}
		if behavior != "domain" && behavior != "classical" && !slices.Contains(selections, rule.ID) {
			continue
		}
		seen[rule.ID] = true
		reason := ""
		u, _ := url.Parse(rawURL)
		mrs := format == "mrs" || u != nil && strings.HasSuffix(strings.ToLower(u.Path), ".mrs")
		switch {
		case rule.LibraryRuleID != "" && !found:
			reason = msgLibraryDeleted
		case behavior != "domain" && behavior != "classical":
			reason = msgExistingIP
		case mrs:
			reason = msgMRS
		case !enabled:
			reason = msgDisabled
		}
		description := msgDomain
		if behavior == "classical" {
			description = msgClassic
		}
		result = append(result, DomainCandidate{ID: rule.ID, Name: name, Behavior: behavior, Policy: policy(rule), Enabled: enabled, Available: reason == "", Reason: reason, Description: description})
	}
	for _, id := range selections {
		if !seen[id] {
			result = append(result, DomainCandidate{ID: id, Name: id, Reason: msgMissing})
		}
	}
	return result, nil
}

// Prerequisites describes actual emitted policy reachability, or a cheap read-screen estimate.
type Prerequisites struct {
	Ready    bool     `json:"ready"`
	Policies []string `json:"policies"`
	Warnings []string `json:"warnings"`
	Verified bool     `json:"verified"`
}

func proxyPrerequisites(config Document, document *yamlx.Node) (Prerequisites, error) {
	result := Prerequisites{Policies: []string{}, Warnings: []string{}, Verified: document != nil}
	profile, shared, err := readConfig(config)
	if err != nil {
		return result, err
	}
	if document == nil {
		for _, group := range profile.ProxyGroups {
			if on(group.Enabled) && group.Name != "" {
				result.Policies = append(result.Policies, group.Name)
			}
		}
		sources := false
		for _, node := range shared.Nodes {
			sources = sources || on(node.Enabled)
		}
		for _, sub := range shared.Subscriptions {
			sources = sources || on(sub.Enabled)
		}
		for _, agg := range shared.Aggregations {
			sources = sources || on(agg.Enabled)
		}
		result.Ready = sources && len(result.Policies) > 0
		return result, nil
	}
	providers := map[string]bool{}
	for key := range yamlx.Pairs(field(document, "proxy-providers")) {
		providers[key] = true
	}
	reachable := map[string]bool{}
	if proxies := field(document, "proxies"); proxies != nil {
		for _, proxy := range proxies.Content {
			kind := text(field(proxy, "type"))
			name := text(field(proxy, "name"))
			if name != "" && !slices.Contains([]string{"direct", "reject", "pass"}, kind) {
				reachable[name] = true
			}
		}
	}
	// Reverse edges propagate reachability once per edge, including cycles.
	dependents := map[string][]string{}
	queue := []string{}
	for name := range reachable {
		queue = append(queue, name)
	}
	allProxies := []string{}
	if groups := field(document, "proxy-groups"); groups != nil {
		for _, group := range groups.Content {
			name := text(field(group, "name"))
			if name == "" {
				continue
			}
			ready := boolean(field(group, "include-all-providers")) && len(providers) > 0
			for _, provider := range stringList(field(group, "use")) {
				ready = ready || providers[provider]
			}
			if boolean(field(group, "include-all-proxies")) {
				allProxies = append(allProxies, name)
			}
			for _, member := range stringList(field(group, "proxies")) {
				dependents[member] = append(dependents[member], name)
			}
			if ready && !reachable[name] {
				reachable[name] = true
				queue = append(queue, name)
			}
		}
	}
	if len(queue) > 0 {
		for _, name := range allProxies {
			if !reachable[name] {
				reachable[name] = true
				queue = append(queue, name)
			}
		}
	}
	for i := 0; i < len(queue); i++ {
		for _, name := range dependents[queue[i]] {
			if !reachable[name] {
				reachable[name] = true
				queue = append(queue, name)
			}
		}
	}
	for name := range reachable {
		result.Policies = append(result.Policies, name)
	}
	sort.Strings(result.Policies)
	var catchall *model.RuleConfig
	for _, rule := range profile.RuleConfigs {
		if on(rule.Enabled) && on(rule.LibraryEnabled) {
			result.Ready = result.Ready || reachable[policy(rule)]
			if catchall == nil && rule.ItemType == "rule" && rule.RuleType == "MATCH" {
				v := rule
				catchall = &v
			}
		}
	}
	if catchall == nil {
		result.Warnings = append(result.Warnings, msgNoMatch)
	} else if p := policy(*catchall); !reachable[p] && p != "DIRECT" {
		result.Warnings = append(result.Warnings, msgBadMatch)
	}
	return result, nil
}
