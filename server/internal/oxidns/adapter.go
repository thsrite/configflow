package oxidns

import (
	"context"
	"encoding/json"
	"errors"
	"sync"

	"configflow/internal/model"
	"configflow/internal/mosdns"
	"configflow/internal/yamlx"
)

// Profile is an owned DNS projection of one captured typed repository view.
// OxiDNS settings are deliberately schema-free; resource metadata is typed.
type Profile struct {
	doc             *Node
	source          *model.Config
	notesChanged    func(*Node)
	presenceMu      sync.Mutex
	presenceVersion uint64
	presence        nativeIPResolver
}

// NewTypedProfile projects the consumed members without encoding model.Raw or
// unrelated settings, subscriptions and Agent deployment histories.
func NewTypedProfile(source *model.Config) (*Profile, error) {
	if source == nil || source.Profile == nil {
		source = &model.Config{Profile: &model.Profile{}}
	}
	profile := source.Profile
	doc := M("profile_id", source.ProfileID)
	settings, err := profile.OxiDNS.Node()
	if err != nil {
		return nil, err
	}
	if !isNone(settings) {
		set(doc, "oxidns", settings)
	}
	rules := L()
	for _, item := range profile.RuleConfigs {
		rules.Content = append(rules.Content, ruleNode(item))
	}
	set(doc, "rule_configs", rules)
	for _, key := range []string{"rules", "rule_sets"} {
		if raw, ok := profile.Fields.Get(key); ok {
			var container model.Profile
			if err := container.Fields.Set(key, raw); err != nil {
				return nil, err
			}
			rules, sets, err := mosdns.SplitRules(&model.Config{Profile: &container})
			if err != nil {
				return nil, err
			}
			values := rules
			if key == "rule_sets" {
				values = sets
			}
			list := L()
			for _, item := range values {
				list.Content = append(list.Content, ruleNode(item))
			}
			set(doc, key, list)
		}
	}
	system := source.SystemConfig
	set(doc, "system_config", M("server_domain", system.ServerDomain, "rule_proxy_token", system.RuleProxyToken, "config_token", system.ConfigToken, "default_profile_id", text(system.DefaultProfileID, "default"), "node_test_dns", strs(system.NodeTestDNS)))
	if system.NodeTestDNSConfig != nil {
		v := system.NodeTestDNSConfig
		set(get(doc, "system_config"), "node_test_dns_config", M("mode", v.Mode, "servers", strs(v.Servers)))
	}
	groups := L()
	for _, group := range profile.ProxyGroups {
		node, err := groupNode(group)
		if err != nil {
			return nil, err
		}
		groups.Content = append(groups.Content, node)
	}
	set(doc, "proxy_groups", groups)
	selectors := profile.MosDNS
	set(doc, "mosdns", M("direct_rulesets", strs(selectors.DirectRulesets), "proxy_rulesets", strs(selectors.ProxyRulesets), "direct_rules", strs(selectors.DirectRules), "proxy_rules", strs(selectors.ProxyRules)))
	if profile.DeviceNotes.Len() > 0 {
		notes := M()
		for key, value := range profile.DeviceNotes.All() {
			set(notes, key, S(value))
		}
		set(doc, "device_notes", notes)
	}
	agents := L()
	for _, agent := range source.Agents {
		// Agent rows are an opaque foreign-owner DTO: public display names and the
		// rulesync callback require original fields. No Agent policy is duplicated.
		data, err := model.Marshal(agent)
		if err != nil {
			return nil, err
		}
		row, err := yamlx.FromJSON(data)
		if err != nil {
			return nil, err
		}
		agents.Content = append(agents.Content, row)
	}
	if source.HasAgents {
		set(doc, "agents", agents)
	}
	nodes := L()
	for _, node := range source.Nodes {
		nodes.Content = append(nodes.Content, M("id", node.ID, "name", node.Name))
	}
	set(doc, "nodes", nodes)
	subscriptions := L()
	for _, item := range source.Subscriptions {
		subscriptions.Content = append(subscriptions.Content, M("id", item.ID, "name", item.Name))
	}
	set(doc, "subscriptions", subscriptions)
	aggregations := L()
	for _, item := range source.Aggregations {
		aggregations.Content = append(aggregations.Content, M("id", item.ID, "name", item.Name, "enabled", enabled(item.Enabled), "subscriptions", strs(item.Subscriptions)))
	}
	set(doc, "subscription_aggregations", aggregations)

	return &Profile{doc: doc, source: source}, nil
}

func text(value *string, fallback string) string {
	if value == nil {
		return fallback
	}
	return *value
}
func enabled(value *bool) bool { return value == nil || *value }
func ruleNode(item model.RuleConfig) *Node {
	out := M("id", item.ID, "itemType", item.ItemType, "name", item.Name, "rule_type", item.RuleType, "value", item.Value, "enabled", enabled(item.Enabled), "library_enabled", enabled(item.LibraryEnabled))
	if item.LibraryRuleID != "" {
		set(out, "library_rule_id", S(item.LibraryRuleID))
	}
	for _, field := range []struct {
		key   string
		value *string
	}{{"url", item.URL}, {"behavior", item.Behavior}, {"content", item.Content}, {"source_type", item.SourceType}, {"format", item.Format}, {"policy", item.Policy}} {
		if field.value != nil {
			set(out, field.key, S(*field.value))
		}
	}
	return out
}
func libraryNode(item model.RuleLibraryItem) *Node {
	out := M("id", item.ID, "name", item.Name, "enabled", enabled(item.Enabled))
	for _, field := range []struct {
		key   string
		value *string
	}{{"url", item.URL}, {"behavior", item.Behavior}, {"content", item.Content}, {"source_type", item.SourceType}, {"format", item.Format}} {
		if field.value != nil {
			set(out, field.key, S(*field.value))
		}
	}
	return out
}
func groupNode(group model.ProxyGroup) (*Node, error) {
	// A DTO alias excludes storage's preservation marshaler. Known consumed
	// values have already passed the shared R5 decoder; private Raw is omitted.
	type groupDTO model.ProxyGroup
	value := groupDTO(group)
	value.Fields = model.Fields{}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	node, err := yamlx.FromJSON(data)
	if err != nil {
		return nil, err
	}
	del(node, "Fields")
	for _, key := range keys(node) {
		if isNone(get(node, key)) {
			del(node, key)
		}
	}
	set(node, "enabled", B(enabled(group.Enabled)))
	return node, nil
}

// ProfileFromJSON decodes only DNS/profile metadata; unrelated fields do not
// enter model.Profile.Raw or trigger whole-configuration decoding.
func ProfileFromJSON(data []byte) (*Profile, error) {
	var fields model.Fields
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, err
	}
	var selected model.Fields
	for _, key := range []string{"oxidns", "mosdns", "rule_configs", "rules", "rule_sets", "proxy_groups", "device_notes"} {
		if raw, ok := fields.Get(key); ok {
			if err := selected.Set(key, raw); err != nil {
				return nil, err
			}
		}
	}
	data, err := selected.MarshalJSON()
	if err != nil {
		return nil, err
	}
	profile := new(model.Profile)
	if err = json.Unmarshal(data, profile); err != nil {
		return nil, err
	}
	source := &model.Config{Profile: profile}
	if raw, ok := fields.Get("profile_id"); ok {
		var projection model.Fields
		if err = projection.Set("profile_id", raw); err != nil {
			return nil, err
		}
		data, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var identity profileIdentity
		if err = json.Unmarshal(data, &identity); err != nil {
			return nil, err
		}
		source.ProfileID = identity.ID
	}
	if raw, ok := fields.Get("system_config"); ok {
		var projection model.Fields
		if err = projection.Set("system_config", raw); err != nil {
			return nil, err
		}
		data, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var system model.System
		if err = json.Unmarshal(data, &system); err != nil {
			return nil, err
		}
		source.SystemConfig = system.SystemConfig
	}
	if raw, ok := fields.Get("rule_library"); ok {
		var shared model.Shared
		var projection model.Fields
		if err = projection.Set("rule_library", raw); err != nil {
			return nil, err
		}
		bytes, err := projection.MarshalJSON()
		if err != nil {
			return nil, err
		}
		if err = json.Unmarshal(bytes, &shared); err != nil {
			return nil, err
		}
		source.RuleLibrary = shared.RuleLibrary
	}
	if raw, ok := fields.Get("agents"); ok {
		if err = json.Unmarshal(raw, &source.Agents); err != nil {
			return nil, err
		}
		source.HasAgents = true
	}
	var resources model.Fields
	for _, key := range []string{"nodes", "subscriptions", "subscription_aggregations"} {
		if raw, ok := fields.Get(key); ok {
			if err = resources.Set(key, raw); err != nil {
				return nil, err
			}
		}
	}
	resourceData, err := resources.MarshalJSON()
	if err != nil {
		return nil, err
	}
	var shared model.Shared
	if err = json.Unmarshal(resourceData, &shared); err != nil {
		return nil, err
	}
	source.Nodes = shared.Nodes
	source.Subscriptions = shared.Subscriptions
	source.Aggregations = shared.Aggregations
	return NewTypedProfile(source)
}
func (p *Profile) get(key string) *Node {
	if p == nil {
		return nil
	}
	return get(p.doc, key)
}

// Settings returns the owned settings tree; missing settings are nil.
func (p *Profile) Settings() *Node { return p.get("oxidns") }
func (p *Profile) setDeviceNotes(notes *Node) {
	set(p.doc, "device_notes", notes)
	if p.notesChanged != nil {
		p.notesChanged(notes)
	}
}

var errUnsupportedYAML = fail("OxiDNS YAML 只支持普通对象、数组和标量")

func safeLoad(text string) (*Node, bool, error) {
	root, err := yamlx.Parse([]byte(text))
	if err != nil {
		if errors.Is(err, yamlx.ErrSyntax) {
			return nil, false, nil
		}
		return nil, false, yamlReaderFailure(err)
	}
	yamlx.MaterializeAliases(root)
	return root, true, nil
}
func (p *Profile) splitRules() (*Node, *Node) {
	rules, sets, err := mosdns.SplitRules(p.source)
	if err != nil {
		return L(), L()
	}
	r, s := L(), L()
	for _, item := range rules {
		r.Content = append(r.Content, ruleNode(item))
	}
	for _, item := range sets {
		s.Content = append(s.Content, ruleNode(item))
	}
	return r, s
}
func (p *Profile) ruleProxyURL(baseURL, url, part string) (string, error) {
	source := &model.Config{Profile: &model.Profile{}, ProfileID: str(p.get("profile_id")), SystemConfig: model.SystemConfig{RuleProxyToken: str(get(p.get("system_config"), "rule_proxy_token"))}}
	return mosdns.RuleProxyURL(source, baseURL, url, part, "")
}
func rulesetParts(behavior *Node) []string { return mosdns.Parts(str(behavior)) }
func convertRule(text, part string) (string, error) {
	return mosdns.ConvertRuleContentForMosDNS(text, part)
}
func parseUpstreams(text string) (*Node, error) {
	nodes, err := mosdns.Upstreams(text)
	if err != nil {
		return nil, err
	}
	return seqOf(nodes), nil
}
func parseCustomMatches(list *Node) (*Node, error) {
	var settings model.MosDNSSettings
	data := append([]byte(`{"custom_matches":`), ToJSON(list)...)
	data = append(data, '}')
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, err
	}
	return seqOf(mosdns.MatchEntries(settings.CustomMatches)), nil
}
func (p *Profile) renderMembers(group *Node) *Node { return legacyMembers(group, p.doc) }

// NativeClassicalIPLinesFunc queries offline content from typed metadata only.
type NativeClassicalIPLinesFunc func(context.Context, model.RuleConfig, []model.RuleLibraryItem) (*bool, error)

type nativeIPResolver func(context.Context, model.RuleConfig) (*bool, error)
type nativeIPFactory func([]model.RuleLibraryItem) nativeIPResolver

var classicalHook struct {
	sync.RWMutex
	factory nativeIPFactory
	version uint64
}

// SetNativeClassicalIPLines installs the typed offline metadata callback.
func SetNativeClassicalIPLines(fn NativeClassicalIPLinesFunc) func() {
	if fn == nil {
		return setIPFactory(nil)
	}
	return setIPFactory(func(library []model.RuleLibraryItem) nativeIPResolver {
		return func(ctx context.Context, item model.RuleConfig) (*bool, error) { return fn(ctx, item, library) }
	})
}
func setIPFactory(factory nativeIPFactory) func() {
	classicalHook.Lock()
	previous := classicalHook.factory
	classicalHook.factory = factory
	classicalHook.version++
	classicalHook.Unlock()
	return func() {
		classicalHook.Lock()
		classicalHook.factory = previous
		classicalHook.version++
		classicalHook.Unlock()
	}
}
func (p *Profile) classicalWithoutIP(item *Node) bool {
	classicalHook.RLock()
	factory, version := classicalHook.factory, classicalHook.version
	classicalHook.RUnlock()
	if factory == nil {
		return false
	}
	p.presenceMu.Lock()
	if p.presence == nil || p.presenceVersion != version {
		p.presence = factory(p.source.RuleLibrary)
		p.presenceVersion = version
	}
	resolver := p.presence
	p.presenceMu.Unlock()
	rule := model.RuleConfig{ID: str(get(item, "id")), Name: str(get(item, "name")), LibraryRuleID: str(get(item, "library_rule_id"))}
	for _, member := range []struct {
		key         string
		destination **string
	}{{"url", &rule.URL}, {"format", &rule.Format}, {"source_type", &rule.SourceType}, {"content", &rule.Content}, {"behavior", &rule.Behavior}} {
		if value := get(item, member.key); isStr(value) {
			text := str(value)
			*member.destination = &text
		}
	}
	value, err := resolver(context.Background(), rule)
	return err == nil && value != nil && !*value
}

func emptyProfile() *Profile {
	return &Profile{doc: M(), source: &model.Config{Profile: &model.Profile{}}}
}

type profileIdentity struct {
	ID     string `json:"profile_id"`
	Fields model.Fields
	raw    model.Raw
}

func (v *profileIdentity) UnmarshalJSON(data []byte) error {
	return model.DecodeRecord(data, v, &v.raw)
}
