package oxidns

import (
	"configflow/internal/model"
	"encoding/json"
	"fmt"
	"strings"
)

// SyncSettings is the saved rule-library/rule synchronization selection of an Agent.
type SyncSettings struct {
	Library bool `json:"library"`
	Rules   bool `json:"rules"`
}

// RuleDNSReadOptions supplies an optional Agent subset and synchronization adapter.
// Nil Agents selects enabled Agents of the profile; an empty slice selects none.
type RuleDNSReadOptions struct {
	Agents       []model.Agent
	SyncSettings func(agent model.Agent) SyncSettings
}

// RuleDNSAgent describes an Agent shown by the rules page. Missing or null
// identity and service type remain null in JSON for legacy records. The display
// name preserves legacy JSON values without coercion or sharing mutable bytes.
type RuleDNSAgent struct {
	ID          *string         `json:"id"`
	Name        json.RawMessage `json:"name"`
	ServiceType *string         `json:"service_type"`
	AutoSync    SyncSettings    `json:"auto_sync"`
}

func agentText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// RuleDNSEngine describes the active profile's DNS routing capabilities.
type RuleDNSEngine struct {
	Mode             string             `json:"mode"`
	Editable         bool               `json:"editable"`
	Reason           string             `json:"reason"`
	DefaultForward   string             `json:"default_forward"`
	PolicyRoutes     map[string]*string `json:"policy_routes"`
	PreferIPv4       bool               `json:"prefer_ipv4"`
	LocalProbeActive bool               `json:"local_probe_active"`
	InUse            bool               `json:"oxidns_in_use"`
	Warnings         []string           `json:"warnings"`
	Agents           []RuleDNSAgent     `json:"agents"`
}

// RuleDNSRule is one rule's DNS read model. Nil routes mean the default upstream.
type RuleDNSRule struct {
	State         string
	Reason        string
	Route         *string
	Follow        bool
	FollowRoute   *string
	UpstreamLabel string
	InLists       bool
}

// MarshalJSON keeps absent fields distinct from a present null route.
func (r RuleDNSRule) MarshalJSON() ([]byte, error) {
	var route, followRoute **string
	if r.State != "unsupported" {
		route = &r.Route
	}
	if r.State == "follow" || r.State == "follow_drift" {
		followRoute = &r.FollowRoute
	}
	return json.Marshal(struct {
		State         string   `json:"state"`
		Reason        string   `json:"reason,omitempty"`
		Route         **string `json:"route,omitempty"`
		Follow        bool     `json:"follow,omitempty"`
		FollowRoute   **string `json:"follow_route,omitempty"`
		UpstreamLabel string   `json:"upstream_label,omitempty"`
		InLists       bool     `json:"in_lists,omitempty"`
	}{r.State, r.Reason, route, r.Follow, followRoute, r.UpstreamLabel, r.InLists})
}

// UpstreamChoice is an available pinned-upstream selection shown by the rules page.
type UpstreamChoice struct {
	Key    string `json:"key"`
	Source string `json:"source"`
	Addr   string `json:"addr"`
	Label  string `json:"label"`
	Linked bool   `json:"linked"`
}

// UpstreamCandidate includes the complete upstream object, preserving unknown fields.
type UpstreamCandidate struct {
	UpstreamChoice
	Entry json.RawMessage `json:"entry"`
}

// DNSBinding identifies an upstream managed by DNS follow.
type DNSBinding struct {
	UpstreamTag   string `json:"upstream_tag"`
	UpstreamIndex int    `json:"upstream_index"`
}

// RuleDNSState is the JSON read model consumed by the rules page.
type RuleDNSState struct {
	Engine    RuleDNSEngine          `json:"engine"`
	Upstreams []UpstreamChoice       `json:"upstreams"`
	Rules     map[string]RuleDNSRule `json:"rules"`
	MosDNS    map[string]string      `json:"mosdns"`
}

// ProfileAgents returns copies of enabled Agents bound to the resolved profile.
func ProfileAgents(p *Profile) []model.Agent {
	if p == nil {
		return []model.Agent{}
	}
	defaultID := p.defaultProfileID()
	profileID := p.profileID()
	if profileID == "" {
		profileID = defaultID
	}
	out := []model.Agent{}
	for _, agent := range p.agents() {
		id := agent.ProfileID
		if id == "" {
			id = defaultID
		}
		if model.Flag(agent.Enabled, true) && id == profileID {
			out = append(out, agent)
		}
	}
	return out
}

// PolicyDNSRoutes returns configured group defaults; an empty route means no DNS route.
func PolicyDNSRoutes(p *Profile) map[string]string {
	if p == nil {
		p = emptyProfile()
	}
	return policyDNSRoutes(p)
}

// FollowRoute returns the route implied by an item's policy; empty means no route.
func FollowRoute(item model.RuleConfig, p *Profile) string {
	routes := map[string]string{}
	if p != nil {
		routes = policyDNSRoutes(p)
	}
	return followRoute(item, routes)
}

// HydrateRuleset returns a profile reference with its shared library source updated.
// Neither the original reference nor the library entry is modified.
func HydrateRuleset(item, library *Node, profileID string) *Node {
	out := clone(item)
	fields := []string{"name", "url", "behavior", "content", "source_type", "format"}
	for _, field := range append(append([]string{}, fields...), "library_enabled") {
		del(out, field)
	}
	for _, field := range fields {
		if has(library, field) {
			set(out, field, clone(get(library, field)))
		}
	}
	if !has(out, "behavior") {
		set(out, "behavior", S("domain"))
	}
	set(out, "library_enabled", clone(orDefault(library, "enabled", B(true))))
	if isOneOf(get(library, "source_type"), "content") {
		if profileID == "" {
			profileID = "default"
		}
		set(out, "url", S("/api/profiles/"+profileID+"/rule-library/content/"+str(get(library, "id"))))
	}
	return out
}

func upstreamLabel(source, addr string) string {
	return dnsSourceLabels[source] + " · " + strings.TrimPrefix(addr, "udp://")
}

func ruleDNSIDs(list *Node) []string {
	out := []string{}
	for _, item := range items(list) {
		if isStr(item) {
			out = append(out, item.Value)
		}
	}
	return out
}

// UpstreamCatalog returns current local/remote/fallback upstreams in source order.
// A nil document reads template lists or loads the saved native document.
func UpstreamCatalog(settings, document *nativeDoc, bindings []DNSBinding) ([]UpstreamCandidate, error) {
	lists := map[string]*Node{}
	if document == nil && !isOneOf(get(settings, "config_mode"), "native") {
		for _, source := range UpstreamSources {
			var list *Node
			var err error
			text := str(get(settings, source+"_dns"))
			switch source {
			case "local":
				list, err = forwardUpstreams(text, LocalDNSDefaults)
			case "remote":
				list, err = forwardUpstreams(text, RemoteDNSDefaults)
			case "fallback":
				list, err = fallbackUpstreams(text)
			}
			if err == nil {
				lists[source] = list
			}
		}
	} else {
		if document == nil {
			var err error
			document, err = loadNative(get(settings, "native_config"), NativeLabel)
			if err != nil {
				return nil, err
			}
		}
		for _, source := range UpstreamSources {
			var found *Node
			count := 0
			for _, plugin := range items(get(document, "plugins")) {
				if isMap(plugin) && isOneOf(get(plugin, "tag"), sourceTags[source]) {
					found = plugin
					count++
				}
			}
			if count == 1 && isOneOf(get(found, "type"), "forward") {
				lists[source] = at(found, "args", "upstreams")
			}
		}
	}
	linked := map[DNSBinding]bool{}
	for _, b := range bindings {
		linked[b] = true
	}
	out := []UpstreamCandidate{}
	indices := map[string]int{}
	for _, source := range UpstreamSources {
		for index, entry := range items(lists[source]) {
			addr := get(entry, "addr")
			if !isMap(entry) || !isStr(addr) || strings.TrimSpace(addr.Value) == "" {
				continue
			}
			key := EntryKey(entry, source)
			bound := linked[DNSBinding{sourceTags[source], index}]
			if i, exists := indices[key]; exists {
				out[i].Linked = out[i].Linked || bound
				continue
			}
			indices[key] = len(out)
			out = append(out, UpstreamCandidate{UpstreamChoice{key, source, addr.Value, upstreamLabel(source, addr.Value), bound}, ToJSON(entry)})
		}
	}
	return out, nil
}

func storedRuleDNS(settings *Node) *Node {
	values := M()
	for _, key := range Selectors {
		set(values, key, strs(ruleDNSIDs(get(settings, key))))
	}
	for _, key := range ruleDNSFields[:2] {
		actions := M()
		pairs(get(settings, key), func(id string, value *Node) bool {
			if action, err := NormalizeAction(value, key == "rule_dns_parked"); err == nil {
				set(actions, id, action)
			}
			return true
		})
		set(values, key, actions)
	}
	set(values, "rule_dns_follow", unique(strs(ruleDNSIDs(get(settings, "rule_dns_follow")))))
	return values
}

var ruleDNSFields = []string{"rule_actions", "rule_dns_parked", "rule_dns_follow"}

type ruleDNSView struct {
	Values   *Node
	Editable bool
	Reason   string
	Document *Node
	Routing  *Node
}

func readRuleDNS(p *Profile, settings *Node) (ruleDNSView, error) {
	view := ruleDNSView{Values: storedRuleDNS(settings), Editable: true}
	if !isOneOf(get(settings, "config_mode"), "native") {
		return view, nil
	}
	document, err := loadNative(get(settings, "native_config"), NativeLabel)
	if err != nil {
		view.Editable = false
		view.Reason = err.Error()
		return view, nil
	}
	routing, err := ReadRouting(document, settings, p)
	if err != nil {
		return view, err
	}
	for _, key := range append(append([]string{}, Selectors...), "rule_actions", "rule_dns_follow") {
		set(view.Values, key, clone(get(routing.Values, key)))
	}
	capability := routing.Capabilities["direct_rules"]
	view.Document, view.Routing = document, routing.Values
	view.Editable, view.Reason = capability.Editable, capability.Reason
	return view, nil
}

func ruleRoute(values *Node, id string) string {
	if action := get(get(values, "rule_actions"), id); action != nil {
		return RouteKey(action)
	}
	for _, route := range ListRoutes {
		for _, suffix := range []string{"rules", "rulesets"} {
			if contains(get(values, route+"_"+suffix), S(id)) {
				return route
			}
		}
	}
	return ""
}

func nullableRoute(route string) *string {
	if route == "" {
		return nil
	}
	return &route
}

func dnsBindings(agents []model.Agent) []DNSBinding {
	out := []DNSBinding{}
	for _, agent := range agents {
		binding := agent.DNSFollow
		if agent.ServiceType == "oxidns" && binding != nil && binding.UpstreamIndex >= 0 && binding.UpstreamTag != "" {
			out = append(out, DNSBinding{binding.UpstreamTag, int(binding.UpstreamIndex)})
		}
	}
	return out
}

func viewUpstreams(settings *Node, view ruleDNSView, agents []model.Agent) ([]UpstreamCandidate, error) {
	if isOneOf(get(settings, "config_mode"), "native") && view.Document == nil {
		return []UpstreamCandidate{}, nil
	}
	return UpstreamCatalog(settings, view.Document, dnsBindings(agents))
}

// ReadRuleDNS computes the rules page without mutating the profile.
func ReadRuleDNS(p *Profile, options RuleDNSReadOptions) (RuleDNSState, error) {
	if p == nil {
		p = emptyProfile()
	}
	settings := p.Settings()
	view, err := readRuleDNS(p, settings)
	if err != nil {
		return RuleDNSState{}, err
	}
	stored := storedRuleDNS(settings)
	agents := options.Agents
	if agents == nil {
		agents = ProfileAgents(p)
	}
	routes := policyDNSRoutes(p)
	active := map[string]bool{}
	for _, item := range p.activeItems() {
		active[item.ID] = true
	}
	native := isOneOf(get(settings, "config_mode"), "native")
	state := RuleDNSState{Rules: map[string]RuleDNSRule{}, Upstreams: []UpstreamChoice{}, MosDNS: map[string]string{}}
	for _, item := range p.ruleConfigs() {
		id := item.ID
		if !convertible(item) {
			kind := item.RuleType
			if kind == "" {
				kind = model.TextValue(item.Behavior)
			}
			if kind == "" {
				kind = messageDNSUnknownKind
			}
			reason := fmt.Sprintf(messageDNSUnsupportedKind, kind)
			if IsIPRuleType(kind) || kind == "ipcidr" {
				reason = messageDNSIPUnsupported
			}
			state.Rules[id] = RuleDNSRule{State: "unsupported", Reason: reason}
			continue
		}
		route := ruleRoute(view.Values, id)
		action := get(get(view.Values, "rule_actions"), id)
		follow := contains(get(view.Values, "rule_dns_follow"), S(id))
		entry := RuleDNSRule{State: "explicit"}
		switch {
		case !active[id]:
			if follow {
				route, action = followRoute(item, routes), nil
			} else if native && has(get(view.Values, "rule_dns_parked"), id) {
				action = get(get(view.Values, "rule_dns_parked"), id)
				route = RouteKey(action)
			}
			entry.State, entry.Follow = "disabled", follow
		case follow:
			expected := followRoute(item, routes)
			entry.State, entry.FollowRoute = "follow", nullableRoute(expected)
			if route != expected {
				entry.State = "follow_drift"
			}
		case route == "":
			continue
		}
		entry.Route = nullableRoute(route)
		if isOneOf(get(action, "action"), "upstream") {
			entry.UpstreamLabel = upstreamLabel(str(get(action, "source")), str(at(action, "upstream", "addr")))
		}
		if !native && has(get(view.Values, "rule_actions"), id) {
			for _, key := range Selectors {
				entry.InLists = entry.InLists || contains(get(stored, key), S(id))
			}
		}
		state.Rules[id] = entry
	}
	engine := RuleDNSEngine{Mode: str(orDefault(settings, "config_mode", S("template"))), Editable: view.Editable, Reason: view.Reason,
		DefaultForward: str(orDefault(settings, "default_forward", S("forward_remote"))), PolicyRoutes: map[string]*string{}, PreferIPv4: !same(get(settings, "prefer_ipv4"), B(false)),
		InUse: native, Warnings: []string{}, Agents: []RuleDNSAgent{}}
	for name, route := range routes {
		engine.PolicyRoutes[name] = nullableRoute(route)
	}
	for _, key := range append(append([]string{}, Selectors...), ruleDNSFields...) {
		engine.InUse = engine.InUse || (len(items(get(stored, key))) > 0 || isMap(get(stored, key)) && mapSize(get(stored, key)) > 0)
	}
	var steps []*Node
	for _, plugin := range items(get(view.Document, "plugins")) {
		if isMap(plugin) && isOneOf(get(plugin, "tag"), "sequence_main") {
			steps = items(get(plugin, "args"))
			break
		}
	}
	if native {
		engine.PreferIPv4 = false
		for _, step := range steps {
			engine.PreferIPv4 = engine.PreferIPv4 || isOneOf(get(step, "exec"), "prefer_ipv4")
			engine.LocalProbeActive = engine.LocalProbeActive || isOneOf(get(step, "exec"), "jump local_probe")
		}
	}
	if forward := get(view.Routing, "default_forward"); str(forward) != "" {
		engine.DefaultForward = str(forward)
	}
	engine.Warnings = cacheWarnings(settings, view)
	for _, agent := range agents {
		var sync SyncSettings
		if agent.RuleSync != nil {
			sync = SyncSettings{model.Flag(agent.RuleSync.Settings.Library, false), model.Flag(agent.RuleSync.Settings.Rules, false)}
		}
		if options.SyncSettings != nil {
			sync = options.SyncSettings(agent)
		}
		name := agent.Name
		if name == "" {
			name = agent.ID
		}
		display, err := json.Marshal(agentText(name))
		if err != nil {
			return RuleDNSState{}, fmt.Errorf("encode Agent name: %w", err)
		}
		engine.Agents = append(engine.Agents, RuleDNSAgent{agentText(agent.ID), display, agentText(agent.ServiceType), sync})
		engine.InUse = engine.InUse || agent.ServiceType == "oxidns"
	}
	candidates, err := viewUpstreams(settings, view, agents)
	if err != nil {
		return RuleDNSState{}, err
	}
	for _, candidate := range candidates {
		state.Upstreams = append(state.Upstreams, candidate.UpstreamChoice)
	}
	selectors := p.source.Profile.MosDNS
	for _, selector := range []struct {
		route string
		ids   []string
	}{{"direct", selectors.DirectRules}, {"direct", selectors.DirectRulesets}, {"proxy", selectors.ProxyRules}, {"proxy", selectors.ProxyRulesets}} {
		for _, id := range selector.ids {
			state.MosDNS[id] = selector.route
		}
	}
	state.Engine = engine
	return state, nil
}

func cacheWarnings(settings *Node, view ruleDNSView) []string {
	matches, position := get(settings, "custom_matches"), str(get(settings, "custom_match_position"))
	types := map[string]string{"lazy_cache": "cache", "local_probe_cache": "cache"}
	if view.Routing != nil {
		matches, position = get(view.Routing, "custom_matches"), str(get(view.Routing, "custom_match_position"))
		types = map[string]string{}
		for _, plugin := range items(get(view.Document, "plugins")) {
			if isMap(plugin) {
				types[str(get(plugin, "tag"))] = str(get(plugin, "type"))
			}
		}
	}
	if position == "head" {
		for _, entry := range items(matches) {
			for _, word := range strings.Fields(str(get(entry, "exec"))) {
				if types[strings.TrimLeft(word, "$")] == "cache" {
					return []string{messageDNSCachedHead}
				}
			}
		}
	}
	return []string{}
}

// ExplicitRuleDNS returns a rule's own route, including a parked choice, or empty.
func ExplicitRuleDNS(p *Profile, id string) (string, error) {
	if p == nil {
		p = emptyProfile()
	}
	view, err := readRuleDNS(p, p.Settings())
	if err != nil {
		return "", err
	}
	if contains(get(view.Values, "rule_dns_follow"), S(id)) {
		return "", nil
	}
	route := ruleRoute(view.Values, id)
	if parked := get(get(view.Values, "rule_dns_parked"), id); route == "" && parked != nil {
		route = RouteKey(parked)
	}
	return route, nil
}

// RuleDNSEntries returns selected read-model entries with explicit default entries.
func RuleDNSEntries(p *Profile, ids []string, options RuleDNSReadOptions) (map[string]RuleDNSRule, error) {
	state, err := ReadRuleDNS(p, options)
	if err != nil {
		return nil, err
	}
	out := make(map[string]RuleDNSRule, len(ids))
	for _, id := range ids {
		entry, ok := state.Rules[id]
		if !ok {
			entry = RuleDNSRule{State: "default"}
		}
		out[id] = entry
	}
	return out, nil
}
