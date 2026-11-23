package oxidns

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"configflow/internal/jsondecode"
	"configflow/internal/model"
)

// ErrRoutingLocked means a native DNS routing graph cannot be safely rewritten.
var ErrRoutingLocked = errors.New("OxiDNS routing is locked")

// RuleDNSMetadataFields returns the action, parked-action and follow storage keys.
// RoutingFields also includes selectors, but its RuleDNSFields omits parked data.
func RuleDNSMetadataFields() []string { return append([]string{}, ruleDNSFields...) }

// Convertible reports whether a rule can participate in domain DNS routing.
func Convertible(item model.RuleConfig) bool { return convertible(item) }

// HasIPPart reports known or possible IP content, for legacy routing recognition.
func HasIPPart(item model.RuleConfig, p *Profile) bool { return hasIPPart(item, p) }

// SettingsHead is the editor mode, native draft and follow marks of a stored
// settings object. Members of the wrong type read as empty; FollowOK reports
// that rule_dns_follow is a list of text values.
type SettingsHead struct {
	Mode         string
	NativeConfig string
	Follow       []string
	FollowOK     bool
}

// ReadSettingsHead decodes the head of a stored settings member.
func ReadSettingsHead(stored model.Dynamic) (SettingsHead, error) {
	var w struct {
		Mode         member[string] `json:"config_mode"`
		NativeConfig member[string] `json:"native_config"`
		Follow       texts          `json:"rule_dns_follow"`
	}
	if data := stored.JSON(); data != nil {
		var object map[string]json.RawMessage
		if jsondecode.Unmarshal(data, &object) == nil {
			if err := jsondecode.Unmarshal(data, &w); err != nil {
				return SettingsHead{}, fmt.Errorf("decode OxiDNS settings: %w", err)
			}
		}
	}
	follow, ok := textList(w.Follow)
	return SettingsHead{Mode: w.Mode.Value, NativeConfig: w.NativeConfig.Value, Follow: follow, FollowOK: ok}, nil
}

// WithoutRuleReferences returns settings (an object; anything else counts as
// empty) without rule id in the route selectors and per-rule DNS metadata.
// Selector lists keep their other values; a null selector becomes empty.
func WithoutRuleReferences(settings *Node, id string) *Node {
	out := M()
	if isMap(settings) {
		out = clone(settings)
	}
	for _, key := range []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"} {
		value := get(out, key)
		if !has(out, key) || !isSeq(value) && !isNone(value) {
			continue
		}
		kept := L()
		for _, item := range items(value) {
			if !isStr(item) || item.Value != id {
				kept.Content = append(kept.Content, item)
			}
		}
		set(out, key, kept)
	}
	for _, key := range ruleDNSFields {
		value := get(out, key)
		switch {
		case isMap(value):
			del(value, id)
		case isSeq(value) && allStrings(value):
			kept := L()
			for _, item := range value.Content {
				if item.Value != id {
					kept.Content = append(kept.Content, item)
				}
			}
			set(out, key, kept)
		}
	}
	return out
}

// IsIPRuleType identifies connection-only IP rules for rules-page validation.
func IsIPRuleType(kind string) bool { return kind == "IP-CIDR" || kind == "IP-CIDR6" }

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

// Error returns the user-visible conflict reason.
func (e *RoutingLockedError) Error() string { return e.Reason }

// Unwrap lets callers recognize a routing conflict with errors.Is.
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

func dnsRuleMap(p *Profile) map[string]model.RuleConfig {
	out := map[string]model.RuleConfig{}
	for _, item := range p.ruleConfigs() {
		out[item.ID] = item
	}
	return out
}

// ruleDependency is what one rule contributes to DNS routing decisions.
type ruleDependency struct {
	id, itemType, ruleType, value string
	behavior, url                 *string
	enabled, libraryEnabled       bool
	route                         string
}

func (d ruleDependency) equal(o ruleDependency) bool {
	sameText := func(a, b *string) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	return d.id == o.id && d.itemType == o.itemType && d.ruleType == o.ruleType && d.value == o.value &&
		sameText(d.behavior, o.behavior) && sameText(d.url, o.url) &&
		d.enabled == o.enabled && d.libraryEnabled == o.libraryEnabled && d.route == o.route
}

func routingDependencies(p *Profile, routes map[string]string) []ruleDependency {
	out := []ruleDependency{}
	for _, item := range p.ruleConfigs() {
		out = append(out, ruleDependency{item.ID, item.ItemType, item.RuleType, model.TextValue(item.Value), item.Behavior, item.URL,
			model.Flag(item.Enabled, true), model.Flag(item.LibraryEnabled, true), followRoute(item, routes)})
	}
	return out
}

func sameDependencies(a, b []ruleDependency) bool {
	return slices.EqualFunc(a, b, ruleDependency.equal)
}

func pruneDNSFields(values *ruleDNSValues, rules map[string]model.RuleConfig) *ruleDNSValues {
	out := newRuleDNSValues()
	for _, id := range values.Actions.order {
		if _, ok := rules[id]; ok {
			a := values.Actions.items[id]
			if a.Upstream != nil {
				a.Upstream = clone(a.Upstream)
			}
			out.Actions.set(id, a)
		}
	}
	for _, id := range values.Parked.order {
		if _, ok := rules[id]; ok {
			a := values.Parked.items[id]
			if a.Upstream != nil {
				a.Upstream = clone(a.Upstream)
			}
			out.Parked.set(id, a)
		}
	}
	out.Follow = filterStrings(values.Follow, func(id string) bool {
		_, ok := rules[id]
		return ok
	})
	return out
}

func dirtyDNSField(settings *Node, stored *ruleDNSValues, key string) bool {
	value := get(settings, key)
	return !isNone(value) && !equal(value, stored.fieldNode(key))
}

func installDNSFields(settings *Node, values *ruleDNSValues, fields []string) {
	for _, key := range fields {
		set(settings, key, values.fieldNode(key))
	}
}

func placeDNSChoice(values *ruleDNSValues, item model.RuleConfig, action *RuleAction) {
	id := item.ID
	for _, key := range Selectors {
		values.setSelector(key, filterStrings(values.selector(key), func(value string) bool { return value != id }))
	}
	values.Actions.delete(id)
	values.Parked.delete(id)
	if action == nil {
		return
	}
	if isListRoute(action.Action) {
		key := action.Action + "_" + suffixOf(item)
		values.appendSelector(key, id)
	} else {
		copied := *action
		if copied.Upstream != nil {
			copied.Upstream = clone(copied.Upstream)
		}
		values.Actions.set(id, copied)
	}
}

func resolveDNSChoice(choice string, item model.RuleConfig, values *ruleDNSValues, catalog []UpstreamCandidate, routes map[string]string) (*RuleAction, error) {
	switch choice {
	case "default":
		return nil, nil
	case "follow":
		if route := followRoute(item, routes); route != "" {
			return &RuleAction{Action: route}, nil
		}
		return nil, nil
	}
	if !strings.HasPrefix(choice, "upstream:") {
		return &RuleAction{Action: choice}, nil
	}
	id := item.ID
	current, ok := values.Actions.get(id)
	if !ok {
		current, ok = values.Parked.get(id)
	}
	if ok && current.Key() == choice {
		copied := current
		if copied.Upstream != nil {
			copied.Upstream = clone(copied.Upstream)
		}
		return &copied, nil
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
		return &RuleAction{Action: "upstream", Source: candidate.Source, Upstream: entry}, nil
	}
	return nil, fail(messageDNSStaleUpstream)
}

type validationKey struct {
	path, code, message string
}

func validationIssueKey(issue Issue) validationKey {
	path, _ := json.Marshal(issue.Path)
	return validationKey{string(path), issue.Code, issue.Message}
}

func newValidationIssue(before, after *Node) *Issue {
	existing := map[validationKey]bool{}
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
		before = emptyProfile()
	}
	if after == nil {
		after = emptyProfile()
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
	}
	for _, id := range order {
		item, ok := rules[id]
		if !ok {
			return RuleDNSUpdate{}, fail(fmt.Sprintf(messageDNSMissingRule, id))
		}
		if choices[id] != "default" && !convertible(item) {
			kind := item.RuleType
			if kind == "" {
				kind = model.TextValue(item.Behavior)
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
	if len(order) == 0 && sameDependencies(routingDependencies(before, beforeRoutes), routingDependencies(after, afterRoutes)) {
		for _, key := range ruleDNSFields {
			if !pruned.fieldEqual(stored, key) || dirtyDNSField(settings, stored, key) {
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
	native := readSettingsView(settings).isNative()
	if native && view.Document == nil {
		return locked(view.Reason)
	}
	values := view.Values.clone()
	for _, key := range Selectors {
		values.setSelector(key, filterStrings(values.selector(key), func(id string) bool {
			_, ok := rules[id]
			return ok
		}))
	}
	for _, id := range values.Actions.keys() {
		if _, ok := rules[id]; !ok {
			values.Actions.delete(id)
		}
	}
	values.Follow = slices.Clone(pruned.Follow)
	values.Parked = pruned.Parked.clone()
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
	for _, id := range order {
		choice := choices[id]
		action, err := resolveDNSChoice(choice, rules[id], values, catalog, afterRoutes)
		if err != nil {
			return RuleDNSUpdate{}, err
		}
		placeDNSChoice(values, rules[id], action)
		if choice == "follow" {
			if !slices.Contains(values.Follow, id) {
				values.Follow = append(values.Follow, id)
			}
		} else {
			values.Follow = filterStrings(values.Follow, func(value string) bool { return value != id })
		}
	}
	for _, id := range values.Follow {
		_, assigned := choices[id]
		_, existed := previous[id]
		if _, exists := rules[id]; assigned || !existed || !exists {
			continue
		}
		route := followRoute(rules[id], afterRoutes)
		if route != followRoute(previous[id], beforeRoutes) {
			var action *RuleAction
			if route != "" {
				action = &RuleAction{Action: route}
			}
			placeDNSChoice(values, rules[id], action)
		}
	}
	fields := append(append([]string{}, Selectors...), ruleDNSFields...)
	if !native {
		if err := validateRuleDNS(values); err != nil {
			return RuleDNSUpdate{}, err
		}
		for _, key := range fields {
			if !values.fieldEqual(stored, key) || dirtyDNSField(settings, stored, key) {
				set(out, key, values.fieldNode(key))
			}
		}
		return finish([]string{}), nil
	}
	active := map[string]bool{}
	for _, item := range after.activeItems() {
		active[item.ID] = true
	}
	for _, id := range values.Follow {
		if !active[id] {
			placeDNSChoice(values, rules[id], nil)
		}
	}
	actions := make([]RuleActionEntry, 0, values.Actions.len())
	for _, id := range values.Actions.order {
		actions = append(actions, RuleActionEntry{RuleID: id, Action: values.Actions.items[id]})
	}
	changes := RoutingChanges{
		DirectRules:    &values.DirectRules,
		ProxyRules:     &values.ProxyRules,
		DirectRulesets: &values.DirectRulesets,
		ProxyRulesets:  &values.ProxyRulesets,
		RuleActions:    &actions,
		RuleDNSFollow:  &values.Follow,
	}
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
	for _, key := range fields {
		set(out, key, clone(get(resultSettings, key)))
	}
	return finish([]string{}), nil
}

// InvalidateFollow copies replacement settings, removing follow marks only when
// an active rule's actual route changes. Locked drafts retain all intent markers.
func InvalidateFollow(p *Profile, settings *Node) (*Node, error) {
	if p == nil {
		p = emptyProfile()
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
		active[item.ID] = true
	}
	set(out, "rule_dns_follow", seqOf(filter(items(get(settings, "rule_dns_follow")), func(id *Node) bool {
		return !active[str(id)] || ruleRoute(old.Values, str(id)) == ruleRoute(next.Values, str(id))
	})))
	return out, nil
}
