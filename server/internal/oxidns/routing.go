package oxidns

import (
	"configflow/internal/model"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Reading and editing the recognizable routing part of a native document.
// The native providers and sequence are the source of truth; unrecognized
// routing is left to the complete editor. Per-rule DNS actions are rule
// groups too, on generated providers nothing else uses; their helper plugins
// are created and removed with the groups and never overwritten when edited
// by hand.

// Routing field names.
var (
	Selectors     = []string{"direct_rules", "proxy_rules", "direct_rulesets", "proxy_rulesets"}
	RuleDNSFields = []string{"rule_actions", "rule_dns_follow"}
	RoutingFields = append(append(append([]string{}, Selectors...), RuleDNSFields...), "aggregate_rules", "default_forward", "custom_matches", "custom_match_position")
	customFields  = []string{"custom_matches", "custom_match_position"}
)

const (
	routingReason = "当前分流执行链包含自定义结构，请在完整参数中编辑。"
	missingRule   = "所选规则不在当前空间的可用规则库中，请刷新后重试。"
)

var (
	helperTagRE   = regexp.MustCompile(`^(?:` + BlackholeTag + `|cf_dns_(?:up|fwd|cache)_([0-9a-f]{12}))$`)
	helperExecRE  = regexp.MustCompile(`^(?:\$|goto |jump )cf_dns_`)
	domainTargets = map[string]string{"goto china_dns": "direct", "goto proxy_dns_seq": "proxy"}
)

// Capability says whether a field can be edited here, and why not.
type Capability struct {
	Editable bool
	Reason   string
	Target   string
	Absent   bool
	routing  bool // routing capabilities always carry reason and target
}

func (c Capability) MarshalJSON() ([]byte, error) {
	out := map[string]any{"editable": c.Editable}
	if c.routing || c.Reason != "" {
		out["reason"] = c.Reason
	}
	if c.routing || c.Target != "" {
		out["target"] = c.Target
	}
	if c.Absent {
		out["absent"] = true
	}
	return json.Marshal(out)
}

func routingCap(editable bool, reason string) Capability {
	return Capability{Editable: editable, Reason: reason, Target: "sequence_main", routing: true}
}

// refersTo is _references: a string anywhere in value mentions tag.
func refersTo(value *Node, tag string) bool {
	switch {
	case isStr(value):
		return mentions(value.Value, tag)
	case isMap(value):
		found := false
		pairs(value, func(_ string, v *Node) bool { found = refersTo(v, tag); return !found })
		return found
	case isSeq(value):
		for _, item := range value.Content {
			if refersTo(item, tag) {
				return true
			}
		}
	}
	return false
}

// positions are the indexes of the values that refer to tag.
func positions(values []*Node, tag string) []int {
	var out []int
	for i, v := range values {
		if refersTo(v, tag) {
			out = append(out, i)
		}
	}
	return out
}

func withoutPlugins(document *nativeDoc) *Node {
	top := M()
	pairs(document, func(key string, value *Node) bool {
		if key != "plugins" {
			set(top, key, value)
		}
		return true
	})
	return top
}

func entryMatches(entry *Node) *Node {
	value := get(entry, "matches")
	if isStr(value) {
		return L(value)
	}
	return value
}

func singleMatch(entry *Node) (string, bool) {
	values := entryMatches(entry)
	if isSeq(values) && len(values.Content) == 1 && isStr(values.Content[0]) {
		return values.Content[0].Value, true
	}
	return "", false
}

var qtypeMatch = regexp.MustCompile(`^qtype(?:\s+[0-9]+)+$`)

// prefixEnd skips the recognized built-ins at the head of sequence_main.
func prefixEnd(args []*Node) int {
	index := 0
	for index < len(args) {
		item := args[index]
		action := get(item, "exec")
		match, single := singleMatch(item)
		switch {
		case equal(item, WatchdogRule()):
			index++
		case len(items(get(item, "matches"))) == 0 && (isOneOf(action, "prefer_ipv4", "$query_recorder") || strings.HasPrefix(str(action), "metrics_collector ")):
			index++
		case isOneOf(action, blockQtypesExecs...) && single && qtypeMatch.MatchString(match):
			index++
		case equal(action, S("$custom_hosts")) && len(items(get(item, "matches"))) == 0 && index+1 < len(args) &&
			equal(get(args[index+1], "exec"), S("accept")) && func() bool { m, ok := singleMatch(args[index+1]); return ok && m == "has_resp" }():
			index += 2
		case equal(action, S("goto adguard_reject")) && single && match == "question $adguard_rules":
			index++
		case equal(item, policyHook):
			index++ // device policy keeps its hook ahead of the domain rules
		default:
			return index
		}
	}
	return index
}

// PrefixEnd is the index after the built-in head of sequence_main entries.
func PrefixEnd(args *Node) int { return prefixEnd(items(args)) }

// ---- rule catalog ----

type ruleRecord struct {
	suffix string
	parts  map[string]bool
	shapes map[string]bool
	item   model.RuleConfig
}

type ruleCatalog struct {
	order   []string
	records map[string]*ruleRecord
	atoms   map[string]map[string]bool
}

func atomKey(part, key string, value *Node) (string, bool) {
	if !isStr(value) {
		return "", false
	}
	text := value.Value
	if key == "files" {
		text = strings.TrimPrefix(text, "./")
	}
	return part + "\x00" + key + "\x00" + text, true
}

func shapeKey(parts map[string]bool) string {
	var list []string
	for p := range parts {
		list = append(list, p)
	}
	sort.Strings(list)
	return strings.Join(list, ",")
}

func selectorSettings() *Node {
	s := M()
	for _, key := range Selectors {
		set(s, key, L())
	}
	return s
}

// catalog is each rule's exact inline expression or generated resource path.
func (p *Profile) catalog() *ruleCatalog {
	c := &ruleCatalog{records: map[string]*ruleRecord{}, atoms: map[string]map[string]bool{}}
	list := p.activeItems()
	same := map[string][]model.RuleConfig{}
	for _, item := range list {
		key := item.ID + "\x00" + suffixOf(item)
		same[key] = append(same[key], item)
	}
	for _, item := range list {
		id := item.ID
		if c.records[id] != nil {
			continue
		}
		suffix := suffixOf(item)
		values := newRuleDNSValues()
		values.setSelector("direct_"+suffix, []string{id})
		groups, err := p.routingGroups(values, false, legacyAll, same[id+"\x00"+suffix])
		if err != nil {
			continue
		}
		parts := map[string]bool{}
		for _, part := range []string{"domain", "ip"} {
			for _, g := range *groups.part(part) {
				pairs(g.Args, func(key string, values *Node) bool {
					for _, value := range items(values) {
						if atom, ok := atomKey(part, key, value); ok {
							if c.atoms[atom] == nil {
								c.atoms[atom] = map[string]bool{}
							}
							c.atoms[atom][id] = true
							parts[part] = true
						}
					}
					return true
				})
			}
		}
		current := map[string]bool{}
		if suffix == "rulesets" {
			for _, part := range managedRulesetParts(behaviorOf(item)) {
				current[part] = true
			}
		} else {
			current["domain"] = parts["domain"]
			if !parts["domain"] {
				delete(current, "domain")
			}
		}
		aware := map[string]bool{"domain": parts["domain"], "ip": parts["ip"] && hasIPPart(item, p)}
		for k, v := range aware {
			if !v {
				delete(aware, k)
			}
		}
		c.order = append(c.order, id)
		c.records[id] = &ruleRecord{suffix, parts,
			map[string]bool{shapeKey(parts): true, shapeKey(aware): true, shapeKey(current): true}, item}
	}
	return c
}

func (c *ruleCatalog) owners(part, key string, value *Node) map[string]bool {
	atom, ok := atomKey(part, key, value)
	if !ok {
		return nil
	}
	return c.atoms[atom]
}

func only(set map[string]bool) string {
	for k := range set {
		return k
	}
	return ""
}

// ---- recognition ----

type recGroup struct {
	index    int
	tag      string
	route    string
	args     *Node
	entry    *Node
	provider *Node
	action   *RuleAction
}

type recGroups struct{ Domain, IP []*recGroup }

func (g *recGroups) part(name string) *[]*recGroup {
	if name == "ip" {
		return &g.IP
	}
	return &g.Domain
}

func (g *recGroups) all() []*recGroup { return append(append([]*recGroup{}, g.Domain...), g.IP...) }

type routingState struct {
	values        *routingValues
	caps          map[string]Capability
	full          bool
	plugins       map[string]*Node
	main          *Node
	groups        recGroups
	prefix        []*Node
	dynamic       []*Node
	custom        []*Node
	final         *Node
	records       *ruleCatalog
	claimed       bool
	customIndices []int
	actualMainTag string
}

// knownRules is every rule of the space by id (disabled ones included).
func (p *Profile) knownRules() (map[string]*model.RuleConfig, []string) {
	list := p.ruleConfigs()
	if len(list) == 0 {
		list = p.activeItems()
	}
	known := map[string]*model.RuleConfig{}
	var order []string
	for i := range list {
		if id := list[i].ID; known[id] == nil {
			known[id] = &list[i]
			order = append(order, id)
		}
	}
	return known, order
}

func storedFollow(settings *Node, p *Profile) []string {
	follow := get(settings, "rule_dns_follow")
	known, _ := p.knownRules()
	out := []string{}
	if !isSeq(follow) {
		return out
	}
	for _, id := range uniqueStrings(ruleDNSIDs(follow)) {
		if known[id] != nil {
			out = append(out, id)
		}
	}
	return out
}

func ipSuffix(entries []*Node, defaultForward string, probe bool) []*Node {
	if len(entries) == 0 {
		return nil
	}
	if !probe {
		return append([]*Node{}, entries...)
	}
	last := M("exec", "drop_resp")
	if defaultForward == "forward_local" {
		last = M("matches", L("has_resp"), "exec", "goto default_direct_ip")
	}
	return append(append([]*Node{M("exec", "jump local_probe")}, entries...), last)
}

// ownedGroup: a generated provider only this sequence_main entry uses.
func ownedGroup(document *nativeDoc, args []*Node, plugins []*Node, entry *Node, tag string) bool {
	if !groupTagRE.MatchString(tag) || mapSize(entry) != 2 || !has(entry, "matches") || !has(entry, "exec") {
		return false
	}
	if len(positions(args, tag)) != 1 || refersTo(withoutPlugins(document), tag) {
		return false
	}
	for _, i := range positions(plugins, tag) {
		if !isOneOf(get(plugins[i], "tag"), "sequence_main", "rules_reload", tag) {
			return false
		}
	}
	return true
}

func uniqueAtoms(part string, provider *Node, c *ruleCatalog) bool {
	args := get(provider, "args")
	allowed := map[string]string{"domain": "exps", "ip": "ips"}[part]
	if !isMap(args) || mapSize(args) == 0 {
		return false
	}
	ok := true
	pairs(args, func(key string, values *Node) bool {
		if key != "files" && key != allowed || !isSeq(values) || len(values.Content) == 0 {
			ok = false
			return false
		}
		for _, value := range values.Content {
			if len(c.owners(part, key, value)) != 1 {
				ok = false
				return false
			}
		}
		return true
	})
	return ok
}

var providerMatch = regexp.MustCompile(`^(qname|resp_ip) \$(\S+)$`)

func (p *Profile) recognize(document *nativeDoc, settings *Node, claim bool, catalog func() *ruleCatalog) *routingState {
	state := &routingState{values: newRoutingValues(settings, p), caps: map[string]Capability{}}
	for _, key := range RoutingFields {
		state.caps[key] = routingCap(false, routingReason)
	}
	values, caps := state.values, state.caps
	plugins := pluginMap(document)
	main := plugins["sequence_main"]
	args := get(main, "args")
	if !equal(get(main, "type"), S("sequence")) || !isSeq(args) || len(args.Content) == 0 {
		return state
	}
	for _, item := range args.Content {
		if !isMap(item) || !isStr(get(item, "exec")) {
			return state
		}
	}
	entries := args.Content
	final := entries[len(entries)-1]
	if (len(items(get(final, "matches"))) > 0) || domainTargets[get(final, "exec").Value] == "" {
		return state
	}
	defaultForward := "forward_remote"
	if get(final, "exec").Value == "goto china_dns" {
		defaultForward = "forward_local"
	}
	values.DefaultForward = strPtr(defaultForward)
	caps["default_forward"] = routingCap(true, "")
	records := catalog()
	var pluginList []*Node
	for _, item := range items(get(document, "plugins")) {
		pluginList = append(pluginList, item)
	}
	if len(plugins) == 0 {
		pluginList = nil
	}
	var groups recGroups
	var dynamic []*recGroup
	middleEnd := len(entries) - 1
	recognized := true
	invalid, claimed := "", false
	for index, entry := range entries[:len(entries)-1] {
		match, _ := singleMatch(entry)
		exec := get(entry, "exec").Value
		if match == "qname $dynamic_domains" && domainTargets[exec] != "" {
			dynamic = append(dynamic, &recGroup{index: index, entry: entry})
			middleEnd = min(middleEnd, index)
			continue
		}
		if parsed := providerMatch.FindStringSubmatch(match); parsed != nil {
			part := map[string]string{"qname": "domain", "resp_ip": "ip"}[parsed[1]]
			provider := plugins[parsed[2]]
			route := ""
			if equal(get(provider, "type"), S(map[string]string{"domain": "domain_set", "ip": "ip_set"}[part])) {
				route = parseExec(get(entry, "exec"), part, plugins)
			}
			if route != "" && !isListRoute(route) {
				if claim && ownedGroup(document, entries, pluginList, entry, parsed[2]) && (isInvalid(route) || uniqueAtoms(part, provider, records)) {
					claimed = true
				} else {
					route = ""
				}
			}
			if route != "" {
				group := &recGroup{index: index, tag: parsed[2], route: route, args: get(provider, "args"), entry: entry, provider: provider}
				if provider == nil {
					group.provider = M()
				}
				if isInvalid(route) {
					if invalid == "" {
						invalid = route[1:]
					}
				} else if strings.HasPrefix(route, "upstream:") {
					if action, ok := pinnedRuleAction(strings.TrimPrefix(exec, "goto "+UpstreamPrefix), plugins); ok {
						group.action = &action
					}
				}
				*groups.part(part) = append(*groups.part(part), group)
				if part == "ip" {
					middleEnd = min(middleEnd, index)
				}
				continue
			}
		}
		if len(items(get(entry, "matches"))) == 0 && exec == "jump local_probe" {
			middleEnd = min(middleEnd, index)
		}
	}
	if len(dynamic) > 1 {
		return state
	}
	var ipEntries []*Node
	var ipRoutes []string
	for _, g := range groups.IP {
		ipEntries = append(ipEntries, g.entry)
		ipRoutes = append(ipRoutes, g.route)
	}
	candidates := [][]*Node{ipSuffix(ipEntries, defaultForward, true)}
	if !probeNeeded(ipRoutes, defaultForward) {
		candidates = append([][]*Node{ipSuffix(ipEntries, defaultForward, false)}, candidates...)
	}
	var head []*Node
	if len(dynamic) == 1 {
		head = []*Node{dynamic[0].entry}
	}
	matchedTail := false
	for _, candidate := range candidates {
		expected := append(append(append([]*Node{}, head...), candidate...), final)
		matchedTail = matchedTail || same(seqOf(entries[middleEnd:]), seqOf(expected))
	}
	if !matchedTail {
		caps["default_forward"] = routingCap(false, routingReason)
		return state
	}
	prefix := prefixEnd(entries[:middleEnd])
	domainIndices := map[int]bool{}
	for _, g := range groups.Domain {
		if g.index < prefix || g.index >= middleEnd {
			return state
		}
		domainIndices[g.index] = true
	}
	var customIndices []int
	var custom []*Node
	for i := prefix; i < middleEnd; i++ {
		if !domainIndices[i] {
			customIndices = append(customIndices, i)
			custom = append(custom, entries[i])
		}
	}
	customValid := true
	for _, entry := range custom {
		matches := entryMatches(entry)
		ok := isSeq(matches) && len(matches.Content) > 0 && strings.TrimSpace(get(entry, "exec").Value) != ""
		for _, m := range items(matches) {
			ok = ok && isStr(m) && strings.TrimSpace(m.Value) != ""
		}
		customValid = customValid && ok
	}
	position := "tail"
	if len(customIndices) > 0 && len(domainIndices) > 0 {
		minDomain, maxDomain := len(entries), -1
		for i := range domainIndices {
			minDomain, maxDomain = min(minDomain, i), max(maxDomain, i)
		}
		if customIndices[len(customIndices)-1] < minDomain {
			position = "head"
		} else if customIndices[0] <= maxDomain {
			customValid = false
		}
	}
	values.CustomMatches = make([]*Node, len(custom))
	for i, c := range custom {
		values.CustomMatches[i] = clone(c)
	}
	if customValid && len(custom) > 0 {
		values.CustomMatchPosition = strPtr(position)
	}
	if customValid {
		caps["custom_matches"] = routingCap(true, "")
		reason := ""
		switch {
		case len(custom) == 0:
			reason = "请先添加自定义匹配，再设置执行顺序。"
		case len(domainIndices) == 0:
			reason = "当前没有域名分流规则，两种执行顺序效果相同。"
		}
		caps["custom_match_position"] = routingCap(len(custom) > 0, reason)
	} else {
		recognized = false
	}
	if invalid != "" {
		// Rebuilding for a custom match edit would drop the hand-edited entry.
		recognized = false
		caps["custom_matches"] = routingCap(false, invalid)
		caps["custom_match_position"] = routingCap(false, invalid)
	}
	assigned := map[string]map[string]string{}
	var assignedOrder []string
	var orphans [][2]string
	actions := map[string]RuleAction{}
	for _, part := range []string{"domain", "ip"} {
		for _, g := range *groups.part(part) {
			if isInvalid(g.route) {
				continue
			}
			if !isListRoute(g.route) {
				action := RuleAction{Action: g.route}
				if g.action != nil {
					action = *g.action
				}
				actions[g.route] = action
			}
			allowed := map[string]string{"domain": "exps", "ip": "ips"}[part]
			if !isMap(g.args) || mapSize(g.args) == 0 || mapSize(g.entry) != 2 || !has(g.entry, "matches") {
				recognized = false
				continue
			}
			pairs(g.args, func(key string, list *Node) bool {
				if key != "files" && key != allowed {
					recognized = false
				}
				return true
			})
			if !recognized && len(filter(keyNodes(g.args), func(k *Node) bool { return k.Value != "files" && k.Value != allowed })) > 0 {
				continue
			}
			pairs(g.args, func(key string, list *Node) bool {
				if !isSeq(list) || len(list.Content) == 0 {
					recognized = false
					return true
				}
				for _, value := range list.Content {
					owners := records.owners(part, key, value)
					if len(owners) != 1 {
						recognized = false
						if len(owners) == 0 && isStr(value) {
							text := value.Value
							if key == "files" {
								text = strings.TrimPrefix(text, "./")
							}
							orphans = append(orphans, [2]string{key, text})
						}
						continue
					}
					identity := only(owners)
					previous := assigned[identity]
					if previous == nil {
						previous = map[string]string{}
						assigned[identity] = previous
						assignedOrder = append(assignedOrder, identity)
					}
					if _, ok := previous[part]; ok {
						recognized = false
					}
					previous[part] = g.route
				}
				return true
			})
		}
	}
	for _, identity := range assignedOrder {
		record, parts := records.records[identity], assigned[identity]
		routes := map[string]bool{}
		partSet := map[string]bool{}
		for part, route := range parts {
			routes[route] = true
			partSet[part] = true
		}
		if !record.shapes[shapeKey(partSet)] || len(routes) != 1 {
			recognized = false
			continue
		}
		route := only(routes)
		if isListRoute(route) {
			values.appendSelector(route+"_"+record.suffix, identity)
		} else {
			action := actions[route]
			if action.Upstream != nil {
				action.Upstream = clone(action.Upstream)
			}
			values.Actions.set(identity, action)
		}
	}
	for _, key := range Selectors {
		selected := values.selector(key)
		ordered := make([]string, 0, len(selected))
		for _, identity := range records.order {
			if slices.Contains(selected, identity) {
				ordered = append(ordered, identity)
			}
		}
		values.setSelector(key, ordered)
	}
	orderedActions := newActionMap()
	for _, identity := range records.order {
		if action, ok := values.Actions.get(identity); ok {
			orderedActions.set(identity, action)
		}
	}
	values.Actions = orderedActions
	var matches []bool
	if recognized {
		for _, aggregate := range []bool{true, false} {
			expected := p.observedLegacyGroups(&values.ruleDNSValues, assigned, records, aggregate)
			ok := true
			for _, part := range []string{"domain", "ip"} {
				got, want := *groups.part(part), *expected.part(part)
				ok = ok && len(got) == len(want)
				for i := 0; ok && i < len(got); i++ {
					ok = got[i].route == want[i].Route && same(got[i].args, want[i].Args)
				}
			}
			if ok {
				matches = append(matches, aggregate)
			}
		}
		recognized = len(matches) > 0
	}
	if recognized {
		if len(matches) == 1 {
			values.AggregateRules = boolPtr(matches[0])
		}
		for _, key := range append(append([]string{}, Selectors...), RuleDNSFields...) {
			caps[key] = routingCap(true, "")
		}
		reason := ""
		if len(matches) == 2 {
			reason = "当前没有相邻且同策略的规则，无需合并。"
		}
		caps["aggregate_rules"] = routingCap(len(matches) == 1, reason)
	} else {
		reason := invalid
		if reason == "" {
			reason = "规则资源或顺序无法唯一对应当前规则库，请在完整参数中编辑。"
			var files []string
			for _, o := range orphans {
				if o[0] == "files" {
					files = append(files, o[1])
				}
			}
			if hints := UnavailableRulesets(p, files); len(hints) > 0 {
				reason = "分流里有不可用的规则集，暂时不能在这里编辑。" + strings.Join(hints, "；") + "。"
			} else if len(orphans) > 0 {
				reason = "分流里有规则库中不存在的单条规则（" + orphans[0][1] + "），请在完整参数中删除后再编辑。"
			}
		}
		for _, key := range append(append([]string{}, Selectors...), append(RuleDNSFields, "aggregate_rules")...) {
			caps[key] = routingCap(false, reason)
		}
	}
	state.full, state.plugins, state.main, state.groups = true, plugins, main, groups
	state.prefix, state.custom, state.final = entries[:prefix], custom, final
	for _, d := range dynamic {
		state.dynamic = append(state.dynamic, d.entry)
	}
	state.records, state.claimed, state.customIndices = records, claimed, customIndices
	return state
}

// observedLegacyGroups validates old per-rule parts against the catalog.
func (p *Profile) observedLegacyGroups(values *ruleDNSValues, assigned map[string]map[string]string, c *ruleCatalog, aggregate bool) *routingGroups {
	raw, err := p.routingGroups(values, false, legacyAll, nil)
	expected := &routingGroups{}
	if err != nil {
		return expected
	}
	for _, part := range []string{"domain", "ip"} {
		for _, g := range *raw.part(part) {
			owners := map[string]bool{}
			pairs(g.Args, func(key string, list *Node) bool {
				for _, value := range items(list) {
					for identity := range c.owners(part, key, value) {
						owners[identity] = true
					}
				}
				return true
			})
			if len(owners) != 1 {
				continue
			}
			if _, ok := assigned[only(owners)][part]; !ok {
				continue
			}
			target := expected.part(part)
			if n := len(*target); aggregate && n > 0 && (*target)[n-1].Route == g.Route {
				pairs(g.Args, func(key string, list *Node) bool {
					l := setDefault((*target)[n-1].Args, key, L())
					l.Content = append(l.Content, list.Content...)
					return true
				})
				continue
			}
			*target = append(*target, &ruleGroup{g.Route, clone(g.Args), g.Tag})
		}
	}
	return expected
}

func cachedCatalog(p *Profile) func() *ruleCatalog {
	var c *ruleCatalog
	return func() *ruleCatalog {
		if c == nil {
			c = p.catalog()
		}
		return c
	}
}

// analyzeRouting is _analyze: recognition that never locks a document that
// was editable before rule DNS actions existed.
func (p *Profile) analyzeRouting(document *nativeDoc, settings *Node) *routingState {
	catalog := cachedCatalog(p)
	state := p.recognize(document, settings, true, catalog)
	if state.claimed {
		helper := false
		for _, entry := range items(get(state.main, "args")) {
			helper = helper || helperExecRE.MatchString(str(get(entry, "exec")))
		}
		if !helper {
			legacy := p.recognize(document, settings, false, catalog)
			if !state.caps["direct_rules"].Editable || !state.caps["custom_matches"].Editable && legacy.caps["custom_matches"].Editable {
				return legacy
			}
		}
	}
	return state
}

// managedTags are the tags the template owns (native apply's _MANAGED).
var managedTags = func() map[string]bool {
	m := map[string]bool{}
	for tag := range tagTypes {
		m[tag] = true
	}
	for _, tag := range strings.Fields("china_dns direct_ip default_direct_ip sequence_main remote_dns local_probe proxy_dns_seq ip_requery_proxy fallback_dns adguard_reject rules_refresh") {
		m[tag] = true
	}
	return m
}()

func tagType(tag string) string {
	if t, ok := tagTypes[tag]; ok {
		return t
	}
	return "sequence"
}

var referenceKeys = []string{"exec", "matches", "entry", "primary", "secondary", "provider", "executors", "args"}

// renameReferences rewrites explicit reference fields from old to new (never
// credentials or file paths). Lists are rebuilt as new lists.
func renameReferences(value *Node, old, new string, field string) *Node {
	switch {
	case isMap(value):
		for i := 0; i+1 < len(value.Content); i += 2 {
			if key := value.Content[i].Value; key != "tag" && key != "type" {
				value.Content[i+1] = renameReferences(value.Content[i+1], old, new, key)
			}
		}
	case isSeq(value):
		out := L()
		for _, item := range value.Content {
			out.Content = append(out.Content, renameReferences(item, old, new, field))
		}
		return out
	case isStr(value) && containsString(referenceKeys, field):
		if containsString([]string{"entry", "primary", "secondary", "provider"}, field) && value.Value == old {
			return S(new)
		}
		if field == "exec" && (value.Value == "goto "+old || value.Value == "jump "+old) {
			return S(strings.SplitN(value.Value, " ", 2)[0] + " " + new)
		}
		return S(replaceToken(value.Value, "$"+old, "$"+new))
	}
	return value
}

// replaceToken replaces whole-token occurrences of token (tag-character
// boundaries on both sides).
func replaceToken(s, token, replacement string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if strings.HasPrefix(s[i:], token) {
			before := []rune(s[:i])
			after := []rune(s[i+len(token):])
			if (len(before) == 0 || !isTagRune(before[len(before)-1])) && (len(after) == 0 || !isTagRune(after[0])) {
				b.WriteString(replacement)
				i += len(token)
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// aliasedCustomState recognizes recorded managed renames for the custom
// match controls only.
func (p *Profile) aliasedCustomState(document *nativeDoc, settings *Node) (*routingState, error) {
	aliases := get(get(settings, "_common_disabled"), "renamed_plugins")
	if !isMap(aliases) || len(aliases.Content) == 0 {
		return nil, nil
	}
	renames := map[string]string{}
	var order []string
	valid := true
	pairs(aliases, func(old string, new *Node) bool {
		if managedTags[old] {
			if !isStr(new) || !validTag(new.Value) {
				valid = false
			}
			renames[old] = new.Value
			order = append(order, old)
		}
		return true
	})
	if len(renames) == 0 || !valid {
		return nil, nil
	}
	plugins := pluginMap(document)
	expanded := plugins
	if externalIncludes(document) {
		doc, err := NativeDocument(settings, false, nil)
		if err != nil {
			return nil, err
		}
		expanded = pluginMap(doc)
	}
	targets := map[string]bool{}
	for _, new := range renames {
		targets[new] = true
	}
	if len(expanded) == 0 || len(targets) != len(renames) {
		return nil, nil
	}
	for _, old := range order {
		new := renames[old]
		if expanded[old] != nil || plugins[new] == nil || !equal(get(plugins[new], "type"), S(tagType(old))) {
			return nil, nil
		}
	}
	canonical := clone(document)
	byTag := pluginMap(canonical)
	for _, old := range order {
		new := renames[old]
		set(byTag[new], "tag", S(old))
		renameReferences(canonical, new, old, "")
	}
	state := p.analyzeRouting(canonical, settings)
	mainTag := "sequence_main"
	if t, ok := renames["sequence_main"]; ok {
		mainTag = t
	}
	if state.full {
		custom := make([]*Node, 0, len(state.customIndices))
		for _, index := range state.customIndices {
			custom = append(custom, clone(items(get(plugins[mainTag], "args"))[index]))
		}
		state.values.CustomMatches = custom
	}
	for _, field := range customFields {
		c := state.caps[field]
		c.Target = mainTag
		state.caps[field] = c
	}
	state.actualMainTag = mainTag
	return state, nil
}

// RoutingView is read_routing's answer.
type RoutingView struct {
	Values       *Node
	Capabilities map[string]Capability
}

func (p *Profile) readRouting(document *nativeDoc, settings *Node) (*routingValues, map[string]Capability, error) {
	state := p.analyzeRouting(document, settings)
	aliased, err := p.aliasedCustomState(document, settings)
	if err != nil {
		return nil, nil, err
	}
	if aliased != nil {
		state.values.CustomMatches = make([]*Node, len(aliased.values.CustomMatches))
		for i, c := range aliased.values.CustomMatches {
			state.values.CustomMatches[i] = clone(c)
		}
		if aliased.values.CustomMatchPosition != nil {
			state.values.CustomMatchPosition = strPtr(*aliased.values.CustomMatchPosition)
		} else {
			state.values.CustomMatchPosition = nil
		}
		for _, field := range customFields {
			state.caps[field] = aliased.caps[field]
		}
	}
	caps := map[string]Capability{}
	for k, v := range state.caps {
		caps[k] = v
	}
	return state.values.clone(), caps, nil
}

// ReadRouting reads the recognized routing values and what can be edited.
func ReadRouting(document *nativeDoc, settings *Node, p *Profile) (RoutingView, error) {
	values, caps, err := p.readRouting(document, settings)
	if err != nil {
		return RoutingView{}, err
	}
	return RoutingView{values.toNode(), caps}, nil
}

// MigrateDomainOnlyRouting renders old managed IP routing as domain-only
// (only a fully recognizable owned suffix; anything else is returned as is).
func MigrateDomainOnlyRouting(document *nativeDoc, settings *Node, p *Profile, baseURL string) (*Node, *Node, error) {
	source := M()
	if view := readSettingsView(settings); view.NativeConfig.OK && strings.TrimSpace(view.NativeConfig.Value) != "" {
		var err error
		if source, err = LoadNativeYAML(view.NativeConfig.Value, NativeLabel); err != nil {
			return nil, nil, err
		}
	}
	if (externalIncludes(document)) || (externalIncludes(source)) {
		return document, settings, nil
	}
	current := p.analyzeRouting(document, settings)
	if len(current.groups.IP) == 0 {
		return document, settings, nil
	}
	for _, key := range append(append([]string{}, Selectors...), "rule_actions") {
		if !current.caps[key].Editable {
			return document, settings, nil
		}
	}
	rendered, values, err := ApplyRouting(document, settings, p, RoutingChanges{}, baseURL, RoutingOptions{BeforeConfig: p})
	if err == nil {
		if err = setNativeConfig(values, rendered); err != nil {
			return nil, nil, err
		}
		var ctx *commonContext
		if ctx, err = newContext(rendered, values); err == nil {
			if err = finishRuleUpdates(ctx, M("direct_rulesets", L()), p, baseURL); err == nil {
				if err = setNativeConfig(values, rendered); err != nil {
					return nil, nil, err
				}
				return rendered, values, nil
			}
		}
	}
	if _, ok := IsSettingsError(err); ok {
		return document, settings, nil
	}
	return nil, nil, err
}

// usesRule: a domain/IP provider still holds a resource generated for identity.
func (p *Profile) usesRule(document *nativeDoc, identity string) bool {
	c := p.catalog()
	for _, plugin := range items(get(document, "plugins")) {
		part := map[string]string{"domain_set": "domain", "ip_set": "ip"}[str(get(plugin, "type"))]
		if part == "" {
			continue
		}
		found := false
		pairs(get(plugin, "args"), func(key string, values *Node) bool {
			for _, value := range items(values) {
				found = found || c.owners(part, key, value)[identity]
			}
			return !found
		})
		if found {
			return true
		}
	}
	return false
}

// RemoveRule returns native settings without a rule that is about to be
// deleted, or nil when the draft does not use it.
func RemoveRule(settings *Node, p *Profile, identity, baseURL string) (*Node, error) {
	document, err := readSettingsView(settings).loadNative(NativeLabel)
	if err != nil {
		return nil, err
	}
	state := p.analyzeRouting(document, settings)
	changes := M()
	for _, key := range Selectors {
		if state.values.hasInSelector(key, identity) {
			set(changes, key, strs(filterStrings(state.values.selector(key), func(v string) bool { return v != identity })))
		}
	}
	if state.values.Actions.has(identity) {
		kept := state.values.Actions.clone()
		kept.delete(identity)
		set(changes, "rule_actions", kept.toNode())
	}
	editable := true
	for _, key := range keys(changes) {
		editable = editable && state.caps[key].Editable
	}
	var result *Node
	if len(changes.Content) > 0 && editable {
		applied, err := ApplyCommonSettings(settings, changes, p, baseURL)
		if err != nil {
			return nil, err
		}
		result = applied.Settings
	} else if len(changes.Content) == 0 && !p.usesRule(document, identity) {
		return nil, nil
	}
	if result != nil {
		after, err := loadNative(get(result, "native_config"), NativeLabel)
		if err != nil {
			return nil, err
		}
		if !p.usesRule(after, identity) {
			return result, nil
		}
	}
	return nil, fail("OxiDNS 还在用这条规则，而且那部分配置是手动写的，没法自动移除。请先在 OxiDNS 设置的 YAML 中删掉对它的引用，再删除。")
}

func newName(preferred string, occupied map[string]bool) string {
	result := preferred
	for index := 2; occupied[result]; index++ {
		result = fmt.Sprintf("%s_%d", preferred, index)
	}
	occupied[result] = true
	return result
}

// PlaceRoutingGroups tags and inserts routing providers: unchanged providers
// keep tag and position; fresh tags avoid the old ones. external tags are
// still used elsewhere and stay.
func placeRoutingGroups(plugins []*Node, old []*recGroup, expected *routingGroups, external map[string]bool) ([]*Node, *routingGroups, error) {
	occupied := map[string]bool{}
	for _, plugin := range plugins {
		occupied[str(get(plugin, "tag"))] = true
	}
	retained := map[string]bool{}
	var additions []*Node
	groups := &routingGroups{}
	for _, part := range []string{"domain", "ip"} {
		kind := map[string]string{"domain": "domain_set", "ip": "ip_set"}[part]
		for _, g := range *expected.part(part) {
			tag := ""
			for _, candidate := range old {
				if !retained[candidate.tag] && equal(get(candidate.provider, "type"), S(kind)) && same(candidate.args, g.Args) {
					tag = candidate.tag
					break
				}
			}
			if tag != "" {
				retained[tag] = true
			} else {
				tag = newName(g.Tag, occupied)
				additions = append(additions, M("tag", tag, "type", kind, "args", clone(g.Args)))
			}
			*groups.part(part) = append(*groups.part(part), &ruleGroup{g.Route, clone(g.Args), tag})
		}
	}
	removable := map[string]bool{}
	for _, g := range old {
		if !retained[g.tag] && !external[g.tag] {
			removable[g.tag] = true
		}
	}
	insertion := -1
	for i, plugin := range plugins {
		if removable[str(get(plugin, "tag"))] {
			insertion = i
			break
		}
	}
	if insertion < 0 {
		for i, plugin := range plugins {
			if equal(get(plugin, "tag"), S("sequence_main")) {
				insertion = i
				break
			}
		}
	}
	if insertion < 0 {
		return nil, nil, errMalformed
	}
	var placed []*Node
	for i, plugin := range plugins {
		if i == insertion {
			placed = append(placed, additions...)
		}
		if !removable[str(get(plugin, "tag"))] {
			placed = append(placed, plugin)
		}
	}
	return placed, groups, nil
}

// ReloadReferences edits a reload provider list: retained entries keep
// their order, new providers are appended.
func ReloadReferences(existing []*Node, oldTags map[string]bool, newTags []string, plugins map[string]*Node) []*Node {
	values := append([]*Node{}, existing...)
	for _, tag := range newTags {
		values = append(values, S("$"+tag))
	}
	var refs []*Node
	for _, value := range values {
		keep := true
		if isStr(value) {
			tag := strings.TrimPrefix(value.Value, "$")
			keep = containsString(newTags, tag) || !oldTags[tag] || plugins[tag] != nil
		}
		if keep && !contains(seqOf(refs), value) {
			refs = append(refs, value)
		}
	}
	return refs
}

// ResourceDownloads are the download descriptors ({url, dir, filename}) of
// the provider resources the document actually reads.
func ResourceDownloads(document *nativeDoc, settings *Node, p *Profile, baseURL string) ([]*Node, error) {
	return p.resourceDownloads(document, settings, baseURL, nil)
}

func (p *Profile) resourceDownloads(document *nativeDoc, settings *Node, baseURL string, c *ruleCatalog) ([]*Node, error) {
	referenced, err := referencedPaths(document)
	if err != nil {
		return nil, err
	}
	effective := p.base(baseURL)
	if c == nil {
		c = p.catalog()
	}
	desired := []*Node{}
	entry := func(url, local string) *Node {
		return M("url", url, "dir", posixParent(local), "filename", posixName(local))
	}
	for _, identity := range c.order {
		record := c.records[identity]
		if record.suffix != "rulesets" {
			continue
		}
		var parts []string
		for part := range record.parts {
			parts = append(parts, part)
		}
		sort.Strings(parts)
		for _, part := range parts {
			local := rulePath(record.item.ID, part)
			if rel, _, err := relativeResource(local); err != nil || !referenced[rel] {
				continue
			}
			link := model.TextValue(record.item.URL)
			if link == "" {
				return nil, fail("已选择的规则集缺少下载来源。")
			}
			if strings.HasPrefix(link, "/") {
				link = effective + link
			}
			proxied, err := p.ruleProxyURL(effective, link, part)
			if err != nil {
				return nil, err
			}
			desired = append(desired, entry(proxied, local))
		}
	}
	for _, url := range readSettingsView(settings).adguardURLs() {
		local := "./rules/" + tagForString("adguard", url) + ".txt"
		if rel, _, err := relativeResource(local); err == nil && referenced[rel] {
			desired = append(desired, entry(url, local))
		}
	}
	return desired, nil
}

// ResourceReaders are conservative resource consumers (strings that look
// like paths or patterns, including unknown plugin fields).
func ResourceReaders(document *nativeDoc) []string {
	var out []string
	var walk func(*Node)
	walk = func(v *Node) {
		switch {
		case isStr(v):
			if strings.ContainsAny(v.Value, "/*?[") || strings.Contains(v.Value, "${") {
				out = append(out, v.Value)
			}
		case isMap(v):
			pairs(v, func(_ string, child *Node) bool { walk(child); return true })
		case isSeq(v):
			for _, item := range v.Content {
				walk(item)
			}
		}
	}
	walk(withoutPlugins(document))
	for _, plugin := range items(get(document, "plugins")) {
		if !equal(get(plugin, "type"), S("download")) {
			walk(plugin)
		}
	}
	return out
}

// globMatch is fnmatch.fnmatchcase: shell wildcards over the whole string
// (* also matches /); [!x] is a negated class.
func globMatch(name, pattern string) bool {
	var b strings.Builder
	b.WriteString(`(?s)\A`)
	for i := 0; i < len(pattern); {
		c, width := utf8.DecodeRuneInString(pattern[i:])
		i += width
		switch c {
		case '*':
			for i < len(pattern) && pattern[i] == '*' {
				i++
			}
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '[':
			j := i
			if j < len(pattern) && pattern[j] == '!' {
				j++
			}
			if j < len(pattern) && pattern[j] == ']' {
				j++
			}
			for j < len(pattern) && pattern[j] != ']' {
				j++
			}
			if j >= len(pattern) {
				b.WriteString(`\[`)
				continue
			}
			stuff := strings.ReplaceAll(pattern[i:j], `\`, `\\`)
			i = j + 1
			switch {
			case stuff == "":
				b.WriteString(`[^\x00-\x{10FFFF}]`)
			case stuff == "!":
				b.WriteString(".")
			default:
				if stuff[0] == '!' {
					stuff = "^" + stuff[1:]
				} else if stuff[0] == '^' || stuff[0] == '[' {
					stuff = `\` + stuff
				}
				b.WriteString("[" + stuff + "]")
			}
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString(`\z`)
	re, err := regexp.Compile(b.String())
	return err == nil && re.MatchString(name)
}

// ResourceIsRead: one of the readers may read path.
func ResourceIsRead(path string, readers []string) bool {
	for _, reference := range readers {
		if strings.Contains(reference, "${") || globMatch(path, strings.TrimPrefix(reference, "./")) || strings.HasSuffix(reference, "/"+path) {
			return true
		}
		// An absolute pattern may name the resource under the target's directory.
		if strings.HasPrefix(reference, "/") {
			for i := 0; i < len(reference); i++ {
				if reference[i] == '/' && globMatch(path, reference[i+1:]) {
					return true
				}
			}
		}
	}
	return false
}

// replaceDownloadEntries detaches the downloader's args from other alias
// holders and sets its downloads.
func replaceDownloadEntries(download *Node, entries []*Node) {
	args := shallow(get(download, "args"))
	set(args, "downloads", seqOf(entries))
	set(download, "args", clone(args))
}

// ownHelpers creates, keeps, refreshes or removes the owned helper plugins.
// entries are the rule group entries just written (not "elsewhere").
func ownHelpers(document *nativeDoc, routes []string, actions *actionMap, entries []*Node) error {
	list := get(document, "plugins")
	byTag := pluginMap(document)
	desired, err := helperPlugins(routes, actions, documentFeatures(byTag))
	if err != nil {
		return err
	}
	wanted := map[string]*Node{}
	for _, plugin := range desired {
		wanted[str(get(plugin, "tag"))] = plugin
	}
	owned := map[*Node]bool{}
	for _, entry := range entries {
		owned[entry] = true
	}
	var outside []*Node
	for _, plugin := range list.Content {
		if helperTagRE.MatchString(displayValue(get(plugin, "tag"))) {
			continue
		}
		if equal(get(plugin, "tag"), S("sequence_main")) && isSeq(get(plugin, "args")) {
			copy := shallow(plugin)
			set(copy, "args", seqOf(filter(get(plugin, "args").Content, func(item *Node) bool { return !owned[item] })))
			plugin = copy
		}
		outside = append(outside, plugin)
	}
	top := withoutPlugins(document)
	external := func(tag string) bool { return refersTo(top, tag) || len(positions(outside, tag)) > 0 }
	replaceable := func(tag string) bool {
		key := helperTagRE.FindStringSubmatch(tag)[1]
		if key == "" {
			return false
		}
		action := pinnedAction(key, byTag)
		if action == nil {
			return false
		}
		if RouteKey(action) == "upstream:"+key {
			return true
		}
		for _, prefix := range []string{UpstreamPrefix, ForwardPrefix, CachePrefix} {
			if external(prefix + key) {
				return false
			}
		}
		return true
	}
	for _, plugin := range desired {
		tag := str(get(plugin, "tag"))
		if existing := byTag[tag]; existing != nil && !same(existing, plugin) && !replaceable(tag) {
			return fail("已有同名插件 " + tag + "，无法创建 DNS 处理所需插件，请先在完整参数中改名")
		}
	}
	kept := map[string]bool{}
	for tag := range wanted {
		kept[tag] = true
	}
	for tag := range byTag {
		if helperTagRE.MatchString(tag) && external(tag) {
			kept[tag] = true
		}
	}
	var pending []string
	for tag := range kept {
		pending = append(pending, tag)
	}
	for len(pending) > 0 {
		tag := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		source := wanted[tag]
		if source == nil {
			source = byTag[tag]
		}
		for other := range byTag {
			if !kept[other] && helperTagRE.MatchString(other) && refersTo(source, other) {
				kept[other] = true
				pending = append(pending, other)
			}
		}
	}
	var result []*Node
	for _, plugin := range list.Content {
		tag := displayValue(get(plugin, "tag"))
		if wanted[tag] != nil {
			result = append(result, clone(wanted[tag]))
		} else if !helperTagRE.MatchString(tag) || kept[tag] {
			result = append(result, plugin)
		}
	}
	for _, plugin := range desired {
		tag := str(get(plugin, "tag"))
		if byTag[tag] != nil {
			continue
		}
		index := -1
		for i, item := range result {
			other := str(get(item, "tag"))
			if wanted[other] != nil && other != tag && refersTo(wanted[other], tag) {
				index = i
				break
			}
		}
		if index < 0 {
			index = len(result)
			for i, item := range result {
				if equal(get(item, "tag"), S("sequence_main")) {
					index = i
					break
				}
			}
		}
		result = append(result[:index], append([]*Node{clone(plugin)}, result[index:]...)...)
	}
	list.Content = result
	return nil
}

type ownedChain struct {
	tag    string
	action RuleAction
}

func (c ownedChain) toNode() *Node { return c.action.toNode() }

func ownedChainList(document *nativeDoc) []ownedChain {
	plugins := pluginMap(document)
	var out []ownedChain
	for _, item := range items(get(document, "plugins")) {
		tag := str(get(item, "tag"))
		if len(plugins) == 0 || !strings.HasPrefix(tag, UpstreamPrefix) {
			continue
		}
		key := tag[len(UpstreamPrefix):]
		if action, ok := pinnedRuleAction(key, plugins); ok && action.Key() == "upstream:"+key {
			out = append(out, ownedChain{tag, action})
		}
	}
	return out
}

// OwnedChains maps the pinned upstream chains whose tag still names their
// content to their actions.
func OwnedChains(document *nativeDoc) map[string]*Node {
	out := map[string]*Node{}
	for _, c := range ownedChainList(document) {
		out[c.tag] = c.action.toNode()
	}
	return out
}

// RefreshRuleHelpers rebuilds the owned pinned chains for the document's
// current IP selection, RouterOS, cache and TTL features.
func RefreshRuleHelpers(document *nativeDoc) error {
	chains := ownedChainList(document)
	if len(chains) == 0 {
		return nil
	}
	actions := newActionMap()
	var routes []string
	for _, c := range chains {
		actions.set(c.tag, c.action)
		routes = append(routes, c.action.Key())
	}
	return ownHelpers(document, routes, actions, nil)
}

// downloadPath is the managed path of a download entry ("" when unreadable).
func downloadPath(entry *Node) string {
	dir, file := get(entry, "dir"), get(entry, "filename")
	if !isStr(dir) || !isStr(file) {
		return ""
	}
	return strings.TrimPrefix(strings.TrimRight(dir.Value, "/")+"/"+file.Value, "./")
}

func (p *Profile) syncUpdates(document *nativeDoc, settings *Node, oldGroups []*recGroup, newGroups *routingGroups, baseURL string, records, before *ruleCatalog) error {
	plugins := pluginMap(document)
	download, reload := plugins["rules_download"], plugins["rules_reload"]
	if download == nil && reload == nil {
		return nil
	}
	parked := get(get(settings, "_common_disabled"), "rules_reload")
	reloadParked := reload == nil && isMap(parked) && equal(get(parked, "tag"), S("rules_reload")) &&
		equal(get(parked, "type"), S("reload_provider")) && isSeq(get(parked, "args"))
	if download == nil || !equal(get(download, "type"), S("download")) || !isSeq(get(get(download, "args"), "downloads")) ||
		!reloadParked && (reload == nil || !equal(get(reload, "type"), S("reload_provider")) || !isSeq(get(reload, "args"))) {
		return fail("规则定时更新包含自定义结构，请先在完整参数中调整下载与重载任务。")
	}
	oldTags := map[string]bool{}
	for _, g := range oldGroups {
		oldTags[g.tag] = true
	}
	var newTags []string
	for _, g := range newGroups.all() {
		newTags = append(newTags, g.Tag)
	}
	if reload != nil {
		set(reload, "args", seqOf(ReloadReferences(get(reload, "args").Content, oldTags, newTags, plugins)))
	}
	knownPaths := map[string]bool{}
	for _, c := range []*ruleCatalog{records, before} {
		if c == nil {
			continue
		}
		for _, r := range c.records {
			if r.suffix == "rulesets" {
				for part := range r.parts {
					knownPaths[strings.TrimPrefix(rulePath(r.item.ID, part), "./")] = true
				}
			}
		}
	}
	desired, err := p.resourceDownloads(document, settings, baseURL, records)
	if err != nil {
		return err
	}
	wanted := map[string]*Node{}
	var wantedOrder []string
	for _, item := range desired {
		if local := downloadPath(item); knownPaths[local] {
			if wanted[local] == nil {
				wantedOrder = append(wantedOrder, local)
			}
			wanted[local] = item
		}
	}
	var output []*Node
	readers := ResourceReaders(document)
	for _, entry := range get(get(download, "args"), "downloads").Content {
		location := downloadPath(entry)
		switch {
		case !knownPaths[location]:
			output = append(output, entry)
		case wanted[location] != nil:
			output = append(output, entry)
			delete(wanted, location)
		case ResourceIsRead(location, readers):
			output = append(output, entry)
		}
	}
	for _, local := range wantedOrder {
		if wanted[local] != nil {
			output = append(output, wanted[local])
		}
	}
	replaceDownloadEntries(download, output)
	return nil
}

func routeLabel(route string) string { return RouteLabels[strings.SplitN(route, ":", 2)[0]] }

// assignStored routes one active rule by a stored choice.
func assignStored(values *ruleDNSValues, item model.RuleConfig, action RuleAction) error {
	normalized, err := normalizeAction(action.toNode(), true)
	if err != nil {
		return err
	}
	if isListRoute(normalized.Action) {
		values.appendSelector(normalized.Action+"_"+suffixOf(item), item.ID)
	} else {
		values.Actions.set(item.ID, normalized)
	}
	return nil
}

func validateSelections(values *routingValues, changes *Node, known map[string]*model.RuleConfig) error {
	for _, key := range Selectors {
		if has(changes, key) {
			raw := get(changes, key)
			if !allStrings(raw) {
				return fail(key + " 必须为规则 ID 列表")
			}
			ids := uniqueStrings(ruleDNSIDs(raw))
			suffix := "rules"
			if strings.HasSuffix(key, "rulesets") {
				suffix = "rulesets"
			}
			for _, id := range ids {
				if known[id] == nil || suffixOf(*known[id]) != suffix {
					return fail(missingRule)
				}
			}
			values.setSelector(key, ids)
		} else {
			values.setSelector(key, uniqueStrings(values.selector(key)))
		}
	}
	if has(changes, "rule_actions") {
		actions := get(changes, "rule_actions")
		if !isMap(actions) {
			return fail("rule_actions 必须为以规则 ID 为键的对象")
		}
		for _, id := range keys(actions) {
			if known[id] == nil {
				return fail(missingRule)
			}
		}
		normalized := newActionMap()
		var err error
		pairs(actions, func(id string, action *Node) bool {
			if !isNone(action) {
				var n RuleAction
				if n, err = normalizeAction(action, false); err != nil {
					return false
				}
				if n.Upstream != nil {
					n.Upstream = clone(n.Upstream)
				}
				normalized.set(id, n)
			}
			return true
		})
		if err != nil {
			return err
		}
		values.Actions = normalized
	}
	if has(changes, "rule_dns_follow") {
		follow := get(changes, "rule_dns_follow")
		if !allStrings(follow) {
			return fail("rule_dns_follow 必须为规则 ID 数组")
		}
		ids := ruleDNSIDs(follow)
		for _, id := range ids {
			if known[id] == nil {
				return fail(missingRule)
			}
		}
		values.Follow = uniqueStrings(ids)
	}
	for _, suffix := range []string{"rules", "rulesets"} {
		proxy := values.selector("proxy_" + suffix)
		for _, id := range values.selector("direct_" + suffix) {
			if slices.Contains(proxy, id) {
				return fail("同一规则不能同时选择直连和代理。")
			}
		}
	}
	return nil
}

func takeOver(values *routingValues, current *routingState, changes *Node, known map[string]*model.RuleConfig, notices *[]string) error {
	old := current.values
	type taken struct{ identity, before, after string }
	var moved []taken
	if has(changes, "rule_actions") {
		for _, identity := range values.Actions.order {
			if old.Actions.has(identity) {
				continue
			}
			action := values.Actions.items[identity]
			for _, key := range Selectors {
				if values.hasInSelector(key, identity) {
					if has(changes, key) {
						return fail("同一规则不能同时选择多种 DNS 处理。")
					}
					values.removeSelector(key, identity)
					moved = append(moved, taken{identity, routePrefix(key), action.Key()})
				}
			}
		}
	}
	for _, key := range Selectors {
		if !has(changes, key) {
			continue
		}
		for _, id := range values.selector(key) {
			if old.hasInSelector(key, id) || !values.Actions.has(id) {
				continue
			}
			if has(changes, "rule_actions") {
				return fail("同一规则不能同时选择多种 DNS 处理。")
			}
			action, _ := values.Actions.delete(id)
			moved = append(moved, taken{id, action.Key(), routePrefix(key)})
		}
	}
	if notices != nil {
		for _, t := range moved {
			*notices = append(*notices, "规则「"+ruleName(known[t.identity])+"」的 DNS 处理已从「"+routeLabel(t.before)+"」改为「"+routeLabel(t.after)+"」")
		}
	}
	return nil
}

// ruleName is the label of a rule: its name, else its value, else its id.
func ruleName(item *model.RuleConfig) string {
	if item == nil {
		return ""
	}
	for _, text := range []string{item.Name, model.TextValue(item.Value)} {
		if text != "" {
			return text
		}
	}
	return item.ID
}

// settle keeps only active rules in the selections; returns (parked, follow).
func settle(values *routingValues, settings, changes *Node, current *routingState, known map[string]*model.RuleConfig, active *ruleCatalog, routes map[string]string) (*actionMap, []string, error) {
	followGiven := has(changes, "rule_dns_follow")
	follow := filterStrings(values.Follow, func(id string) bool { return known[id] != nil })
	parked := newActionMap()
	pairs(get(settings, "rule_dns_parked"), func(id string, action *Node) bool {
		if known[id] != nil {
			if a, err := normalizeAction(action, true); err == nil {
				if a.Upstream != nil {
					a.Upstream = clone(a.Upstream)
				}
				parked.set(id, a)
			} else {
				parked.set(id, RuleAction{raw: clone(action)})
			}
		}
		return true
	})
	assigned := map[string]bool{}
	for _, key := range Selectors {
		if has(changes, key) {
			for _, id := range values.selector(key) {
				assigned[id] = true
			}
		}
	}
	if has(changes, "rule_actions") {
		for _, id := range values.Actions.order {
			assigned[id] = true
		}
	}
	leaving := newActionMap()
	for _, key := range Selectors {
		list := values.selector(key)
		for _, id := range list {
			if active.records[id] == nil {
				leaving.set(id, RuleAction{Action: routePrefix(key)})
			}
		}
		values.setSelector(key, filterStrings(list, func(id string) bool { return active.records[id] != nil }))
	}
	kept := newActionMap()
	for _, id := range values.Actions.order {
		action := values.Actions.items[id]
		if active.records[id] == nil {
			leaving.set(id, action)
		} else {
			kept.set(id, action)
		}
	}
	values.Actions = kept
	for _, id := range leaving.order {
		action := leaving.items[id]
		if known[id] != nil && (!slices.Contains(follow, id) || assigned[id]) {
			parked.set(id, action)
			if slices.Contains(follow, id) && !followGiven {
				follow = removeString(follow, id)
			}
		}
	}
	before := current.records
	for _, id := range active.order {
		record := active.records[id]
		switch {
		case values.routeOf(id) != "":
			parked.delete(id)
		case parked.has(id):
			stored, _ := parked.delete(id)
			if err := assignStored(&values.ruleDNSValues, record.item, stored); err != nil {
				return nil, nil, err
			}
		case slices.Contains(follow, id) && (before == nil || before.records[id] == nil):
			if route := followRoute(record.item, routes); route != "" {
				if err := assignStored(&values.ruleDNSValues, record.item, RuleAction{Action: route}); err != nil {
					return nil, nil, err
				}
			}
		}
	}
	if !followGiven {
		follow = filterStrings(follow, func(id string) bool {
			if values.Actions.has(id) {
				return false
			}
			return !(before != nil && before.records[id] != nil && active.records[id] != nil &&
				current.values.routeOf(id) != values.routeOf(id))
		})
	}
	for _, id := range follow {
		if action, ok := values.Actions.get(id); ok {
			return nil, nil, fail("规则 " + id + " 已设置 DNS 处理「" + RouteLabels[action.Action] + "」，不能同时跟随策略")
		}
		if parked.has(id) {
			return nil, nil, fail("规则 " + id + " 已暂存停用前的 DNS 处理，不能同时跟随策略")
		}
	}
	return parked, follow, nil
}

func normalizeCustomMatches(value *Node) (*Node, error) {
	if !isSeq(value) {
		return nil, fail("自定义匹配必须为列表。")
	}
	out := L()
	for _, entry := range value.Content {
		if !isMap(entry) {
			return nil, fail("每条自定义匹配必须为对象。")
		}
		if enabled := get(entry, "enabled"); isBool(enabled) && enabled.Value == "false" {
			return nil, fail("常用设置直接编辑执行中的匹配条目，不支持 enabled: false；请移除该条目或改为启用。")
		}
		matches := entryMatches(entry)
		ok := isSeq(matches) && len(matches.Content) > 0 && strings.TrimSpace(str(get(entry, "exec"))) != ""
		for _, m := range items(matches) {
			ok = ok && isStr(m) && strings.TrimSpace(m.Value) != ""
		}
		if !ok {
			return nil, fail("自定义匹配需填写非空条件与执行动作。")
		}
		copy := M()
		pairs(entry, func(key string, v *Node) bool {
			if key != "enabled" {
				set(copy, key, clone(v))
			}
			return true
		})
		out.Content = append(out.Content, copy)
	}
	return out, nil
}

func applyAliasedCustom(document *nativeDoc, settings *Node, state *routingState, changes *Node) (*Node, *Node, error) {
	for _, field := range keys(changes) {
		if c := state.caps[field]; !c.Editable {
			return nil, nil, fail(c.Reason)
		}
	}
	position := "tail"
	if state.values.CustomMatchPosition != nil && *state.values.CustomMatchPosition != "" {
		position = *state.values.CustomMatchPosition
	}
	if has(changes, "custom_match_position") {
		position = str(get(changes, "custom_match_position"))
		if !isOneOf(get(changes, "custom_match_position"), "head", "tail") {
			return nil, nil, fail("自定义匹配位置必须为 head 或 tail。")
		}
	}
	custom := make([]*Node, len(state.values.CustomMatches))
	for i, c := range state.values.CustomMatches {
		custom[i] = clone(c)
	}
	if has(changes, "custom_matches") {
		normalized, err := normalizeCustomMatches(get(changes, "custom_matches"))
		if err != nil {
			return nil, nil, err
		}
		custom = normalized.Content
	}
	result := clone(document)
	main := pluginMap(result)[state.actualMainTag]
	args := get(main, "args").Content
	prefix := len(state.prefix)
	middle := prefix + len(state.customIndices) + len(state.groups.Domain)
	var domain []*Node
	for _, g := range state.groups.Domain {
		domain = append(domain, args[g.index])
	}
	set(main, "args", seqOf(assemble(args[:prefix], custom, domain, position, args[middle:])))
	return result, clone(settings), nil
}

func assemble(prefix, custom, domain []*Node, position string, rest []*Node) []*Node {
	out := append([]*Node{}, prefix...)
	if position == "head" {
		out = append(out, custom...)
	}
	out = append(out, domain...)
	if position == "tail" {
		out = append(out, custom...)
	}
	return append(out, rest...)
}

// RoutingOptions: BeforeConfig is the rule space the document was written
// for (rebuild the owned routing for p even without changes); Notices, when
// set, receives a line for each rule a changed key took from another choice.
type RoutingOptions struct {
	BeforeConfig *Profile
	Notices      *[]string
}

// RuleActionEntry pairs a rule ID with its explicit RuleAction in slice order.
type RuleActionEntry struct {
	RuleID string
	Action RuleAction
}

// RuleActionsFromNode decodes a rule_actions mapping node into ordered entries.
func RuleActionsFromNode(n *Node) []RuleActionEntry {
	out := []RuleActionEntry{}
	pairs(n, func(id string, value *Node) bool {
		if !isNone(value) {
			a := actionOf(value)
			if a.Upstream != nil {
				a.Upstream = clone(a.Upstream)
			}
			out = append(out, RuleActionEntry{RuleID: id, Action: a})
		}
		return true
	})
	return out
}

// RoutingChanges is a typed patch to a native document's recognizable routing.
// Nil fields mean unchanged; non-nil pointers to empty slices clear that field.
type RoutingChanges struct {
	DirectRules         *[]string
	ProxyRules          *[]string
	DirectRulesets      *[]string
	ProxyRulesets       *[]string
	RuleActions         *[]RuleActionEntry
	RuleDNSFollow       *[]string
	AggregateRules      *bool
	DefaultForward      *string
	CustomMatches       *[]*Node
	CustomMatchPosition *string
}

func (c RoutingChanges) toNode() *Node {
	out := M()
	if c.DirectRules != nil {
		set(out, "direct_rules", strs(*c.DirectRules))
	}
	if c.ProxyRules != nil {
		set(out, "proxy_rules", strs(*c.ProxyRules))
	}
	if c.DirectRulesets != nil {
		set(out, "direct_rulesets", strs(*c.DirectRulesets))
	}
	if c.ProxyRulesets != nil {
		set(out, "proxy_rulesets", strs(*c.ProxyRulesets))
	}
	if c.RuleActions != nil {
		actions := M()
		for _, entry := range *c.RuleActions {
			set(actions, entry.RuleID, entry.Action.toNode())
		}
		set(out, "rule_actions", actions)
	}
	if c.RuleDNSFollow != nil {
		set(out, "rule_dns_follow", strs(*c.RuleDNSFollow))
	}
	if c.AggregateRules != nil {
		set(out, "aggregate_rules", B(*c.AggregateRules))
	}
	if c.DefaultForward != nil {
		set(out, "default_forward", S(*c.DefaultForward))
	}
	if c.CustomMatches != nil {
		set(out, "custom_matches", seqOf(*c.CustomMatches))
	}
	if c.CustomMatchPosition != nil {
		set(out, "custom_match_position", S(*c.CustomMatchPosition))
	}
	return out
}

// ApplyRouting applies one typed atomic routing edit to copies of document and
// settings.
func ApplyRouting(document *nativeDoc, settings *Node, p *Profile, changes RoutingChanges, baseURL string, opts RoutingOptions) (*Node, *Node, error) {
	return applyRoutingNode(document, settings, p, changes.toNode(), baseURL, opts)
}

func applyRoutingNode(document *nativeDoc, settings *Node, p *Profile, changes *Node, baseURL string, opts RoutingOptions) (*Node, *Node, error) {
	filtered := M()
	pairs(changes, func(key string, value *Node) bool {
		if containsString(RoutingFields, key) {
			set(filtered, key, value)
		}
		return true
	})
	changes = filtered
	rebuild := opts.BeforeConfig != nil
	if len(changes.Content) == 0 && !rebuild {
		return clone(document), clone(settings), nil
	}
	onlyCustom, anyCustom := true, false
	for _, key := range keys(changes) {
		c := containsString(customFields, key)
		onlyCustom, anyCustom = onlyCustom && c, anyCustom || c
	}
	if !rebuild && anyCustom {
		aliased, err := p.aliasedCustomState(document, settings)
		if err != nil {
			return nil, nil, err
		}
		if aliased != nil {
			if onlyCustom {
				return applyAliasedCustom(document, settings, aliased, changes)
			}
			return nil, nil, fail("执行链已重命名，请单独调整自定义匹配，再编辑其他分流设置。")
		}
	}
	source := p
	if rebuild {
		source = opts.BeforeConfig
	}
	current := source.analyzeRouting(document, settings)
	for _, key := range keys(changes) {
		if c := current.caps[key]; !c.Editable {
			return nil, nil, fail(c.Reason)
		}
	}
	if rebuild && !current.caps["direct_rules"].Editable {
		return nil, nil, fail(current.caps["direct_rules"].Reason)
	}
	values := current.values.clone()
	defaultForward := ""
	if has(changes, "default_forward") {
		df := get(changes, "default_forward")
		if !isOneOf(df, "forward_local", "forward_remote") {
			return nil, nil, fail("默认转发目标无效。")
		}
		defaultForward = df.Value
		values.DefaultForward = strPtr(defaultForward)
	} else if values.DefaultForward != nil && (*values.DefaultForward == "forward_local" || *values.DefaultForward == "forward_remote") {
		defaultForward = *values.DefaultForward
	} else {
		return nil, nil, fail("默认转发目标无效。")
	}
	migrateIP := len(current.groups.IP) > 0 && current.caps["direct_rules"].Editable
	onlyDefault := len(changes.Content) == 2 && has(changes, "default_forward")
	if !rebuild && !migrateIP && (onlyDefault || len(changes.Content) == 0) {
		return switchDefault(document, settings, current, defaultForward)
	}
	known, _ := p.knownRules()
	records := current.records
	if rebuild {
		records = p.catalog()
	}
	active := &ruleCatalog{records: map[string]*ruleRecord{}}
	for _, id := range records.order {
		if convertible(records.records[id].item) {
			active.order = append(active.order, id)
			active.records[id] = records.records[id]
		}
	}
	if err := validateSelections(values, changes, known); err != nil {
		return nil, nil, err
	}
	if err := takeOver(values, current, changes, known, opts.Notices); err != nil {
		return nil, nil, err
	}
	parked, follow, err := settle(values, settings, changes, current, known, active, policyDNSRoutes(p))
	if err != nil {
		return nil, nil, err
	}
	if has(changes, "aggregate_rules") {
		agg := get(changes, "aggregate_rules")
		if !isBool(agg) {
			return nil, nil, fail("合并相邻规则必须为布尔值。")
		}
		values.AggregateRules = boolPtr(agg.Value == "true")
	}
	if has(changes, "custom_match_position") {
		pos := get(changes, "custom_match_position")
		if !isOneOf(pos, "head", "tail") {
			return nil, nil, fail("自定义匹配执行顺序必须为 head 或 tail。")
		}
		values.CustomMatchPosition = strPtr(pos.Value)
	}
	if has(changes, "custom_matches") {
		custom, err := normalizeCustomMatches(get(changes, "custom_matches"))
		if err != nil {
			return nil, nil, err
		}
		values.CustomMatches = custom.Content
	}
	result, resultSettings := clone(document), clone(settings)
	main := pluginMap(result)["sequence_main"]
	routingChanged := rebuild || migrateIP
	for _, key := range append(append([]string{}, Selectors...), "aggregate_rules", "rule_actions") {
		routingChanged = routingChanged || has(changes, key)
	}
	var groups *routingGroups
	if routingChanged {
		aggregate := true
		if values.AggregateRules != nil {
			aggregate = *values.AggregateRules
		}
		expected, err := p.routingGroups(&values.ruleDNSValues, aggregate, legacyNone, nil)
		if err != nil {
			return nil, nil, err
		}
		old := current.groups.all()
		outside := []*Node{withoutPlugins(document), seqOf(values.CustomMatches)}
		inside := items(get(result, "plugins"))
		external := map[string]bool{}
		for _, g := range old {
			extraKeys := len(filter(keyNodes(g.provider), func(k *Node) bool { return !isOneOf(k, "tag", "type", "args") })) > 0
			used := false
			for _, i := range positions(inside, g.tag) {
				used = used || !isOneOf(get(inside[i], "tag"), "sequence_main", "rules_reload", g.tag)
			}
			if extraKeys || (externalIncludes(document)) || len(positions(outside, g.tag)) > 0 || used {
				external[g.tag] = true
			}
		}
		placed, placedGroups, err := placeRoutingGroups(inside, old, expected, external)
		if err != nil {
			return nil, nil, err
		}
		set(result, "plugins", seqOf(placed))
		groups = placedGroups
		for _, key := range Selectors {
			previous := get(resultSettings, key)
			list := L()
			if isSeq(previous) {
				list = clone(previous)
			}
			for _, id := range values.selector(key) {
				if !containsStr(list, id) {
					list.Content = append(list.Content, S(id))
				}
			}
			set(resultSettings, key, list)
		}
		set(resultSettings, "rule_actions", values.Actions.toNode())
		set(resultSettings, "rule_dns_parked", parked.toNode())
	} else {
		groups = &routingGroups{}
		for _, g := range current.groups.Domain {
			groups.Domain = append(groups.Domain, &ruleGroup{g.route, g.args, g.tag})
		}
		for _, g := range current.groups.IP {
			groups.IP = append(groups.IP, &ruleGroup{g.route, g.args, g.tag})
		}
	}
	if routingChanged || has(changes, "rule_dns_follow") {
		set(resultSettings, "rule_dns_follow", strs(follow))
	}
	custom := make([]*Node, len(values.CustomMatches))
	for i, c := range values.CustomMatches {
		custom[i] = clone(c)
	}
	position := "tail"
	if values.CustomMatchPosition != nil && *values.CustomMatchPosition != "" {
		position = *values.CustomMatchPosition
	}
	entryFor := func(part string, index int, g *ruleGroup, recognized []*recGroup) (*Node, error) {
		if !routingChanged {
			return clone(recognized[index].entry), nil
		}
		exec, err := RouteExec(g.Route, part)
		if err != nil {
			return nil, err
		}
		match := map[string]string{"domain": "qname $", "ip": "resp_ip $"}[part] + g.Tag
		return M("matches", L(match), "exec", exec), nil
	}
	var domain, ip []*Node
	var routes []string
	for i, g := range groups.Domain {
		e, err := entryFor("domain", i, g, current.groups.Domain)
		if err != nil {
			return nil, nil, err
		}
		domain = append(domain, e)
		routes = append(routes, g.Route)
	}
	var ipRoutes []string
	for i, g := range groups.IP {
		e, err := entryFor("ip", i, g, current.groups.IP)
		if err != nil {
			return nil, nil, err
		}
		ip = append(ip, e)
		ipRoutes = append(ipRoutes, g.Route)
	}
	routes = append(routes, ipRoutes...)
	final := clone(current.final)
	finalExec := "goto proxy_dns_seq"
	if defaultForward == "forward_local" {
		finalExec = "goto china_dns"
	}
	set(final, "exec", S(finalExec))
	var suffix []*Node
	for _, d := range current.dynamic {
		suffix = append(suffix, clone(d))
	}
	probe := probeNeeded(ipRoutes, defaultForward)
	suffix = append(suffix, ipSuffix(ip, defaultForward, probe)...)
	var affected []*Node
	if has(changes, "default_forward") {
		affected = append(affected, final)
	}
	if routingChanged {
		affected = append(append(affected, domain...), ip...)
	}
	if len(ip) > 0 && probe && (routingChanged || has(changes, "default_forward")) {
		affected = append(affected, M("exec", "jump local_probe"))
		if defaultForward == "forward_local" {
			affected = append(affected, M("exec", "goto default_direct_ip"))
		}
	}
	available := pluginMap(result)
	for _, entry := range affected {
		verb, target, _ := strings.Cut(str(get(entry, "exec")), " ")
		if verb != "goto" && verb != "jump" || strings.HasPrefix(target, UpstreamPrefix) {
			continue
		}
		if !equal(get(available[target], "type"), S("sequence")) {
			return nil, nil, fail("分流执行链 " + target + " 未配置，请在完整参数中添加后再调整。")
		}
	}
	prefix := make([]*Node, len(current.prefix))
	for i, e := range current.prefix {
		prefix[i] = clone(e)
	}
	set(main, "args", seqOf(assemble(prefix, custom, domain, position, append(suffix, final))))
	if routingChanged {
		if err := ownHelpers(result, routes, values.Actions, append(append([]*Node{}, domain...), ip...)); err != nil {
			return nil, nil, err
		}
		var before *ruleCatalog
		if rebuild {
			before = current.records
		}
		if err := p.syncUpdates(result, resultSettings, current.groups.all(), groups, baseURL, records, before); err != nil {
			return nil, nil, err
		}
	}
	return result, resultSettings, nil
}

// switchDefault rewrites only the final fallback (and the IP block, whose
// probe depends on it).
func switchDefault(document *nativeDoc, settings *Node, current *routingState, defaultForward string) (*Node, *Node, error) {
	result := clone(document)
	plugins := pluginMap(result)
	args := get(plugins["sequence_main"], "args")
	direct := defaultForward == "forward_local"
	target := "proxy_dns_seq"
	if direct {
		target = "china_dns"
	}
	if !equal(get(plugins[target], "type"), S("sequence")) {
		return nil, nil, fail("目标分流执行链未配置，请在完整参数中添加后再切换。")
	}
	set(args.Content[len(args.Content)-1], "exec", S("goto "+target))
	if ipGroups := current.groups.IP; len(ipGroups) > 0 {
		first := ipGroups[0].index
		for _, g := range ipGroups {
			first = min(first, g.index)
		}
		start := first
		if first > 0 && equal(args.Content[first-1], M("exec", "jump local_probe")) {
			start = first - 1
		}
		var routes []string
		var entries []*Node
		for _, g := range ipGroups {
			routes = append(routes, g.route)
			entries = append(entries, args.Content[g.index])
		}
		probe := probeNeeded(routes, defaultForward)
		if probe {
			helpers := []string{"local_probe"}
			if direct {
				helpers = append(helpers, "default_direct_ip")
			}
			for _, helper := range helpers {
				if !equal(get(plugins[helper], "type"), S("sequence")) {
					return nil, nil, fail("分流执行链 " + helper + " 未配置，请在完整参数中添加后再调整。")
				}
			}
		}
		n := len(args.Content)
		args.Content = append(append(append([]*Node{}, args.Content[:start]...), ipSuffix(entries, defaultForward, probe)...), args.Content[n-1])
	}
	return result, clone(settings), nil
}

var _ = unicode.IsSpace
