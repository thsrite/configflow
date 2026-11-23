package oxidns

import (
	"slices"
	"strings"
)

// actionMap is an insertion-ordered map of rule IDs to RuleAction values.
type actionMap struct {
	order []string
	items map[string]RuleAction
}

func newActionMap() *actionMap {
	return &actionMap{items: map[string]RuleAction{}}
}

func (m *actionMap) len() int {
	if m == nil {
		return 0
	}
	return len(m.order)
}

func (m *actionMap) has(id string) bool {
	if m == nil {
		return false
	}
	_, ok := m.items[id]
	return ok
}

func (m *actionMap) get(id string) (RuleAction, bool) {
	if m == nil {
		return RuleAction{}, false
	}
	a, ok := m.items[id]
	return a, ok
}

func (m *actionMap) set(id string, action RuleAction) {
	if m.items == nil {
		m.items = map[string]RuleAction{}
	}
	if _, ok := m.items[id]; !ok {
		m.order = append(m.order, id)
	}
	m.items[id] = action
}

func (m *actionMap) setDefault(id string, action RuleAction) {
	if !m.has(id) {
		m.set(id, action)
	}
}

func (m *actionMap) delete(id string) (RuleAction, bool) {
	if m == nil {
		return RuleAction{}, false
	}
	a, ok := m.items[id]
	if !ok {
		return RuleAction{}, false
	}
	delete(m.items, id)
	m.order = removeString(m.order, id)
	return a, true
}

func (m *actionMap) keys() []string {
	if m == nil {
		return nil
	}
	return slices.Clone(m.order)
}

func (m *actionMap) clone() *actionMap {
	out := newActionMap()
	if m == nil {
		return out
	}
	out.order = slices.Clone(m.order)
	for _, id := range m.order {
		a := m.items[id]
		if a.Upstream != nil {
			a.Upstream = clone(a.Upstream)
		}
		if a.raw != nil {
			a.raw = clone(a.raw)
		}
		out.items[id] = a
	}
	return out
}

func (m *actionMap) node() *Node {
	out := M()
	if m == nil {
		return out
	}
	for _, id := range m.order {
		set(out, id, m.items[id].node())
	}
	return out
}

func (m *actionMap) equal(other *actionMap) bool {
	return equal(m.node(), other.node())
}

func actionMapFromNode(n *Node) *actionMap {
	out := newActionMap()
	pairs(n, func(id string, value *Node) bool {
		if !isNone(value) {
			a := actionOf(value)
			if a.Upstream != nil {
				a.Upstream = clone(a.Upstream)
			}
			out.set(id, a)
		}
		return true
	})
	return out
}

// ruleDNSValues holds the typed per-rule DNS routing selections and metadata:
// the four selector lists, explicit rule_actions, rule_dns_follow, and
// rule_dns_parked.
type ruleDNSValues struct {
	DirectRules    []string
	ProxyRules     []string
	DirectRulesets []string
	ProxyRulesets  []string
	Actions        *actionMap
	Follow         []string
	Parked         *actionMap
}

func newRuleDNSValues() *ruleDNSValues {
	return &ruleDNSValues{
		DirectRules:    []string{},
		ProxyRules:     []string{},
		DirectRulesets: []string{},
		ProxyRulesets:  []string{},
		Actions:        newActionMap(),
		Follow:         []string{},
		Parked:         newActionMap(),
	}
}

func (v *ruleDNSValues) clone() *ruleDNSValues {
	if v == nil {
		return newRuleDNSValues()
	}
	return &ruleDNSValues{
		DirectRules:    slices.Clone(v.DirectRules),
		ProxyRules:     slices.Clone(v.ProxyRules),
		DirectRulesets: slices.Clone(v.DirectRulesets),
		ProxyRulesets:  slices.Clone(v.ProxyRulesets),
		Actions:        v.Actions.clone(),
		Follow:         slices.Clone(v.Follow),
		Parked:         v.Parked.clone(),
	}
}

func (v *ruleDNSValues) selector(key string) []string {
	switch key {
	case "direct_rules":
		return v.DirectRules
	case "proxy_rules":
		return v.ProxyRules
	case "direct_rulesets":
		return v.DirectRulesets
	case "proxy_rulesets":
		return v.ProxyRulesets
	}
	return nil
}

func (v *ruleDNSValues) setSelector(key string, ids []string) {
	if ids == nil {
		ids = []string{}
	}
	switch key {
	case "direct_rules":
		v.DirectRules = ids
	case "proxy_rules":
		v.ProxyRules = ids
	case "direct_rulesets":
		v.DirectRulesets = ids
	case "proxy_rulesets":
		v.ProxyRulesets = ids
	}
}

func (v *ruleDNSValues) appendSelector(key, id string) {
	v.setSelector(key, append(v.selector(key), id))
}

func (v *ruleDNSValues) removeSelector(key, id string) {
	v.setSelector(key, removeString(v.selector(key), id))
}

func (v *ruleDNSValues) hasInSelector(key, id string) bool {
	return slices.Contains(v.selector(key), id)
}

func (v *ruleDNSValues) routeOf(id string) string {
	if action, ok := v.Actions.get(id); ok {
		return action.Key()
	}
	if slices.Contains(v.DirectRules, id) || slices.Contains(v.DirectRulesets, id) {
		return "direct"
	}
	if slices.Contains(v.ProxyRules, id) || slices.Contains(v.ProxyRulesets, id) {
		return "proxy"
	}
	return ""
}

func (v *ruleDNSValues) fieldNode(key string) *Node {
	switch key {
	case "direct_rules":
		return strs(v.DirectRules)
	case "proxy_rules":
		return strs(v.ProxyRules)
	case "direct_rulesets":
		return strs(v.DirectRulesets)
	case "proxy_rulesets":
		return strs(v.ProxyRulesets)
	case "rule_actions":
		return v.Actions.node()
	case "rule_dns_follow":
		return strs(v.Follow)
	case "rule_dns_parked":
		return v.Parked.node()
	}
	return Null()
}

func (v *ruleDNSValues) fieldEqual(other *ruleDNSValues, key string) bool {
	switch key {
	case "direct_rules", "proxy_rules", "direct_rulesets", "proxy_rulesets":
		return slices.Equal(v.selector(key), other.selector(key))
	case "rule_dns_follow":
		return slices.Equal(v.Follow, other.Follow)
	case "rule_actions":
		return v.Actions.equal(other.Actions)
	case "rule_dns_parked":
		return v.Parked.equal(other.Parked)
	}
	return false
}

func (v *ruleDNSValues) hasAny() bool {
	return len(v.DirectRules) > 0 || len(v.ProxyRules) > 0 || len(v.DirectRulesets) > 0 || len(v.ProxyRulesets) > 0 ||
		v.Actions.len() > 0 || len(v.Follow) > 0 || v.Parked.len() > 0
}

// ruleDNSFromSettings extracts selector lists and rule_actions from an already
// normalized settings object (used by template generation and downloads).
func ruleDNSFromSettings(settings *Node) *ruleDNSValues {
	v := newRuleDNSValues()
	for _, key := range Selectors {
		node := get(settings, key)
		switch {
		case isSeq(node):
			v.setSelector(key, ruleDNSIDs(node))
		case isMap(node):
			v.setSelector(key, keys(node))
		}
	}
	v.Actions = actionMapFromNode(get(settings, "rule_actions"))
	v.Follow = uniqueStrings(ruleDNSIDs(get(settings, "rule_dns_follow")))
	v.Parked = actionMapFromNode(get(settings, "rule_dns_parked"))
	return v
}

// validateRuleDNS checks the follow exclusivity invariants of ruleDNSValues.
func validateRuleDNS(v *ruleDNSValues) error {
	v.Follow = uniqueStrings(v.Follow)
	for _, identity := range v.Follow {
		if action, ok := v.Actions.get(identity); ok {
			return fail("规则 " + identity + " 已设置 DNS 处理「" + RouteLabels[action.Action] + "」，不能同时跟随策略")
		}
		if v.Parked.has(identity) {
			return fail("规则 " + identity + " 已暂存停用前的 DNS 处理，不能同时跟随策略")
		}
	}
	return nil
}

// routingValues holds the complete recognized routing controls of a native
// document: ruleDNSValues plus aggregate_rules, default_forward and custom
// matches.
type routingValues struct {
	ruleDNSValues
	AggregateRules      *bool
	DefaultForward      *string
	CustomMatches       []*Node
	CustomMatchPosition *string
}

func newRoutingValues(settings *Node, p *Profile) *routingValues {
	return &routingValues{
		ruleDNSValues: ruleDNSValues{
			DirectRules:    []string{},
			ProxyRules:     []string{},
			DirectRulesets: []string{},
			ProxyRulesets:  []string{},
			Actions:        newActionMap(),
			Follow:         storedFollow(settings, p),
			Parked:         newActionMap(),
		},
		CustomMatches: []*Node{},
	}
}

func (v *routingValues) clone() *routingValues {
	if v == nil {
		return &routingValues{ruleDNSValues: *newRuleDNSValues()}
	}
	out := &routingValues{
		ruleDNSValues: *v.ruleDNSValues.clone(),
		CustomMatches: make([]*Node, len(v.CustomMatches)),
	}
	if v.AggregateRules != nil {
		b := *v.AggregateRules
		out.AggregateRules = &b
	}
	if v.DefaultForward != nil {
		s := *v.DefaultForward
		out.DefaultForward = &s
	}
	if v.CustomMatchPosition != nil {
		s := *v.CustomMatchPosition
		out.CustomMatchPosition = &s
	}
	for i, m := range v.CustomMatches {
		out.CustomMatches[i] = clone(m)
	}
	return out
}

func (v *routingValues) node() *Node {
	out := M()
	for _, key := range Selectors {
		set(out, key, strs(v.selector(key)))
	}
	if v.AggregateRules != nil {
		set(out, "aggregate_rules", B(*v.AggregateRules))
	} else {
		set(out, "aggregate_rules", Null())
	}
	if v.DefaultForward != nil {
		set(out, "default_forward", S(*v.DefaultForward))
	} else {
		set(out, "default_forward", Null())
	}
	custom := L()
	for _, entry := range v.CustomMatches {
		custom.Content = append(custom.Content, clone(entry))
	}
	set(out, "custom_matches", custom)
	if v.CustomMatchPosition != nil {
		set(out, "custom_match_position", S(*v.CustomMatchPosition))
	} else {
		set(out, "custom_match_position", Null())
	}
	set(out, "rule_actions", v.Actions.node())
	set(out, "rule_dns_follow", strs(v.Follow))
	return out
}

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }

func removeString(in []string, target string) []string {
	for i, s := range in {
		if s == target {
			out := make([]string, 0, len(in)-1)
			out = append(out, in[:i]...)
			out = append(out, in[i+1:]...)
			return out
		}
	}
	return in
}

func filterStrings(in []string, keep func(string) bool) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if keep(s) {
			out = append(out, s)
		}
	}
	return out
}

func routePrefix(selectorKey string) string {
	prefix, _, _ := strings.Cut(selectorKey, "_")
	return prefix
}
