package oxidns

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrRoutingLocked means a native DNS routing graph cannot be safely rewritten.
var ErrRoutingLocked = errors.New("OxiDNS routing is locked")

// RuleDNSMetadataFields returns the action, parked-action and follow storage keys.
// RoutingFields also includes selectors, but its RuleDNSFields omits parked data.
func RuleDNSMetadataFields() []string { return append([]string{}, ruleDNSFields...) }

// Convertible reports whether a rule can participate in domain DNS routing.
func Convertible(item *Node) bool { return convertible(item) }

// HasIPPart reports known or possible IP content, for legacy routing recognition.
func HasIPPart(item *Node, p *Profile) bool { return hasIPPart(item, p) }

// RoutingUsage identifies the part of the native configuration blocking a write.
type RoutingUsage struct {
	Kind   string `json:"kind"`
	Code   string `json:"code"`
	Target string `json:"target"`
	Reason string `json:"reason"`
}

// RoutingLockedError is a conflict; HTTP callers return 409 with Usages.
type RoutingLockedError struct {
	Reason string
	Usages []RoutingUsage
}

func (e *RoutingLockedError) Error() string { return e.Reason }
func (e *RoutingLockedError) Unwrap() error { return ErrRoutingLocked }

func routingLocked(reason string) error {
	return &RoutingLockedError{reason, []RoutingUsage{{"oxidns", "oxidns_routing_custom", "sequence_main", reason}}}
}

// DNSAssignment assigns one rule's DNS choice. Slice order is preserved.
type DNSAssignment struct {
	RuleID string `json:"rule_id"`
	Choice string `json:"dns"`
}

// RuleDNSApplyOptions controls strict versus warning-only updates to locked drafts.
// Strict is the default. Lenient keeps native YAML and returns a warning on conflict.
type RuleDNSApplyOptions struct {
	BaseURL string
	Lenient bool
}

// RuleDNSUpdate is a replacement settings draft; callers save it in their transaction.
// Neither input profile is changed, including on error. Changed concerns settings only.
type RuleDNSUpdate struct {
	Settings *Node
	Warnings []string
	Changed  bool
}

// CheckDNSChoice validates the rules-page DNS vocabulary, including pinned upstream keys.
func CheckDNSChoice(choice string) error {
	switch choice {
	case "follow", "default", "direct", "proxy", "refused", "nxdomain", "nodata", "blackhole":
		return nil
	}
	if strings.HasPrefix(choice, "upstream:") && keyRE.MatchString(choice[9:]) {
		return nil
	}
	return fail(fmt.Sprintf(messageDNSInvalidChoice, choice))
}

func dnsRuleMap(p *Profile) map[string]*Node {
	out := map[string]*Node{}
	for _, item := range items(p.get("rule_configs")) {
		if isMap(item) && isStr(get(item, "id")) {
			out[str(get(item, "id"))] = item
		}
	}
	return out
}

func routingDependencies(p *Profile, routes map[string]string) *Node {
	out := L()
	for _, item := range items(p.get("rule_configs")) {
		if !isMap(item) {
			continue
		}
		values := L()
		for _, key := range []string{"id", "itemType", "rule_type", "value", "behavior", "url"} {
			values.Content = append(values.Content, orNull(get(item, key)))
		}
		values.Content = append(values.Content, orDefault(item, "enabled", B(true)), orDefault(item, "library_enabled", B(true)))
		route := followRoute(item, routes)
		if route == "" {
			values.Content = append(values.Content, Null())
		} else {
			values.Content = append(values.Content, S(route))
		}
		out.Content = append(out.Content, values)
	}
	return out
}

func pruneDNSFields(values *Node, rules map[string]*Node) *Node {
	out := M()
	for _, key := range ruleDNSFields {
		value := get(values, key)
		if isMap(value) {
			kept := M()
			pairs(value, func(id string, action *Node) bool {
				if rules[id] != nil {
					set(kept, id, clone(action))
				}
				return true
			})
			set(out, key, kept)
		} else {
			set(out, key, seqOf(filter(items(value), func(id *Node) bool { return isStr(id) && rules[id.Value] != nil })))
		}
	}
	return out
}

func dirtyDNSField(settings, stored *Node, key string) bool {
	value := get(settings, key)
	return !isNone(value) && !equal(value, get(stored, key))
}

func installDNSFields(settings, values *Node, fields []string) {
	for _, key := range fields {
		set(settings, key, clone(get(values, key)))
	}
}

func placeDNSChoice(values, item, action *Node) {
	id := str(get(item, "id"))
	for _, key := range Selectors {
		set(values, key, seqOf(filter(items(get(values, key)), func(value *Node) bool { return !isOneOf(value, id) })))
	}
	del(get(values, "rule_actions"), id)
	del(get(values, "rule_dns_parked"), id)
	if action == nil {
		return
	}
	if route := str(get(action, "action")); isListRoute(route) {
		key := route + "_" + suffixOf(item)
		list := get(values, key)
		list.Content = append(list.Content, S(id))
	} else {
		set(get(values, "rule_actions"), id, clone(action))
	}
}

func resolveDNSChoice(choice string, item, values *Node, catalog []UpstreamCandidate, routes map[string]string) (*Node, error) {
	switch choice {
	case "default":
		return nil, nil
	case "follow":
		if route := followRoute(item, routes); route != "" {
			return M("action", route), nil
		}
		return nil, nil
	}
	if !strings.HasPrefix(choice, "upstream:") {
		return M("action", choice), nil
	}
	id := str(get(item, "id"))
	current := get(get(values, "rule_actions"), id)
	if current == nil {
		current = get(get(values, "rule_dns_parked"), id)
	}
	if current != nil && RouteKey(current) == choice {
		return clone(current), nil
	}
	for _, candidate := range catalog {
		if candidate.Key != choice[9:] {
			continue
		}
		if candidate.Linked {
			return nil, fail(messageDNSLinkedUpstream)
		}
		entry, err := FromJSON(candidate.Entry)
		if err != nil {
			return nil, fmt.Errorf("decode upstream candidate: %w", err)
		}
		return M("action", "upstream", "source", candidate.Source, "upstream", entry), nil
	}
	return nil, fail(messageDNSStaleUpstream)
}

func validationIssueKey(issue Issue) string {
	path, _ := json.Marshal(issue.Path)
	return string(path) + "\x00" + issue.Code + "\x00" + issue.Message
}

func newValidationIssue(before, after *Node) *Issue {
	existing := map[string]bool{}
	for _, issue := range ValidateNativeConfig(before) {
		if issue.Severity == "" || issue.Severity == "error" {
			existing[validationIssueKey(issue)] = true
		}
	}
	added := []Issue{}
	for _, issue := range ValidateNativeConfig(after) {
		if (issue.Severity == "" || issue.Severity == "error") && !existing[validationIssueKey(issue)] {
			added = append(added, issue)
		}
	}
	if len(added) == 0 {
		return nil
	}
	sort.SliceStable(added, func(i, j int) bool {
		a, b := added[i], added[j]
		for n := 0; n < len(a.Path) && n < len(b.Path); n++ {
			left, right := a.Path[n], b.Path[n]
			if left.IsIndex && right.IsIndex && left.Index != right.Index {
				return left.Index < right.Index
			}
			if left.Key != right.Key {
				return left.Key < right.Key
			}
		}
		if len(a.Path) != len(b.Path) {
			return len(a.Path) < len(b.Path)
		}
		if a.Code != b.Code {
			return a.Code < b.Code
		}
		return a.Message < b.Message
	})
	return &added[0]
}

// ApplyRuleDNS computes one profile transaction's DNS changes using complete
// resolved before/after snapshots. Duplicate assignments use the last choice,
// retaining the first occurrence's position. It does not persist or mutate inputs.
func ApplyRuleDNS(before, after *Profile, assignments []DNSAssignment, options RuleDNSApplyOptions) (RuleDNSUpdate, error) {
	if before == nil {
		before = NewProfile(nil)
	}
	if after == nil {
		after = NewProfile(nil)
	}
	settings := mapOr(after.Settings())
	out := clone(settings)
	finish := func(warnings []string) RuleDNSUpdate { return RuleDNSUpdate{out, warnings, !same(out, settings)} }
	rules, previous := dnsRuleMap(after), dnsRuleMap(before)
	choices, order := map[string]string{}, []string{}
	for _, assignment := range assignments {
		if _, exists := choices[assignment.RuleID]; !exists {
			order = append(order, assignment.RuleID)
		}
		choices[assignment.RuleID] = assignment.Choice
	}
	for _, id := range order {
		if err := CheckDNSChoice(choices[id]); err != nil {
			return RuleDNSUpdate{}, err
		}
		item := rules[id]
		if item == nil {
			return RuleDNSUpdate{}, fail(fmt.Sprintf(messageDNSMissingRule, id))
		}
		if choices[id] != "default" && !convertible(item) {
			kind := str(get(item, "rule_type"))
			if kind == "" {
				kind = str(get(item, "behavior"))
			}
			if kind == "" {
				kind = messageDNSUnknownKind
			}
			return RuleDNSUpdate{}, fail(fmt.Sprintf(messageDNSAssignmentUnsupported, kind))
		}
	}
	stored := storedRuleDNS(settings)
	pruned := pruneDNSFields(stored, rules)
	beforeRoutes, afterRoutes := policyDNSRoutes(before), policyDNSRoutes(after)
	if len(order) == 0 && equal(routingDependencies(before, beforeRoutes), routingDependencies(after, afterRoutes)) {
		for _, key := range ruleDNSFields {
			if !equal(get(pruned, key), get(stored, key)) || dirtyDNSField(settings, stored, key) {
				installDNSFields(out, pruned, ruleDNSFields)
				break
			}
		}
		return finish([]string{}), nil
	}
	view, err := readRuleDNS(before, settings)
	if err != nil {
		return RuleDNSUpdate{}, err
	}
	locked := func(reason string) (RuleDNSUpdate, error) {
		if !options.Lenient {
			return RuleDNSUpdate{}, routingLocked(reason)
		}
		installDNSFields(out, pruned, ruleDNSFields)
		return finish([]string{fmt.Sprintf(messageDNSUnchanged, reason)}), nil
	}
	native := isOneOf(get(settings, "config_mode"), "native")
	if native && view.Document == nil {
		return locked(view.Reason)
	}
	values := clone(view.Values)
	for _, key := range Selectors {
		set(values, key, seqOf(filter(items(get(values, key)), func(id *Node) bool { return rules[str(id)] != nil })))
	}
	for _, id := range keys(get(values, "rule_actions")) {
		if rules[id] == nil {
			del(get(values, "rule_actions"), id)
		}
	}
	set(values, "rule_dns_follow", clone(get(pruned, "rule_dns_follow")))
	set(values, "rule_dns_parked", clone(get(pruned, "rule_dns_parked")))
	catalog := []UpstreamCandidate{}
	for _, id := range order {
		if strings.HasPrefix(choices[id], "upstream:") {
			catalog, err = viewUpstreams(settings, view, ProfileAgents(after))
			if err != nil {
				return RuleDNSUpdate{}, err
			}
			break
		}
	}
	follow := get(values, "rule_dns_follow")
	for _, id := range order {
		choice := choices[id]
		action, err := resolveDNSChoice(choice, rules[id], values, catalog, afterRoutes)
		if err != nil {
			return RuleDNSUpdate{}, err
		}
		placeDNSChoice(values, rules[id], action)
		if choice == "follow" {
			if !contains(follow, S(id)) {
				follow.Content = append(follow.Content, S(id))
			}
		} else {
			follow.Content = filter(follow.Content, func(value *Node) bool { return !isOneOf(value, id) })
		}
	}
	for _, id := range ruleDNSIDs(follow) {
		if _, assigned := choices[id]; assigned || previous[id] == nil || rules[id] == nil {
			continue
		}
		route := followRoute(rules[id], afterRoutes)
		if route != followRoute(previous[id], beforeRoutes) {
			var action *Node
			if route != "" {
				action = M("action", route)
			}
			placeDNSChoice(values, rules[id], action)
		}
	}
	fields := append(append([]string{}, Selectors...), ruleDNSFields...)
	if !native {
		if err := normalizeRuleDNS(values); err != nil {
			return RuleDNSUpdate{}, err
		}
		for _, key := range fields {
			if !equal(get(values, key), get(stored, key)) || dirtyDNSField(settings, stored, key) {
				set(out, key, clone(get(values, key)))
			}
		}
		return finish([]string{}), nil
	}
	active := map[string]bool{}
	for _, item := range after.activeItems() {
		active[str(get(item, "id"))] = true
	}
	for _, id := range ruleDNSIDs(follow) {
		if !active[id] {
			placeDNSChoice(values, rules[id], nil)
		}
	}
	changes := M()
	installDNSFields(changes, values, append(append([]string{}, Selectors...), "rule_actions", "rule_dns_follow"))
	source := clone(settings)
	installDNSFields(source, values, []string{"rule_dns_parked", "rule_dns_follow"})
	document, resultSettings, err := ApplyRouting(view.Document, source, after, changes, options.BaseURL, RoutingOptions{BeforeConfig: before})
	if err == nil {
		if issue := newValidationIssue(view.Document, document); issue != nil {
			err = fail(fmt.Sprintf(messageDNSInvalidResult, issue.Message))
		}
	}
	if err != nil {
		if reason, ok := IsSettingsError(err); ok {
			return locked(reason)
		}
		return RuleDNSUpdate{}, err
	}
	if !same(document, view.Document) {
		if err := setNativeConfig(out, document); err != nil {
			return RuleDNSUpdate{}, err
		}
	}
	installDNSFields(out, resultSettings, fields)
	return finish([]string{}), nil
}

// InvalidateFollow copies replacement settings, removing follow marks only when
// an active rule's actual route changes. Locked drafts retain all intent markers.
func InvalidateFollow(p *Profile, settings *Node) (*Node, error) {
	if p == nil {
		p = NewProfile(nil)
	}
	out := clone(settings)
	if !isSeq(get(settings, "rule_dns_follow")) || len(items(get(settings, "rule_dns_follow"))) == 0 {
		return out, nil
	}
	old, err := readRuleDNS(p, p.Settings())
	if err != nil {
		return nil, err
	}
	next, err := readRuleDNS(p, settings)
	if err != nil {
		return nil, err
	}
	if !old.Editable || !next.Editable {
		return out, nil
	}
	active := map[string]bool{}
	for _, item := range p.activeItems() {
		active[str(get(item, "id"))] = true
	}
	set(out, "rule_dns_follow", seqOf(filter(items(get(settings, "rule_dns_follow")), func(id *Node) bool {
		return !active[str(id)] || ruleRoute(old.Values, str(id)) == ruleRoute(next.Values, str(id))
	})))
	return out, nil
}
