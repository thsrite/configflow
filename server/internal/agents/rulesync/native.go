package rulesync

import (
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"fmt"
	"gopkg.in/yaml.v3"
	"slices"
	"strings"
)

func profile(doc *yaml.Node) (*oxidns.Profile, error) {
	raw, err := yamlx.ToJSON(doc)
	if err != nil {
		return nil, err
	}
	return oxidns.ProfileFromJSON(raw)
}
func mapping(n *yaml.Node) bool { n = yamlx.Resolve(n); return n != nil && n.Kind == yaml.MappingNode }
func parseMapping(raw []byte) (*yaml.Node, error) {
	n, err := yamlx.Parse(raw)
	if err != nil {
		return nil, err
	}
	if !mapping(n) {
		return nil, refuse(msgSnapshotVerify)
	}
	return yamlx.Resolve(n), nil
}
func nativeProjection(base, current, result *yaml.Node, s Settings, published, withheld map[string]bool) error {
	original := get(base, "oxidns")
	doc, err := oxidns.LoadNativeYAML(text(get(original, "native_config")), oxidns.NativeLabel)
	if err != nil {
		return err
	}
	p, err := profile(base)
	if err != nil {
		return err
	}
	before := oxidns.Analyze(doc, original, p)
	fields := []string{}
	for _, key := range selectors {
		if selected(s, selectorScope(key)) {
			fields = append(fields, key)
		}
	}
	if len(fields) == 0 {
		return nil
	}
	currentSettings := get(current, "oxidns")
	sameDocument := yamlx.Equal(get(currentSettings, "native_config"), get(original, "native_config"))
	if yamlx.Equal(sourceRuleState(base, false), sourceRuleState(result, false)) && sameDocument {
		return nil
	}
	for _, field := range fields {
		if !before.Capabilities[field].Editable {
			return refuse(msgNativeUnsafe)
		}
	}
	desired := before.Values
	if text(get(currentSettings, "config_mode")) == "native" && !sameDocument {
		nextDoc, e := oxidns.LoadNativeYAML(text(get(currentSettings, "native_config")), oxidns.NativeLabel)
		if e != nil {
			return e
		}
		cp, e := profile(current)
		if e != nil {
			return e
		}
		state := oxidns.Analyze(nextDoc, currentSettings, cp)
		for _, field := range fields {
			if !state.Capabilities[field].Editable {
				return refuse(msgDraftUnsafe)
			}
		}
		desired = state.Values
	} else if text(get(currentSettings, "config_mode")) != "native" {
		desired = obj()
		actions := get(currentSettings, "rule_actions")
		if !mapping(actions) {
			actions = obj()
		}
		yamlx.Set(desired, "rule_actions", actions)
		for _, key := range selectors {
			ids := yamlx.Seq()
			for _, id := range list(get(currentSettings, key)) {
				if get(actions, text(id)) == nil {
					ids.Content = append(ids.Content, id)
				}
			}
			yamlx.Set(desired, key, ids)
		}
	}
	// Analyzer results are private values; adding follow does not edit the source.
	yamlx.Set(desired, "rule_dns_follow", get(currentSettings, "rule_dns_follow"))
	known := scopes(result, current, base)
	values := dnsProjection(before.Values, desired, func(id string) bool { return selected(s, known[id]) }, published, withheld)
	selectedScopes := scopes(result)
	for _, field := range selectors {
		ids := []string{}
		for _, id := range list(get(values, field)) {
			if selectedScopes[text(id)] == selectorScope(field) {
				ids = append(ids, text(id))
			}
		}
		yamlx.Set(values, field, stringsNode(ids))
	}
	pruneDNS(values, selectedScopes)
	oldTags := map[string]bool{}
	for _, group := range before.Groups {
		oldTags[group.Tag] = true
		for _, key := range keys(group.Provider) {
			if !slices.Contains([]string{"tag", "type", "args"}, key) {
				return refuse(msgProviderParameters)
			}
		}
	}
	for tag := range oldTags {
		top := obj()
		for _, key := range keys(doc) {
			if key != "plugins" {
				yamlx.Set(top, key, get(doc, key))
			}
		}
		if oxidns.References(top, tag) || oxidns.References(get(before.Values, "custom_matches"), tag) {
			return refuse(msgProviderShared)
		}
		for _, plugin := range list(get(doc, "plugins")) {
			name := text(get(plugin, "tag"))
			if !oldTags[name] && name != "sequence_main" && name != "rules_reload" && oxidns.References(plugin, tag) {
				return refuse(msgProviderShared)
			}
		}
	}
	stringsOf := func(n *yaml.Node) *[]string {
		out := make([]string, 0, len(list(n)))
		for _, item := range list(n) {
			out = append(out, text(item))
		}
		return &out
	}
	actions := oxidns.RuleActionsFromNode(get(values, "rule_actions"))
	changes := oxidns.RoutingChanges{
		RuleActions:   &actions,
		RuleDNSFollow: stringsOf(get(values, "rule_dns_follow")),
	}
	for _, key := range fields {
		switch key {
		case "direct_rules":
			changes.DirectRules = stringsOf(get(values, key))
		case "proxy_rules":
			changes.ProxyRules = stringsOf(get(values, key))
		case "direct_rulesets":
			changes.DirectRulesets = stringsOf(get(values, key))
		case "proxy_rulesets":
			changes.ProxyRulesets = stringsOf(get(values, key))
		}
	}
	projectedSettings := get(result, "oxidns")
	rp, err := profile(result)
	if err != nil {
		return err
	}
	document, settings, err := oxidns.ApplyRouting(doc, projectedSettings, rp, changes, "", oxidns.RoutingOptions{BeforeConfig: p})
	if err != nil {
		return refuse(fmt.Sprintf(msgRoutingFailure, strings.TrimRight(err.Error(), "。")))
	}
	for _, plugin := range list(get(document, "plugins")) {
		tag := text(get(plugin, "tag"))
		if tag == "rules_download" && text(get(plugin, "type")) == "download" && get(get(plugin, "args"), "downloads") != nil && len(list(get(get(plugin, "args"), "downloads"))) == 0 || tag == "rules_reload" && text(get(plugin, "type")) == "reload_provider" && get(plugin, "args") != nil && len(list(get(plugin, "args"))) == 0 {
			return refuse(msgEmptyService)
		}
	}
	raw, err := yamlx.Marshal(document)
	if err != nil {
		return err
	}
	yamlx.Set(settings, "native_config", yamlx.Str(string(raw)))
	yamlx.Set(result, "oxidns", settings)
	return nil
}
