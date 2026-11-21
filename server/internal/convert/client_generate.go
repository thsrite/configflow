package convert

import (
	"context"
	"fmt"
	"strings"

	"configflow/internal/model"
	"configflow/internal/yamlx"
)

func clientPreflight(ctx context.Context, config *model.Config, target string) error {
	if config == nil || config.Profile == nil {
		return fmt.Errorf("%s", msgClientConfig)
	}
	if err := validateRuleIdentities(config.Profile.RuleConfigs); err != nil {
		return err
	}
	if err := ConfigDialers(config); err != nil {
		return &DialerReferenceError{Message: err.Error(), cause: err}
	}
	env := CurrentMihomoEnvironment()
	converter := env.Converter
	if converter == nil {
		converter = missingClientNodeConverter{}
	}
	if err := RejectUnsupportedDialers(ctx, config, target, converter); err != nil {
		return &DialerReferenceError{Message: err.Error(), cause: err}
	}
	if env.ValidateRules == nil {
		return &HookMissingError{Hook: "validate_rule_policies", Provider: "convert/mihomoenv"}
	}
	return nil
}

// SurgeConfig builds the complete client document from a resolved typed view.
func SurgeConfig(ctx context.Context, config *model.Config, base string) (string, error) {
	ctx = EnsureConversions(ctx)
	if err := clientPreflight(ctx, config, "Surge"); err != nil {
		return "", err
	}
	if _, _, err := MihomoRuleLists(config); err != nil {
		return "", err
	}
	custom := splitClientSections(config.Profile.Surge.CustomConfig)
	general, _ := clientSection(custom, "General")
	if general == "" {
		general = strings.Join(SurgeDefaultGeneral, "\n")
	}
	if fallback := FallbackPolicy(config); fallback != "" {
		general = SetGeneralOption(general, "udp-policy-not-supported-behaviour", fallback)
	}
	selected, direct, aggregations, onlyAggregation := map[string]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, group := range config.Profile.ProxyGroups {
		if !enabled(&group, group.Enabled) {
			continue
		}
		source, err := clientGroupSource(group, config)
		if err != nil {
			return "", err
		}
		if source == nil {
			continue
		}
		for _, id := range stringList(source.ManualNodes) {
			if clientNodeRef(id).kind == "node" {
				selected[id] = true
				direct[id] = true
			}
		}
		for _, id := range stringList(source.Aggregations) {
			aggregations[id] = true
		}
		for _, ref := range refList(group.ProxiesOrder) {
			if ref.Type == "node" && clientNodeRef(ref.ID).kind == "node" {
				selected[ref.ID] = true
			}
		}
		for _, ref := range refList(source.ProxiesOrder) {
			if ref.Type == "node" && clientNodeRef(ref.ID).kind == "node" {
				direct[ref.ID] = true
			}
		}
	}
	for _, a := range config.Aggregations {
		if enabled(&a, a.Enabled) && aggregations[a.ID] {
			for _, id := range a.Nodes {
				if !direct[id] {
					onlyAggregation[id] = true
				}
			}
		}
	}
	proxies := []string{"[Proxy]"}
	var wireguard []string
	for _, node := range config.Nodes {
		if !enabled(&node, node.Enabled) || !selected[node.ID] || onlyAggregation[node.ID] {
			continue
		}
		line, section, err := SurgeNode(node)
		if err != nil {
			return "", err
		}
		if line != "" {
			proxies = append(proxies, line)
		}
		if section != "" {
			wireguard = append(wireguard, section)
		}
	}
	priorities := map[string]string{}
	for _, smart := range config.Profile.Surge.SmartGroups {
		priorities[smart.GroupID] = smart.PolicyPriority
	}
	groups := []string{"[Proxy Group]"}
	for _, group := range config.Profile.ProxyGroups {
		if !enabled(&group, group.Enabled) {
			continue
		}
		line, err := SurgeGroup(group, config, base, priorities)
		if err != nil {
			return "", err
		}
		if line != "" {
			groups = append(groups, line)
		}
	}
	if len(groups) == 1 {
		var names []string
		for _, node := range config.Nodes {
			if enabled(&node, node.Enabled) {
				names = append(names, sanitizeClientName(node.Name))
			}
		}
		if len(names) > 0 {
			joined := strings.Join(names, ", ")
			groups = append(groups, "Proxy = select, "+joined, "Auto = url-test, "+joined+", url = https://www.gstatic.com/generate_204, interval = 300")
		}
	}
	effective := clientBaseURL(config, base)
	rules, err := SurgeRules(config, effective)
	if err != nil {
		return "", err
	}
	sections := []string{general, strings.Join(proxies, "\n"), strings.Join(groups, "\n"), strings.Join(rules, "\n")}
	sections = append(sections, wireguard...)
	for _, section := range custom {
		switch section.name {
		case "General", "Proxy", "Proxy Group", "Rule":
		default:
			sections = append(sections, section.text)
		}
	}
	policies := clientPolicyNames(append(append([]string{}, proxies[1:]...), groups[1:]...))
	if err := CurrentMihomoEnvironment().ValidateRules(config, policies, "surge"); err != nil {
		return "", err
	}
	address := effective + clientAPIPath(config, "surge")
	if token := config.SystemConfig.ConfigToken; token != "" {
		address, err = appendProfileQuery(address, [][2]string{{"token", token}})
		if err != nil {
			return "", err
		}
	}
	return "#!MANAGED-CONFIG " + address + " interval=86400 strict=true\n\n" + strings.Join(sections, "\n\n"), nil
}

type clientNameAllocator struct{ used map[string]bool }

func clientNames(reserved []string) *clientNameAllocator {
	allocator := &clientNameAllocator{map[string]bool{}}
	for _, name := range append(append([]string{}, reserved...), LoonBuiltinPolicies...) {
		allocator.used[name] = true
	}
	return allocator
}
func (a *clientNameAllocator) allocate(preferred, suffix string) string {
	name := clientSanitizeName(preferred)
	if name == "" {
		name = suffix
	}
	if !a.used[name] {
		a.used[name] = true
		return name
	}
	candidate := name + "-" + suffix
	for index := 2; a.used[candidate]; index++ {
		candidate = fmt.Sprintf("%s-%s%d", name, suffix, index)
	}
	a.used[candidate] = true
	return candidate
}

type clientLoonMember struct {
	name, ref string
	isRef     bool
}
type clientLoonEntry struct {
	group   model.ProxyGroup
	members []clientLoonMember
}

// LoonConfig builds remote sources, filters and policy references in stable order.
func LoonConfig(ctx context.Context, config *model.Config, base string) (string, error) {
	ctx = EnsureConversions(ctx)
	if err := clientPreflight(ctx, config, "Loon"); err != nil {
		return "", err
	}
	custom := splitClientSections(config.Profile.Loon.CustomConfig)
	general, _ := clientSection(custom, "General")
	fallback := FallbackPolicy(config)
	if general != "" {
		if fallback != "" {
			general = SetGeneralOption(general, "udp-fallback-mode", fallback)
		}
	} else {
		lines := append([]string{}, LoonDefaultGeneral...)
		if fallback != "" {
			for i, line := range lines {
				if strings.HasPrefix(line, "udp-fallback-mode") {
					lines[i] = "udp-fallback-mode = " + fallback
				}
			}
		}
		general = strings.Join(lines, "\n")
	}
	all := config.Profile.ProxyGroups
	byID := map[string]model.ProxyGroup{}
	var effective []model.ProxyGroup
	for _, group := range all {
		byID[group.ID] = group
	}
	for _, group := range all {
		if !enabled(&group, group.Enabled) {
			continue
		}
		result, err := effectiveClientGroup(group, config)
		if err != nil {
			return "", err
		}
		if result != nil {
			effective = append(effective, *result)
		}
	}
	used := map[string]bool{}
	for _, group := range effective {
		for _, ref := range clientMemberRefs(group) {
			if ref.kind == "node" {
				used[ref.id] = true
			}
		}
	}
	proxies := []string{"[Proxy]"}
	nodeNames := map[string]string{}
	var orderedNames []string
	for _, node := range config.Nodes {
		if !enabled(&node, node.Enabled) || !used[node.ID] {
			continue
		}
		line, err := LoonNode(node)
		if err != nil {
			return "", err
		}
		if line != "" {
			proxies = append(proxies, line)
			name, _, _ := strings.Cut(line, " = ")
			nodeNames[node.ID] = name
			orderedNames = append(orderedNames, name)
		} else {
			loonLog.Warning(msgLoonUnsupportedNode + node.Name)
		}
	}
	reserved := append([]string{}, orderedNames...)
	for _, group := range all {
		reserved = append(reserved, clientSanitizeName(group.Name))
	}
	allocator := clientNames(reserved)
	type remoteKey struct{ kind, id, exclude string }
	remoteNames := map[remoteKey]string{}
	remoteLines := []string{"[Remote Proxy]"}
	remoteName := func(kind, id, exclude string) (string, error) {
		key := remoteKey{kind, id, exclude}
		if name, ok := remoteNames[key]; ok {
			return name, nil
		}
		preferred, found, err := clientResourceName(config, kind, id)
		if err != nil {
			return "", err
		}
		if !found {
			remoteNames[key] = ""
			return "", nil
		}
		suffix := "订阅"
		if kind != "subscription" {
			suffix = "聚合"
		}
		name := allocator.allocate(preferred, suffix)
		address, err := clientRemoteURL(config, clientBaseURL(config, base), kind, id, "loon", "", exclude)
		if err != nil {
			return "", err
		}
		remoteLines = append(remoteLines, name+" = "+address+",udp=default,block-quic=default,fast-open=default,vmess-aead=default,skip-cert-verify=default,enabled=true")
		remoteNames[key] = name
		return name, nil
	}
	filterLines := []string{"[Remote Filter]"}
	entries := map[string]clientLoonEntry{}
	for _, group := range effective {
		var members []clientLoonMember
		for _, ref := range clientMemberRefs(group) {
			switch ref.kind {
			case "builtin":
				members = append(members, clientLoonMember{name: ref.id})
			case "node":
				if name, ok := nodeNames[ref.id]; ok {
					members = append(members, clientLoonMember{name: name})
				}
			case "strategy":
				if target, ok := byID[ref.id]; ok && ref.id != group.ID {
					if err := storedText(&target, kindGroup, target.ID, "name"); err != nil {
						return "", err
					}
					members = append(members, clientLoonMember{ref: ref.id, isRef: true})
				}
			}
		}
		_, aggregations, _, subscriptions := clientGroupSources(group)
		for _, source := range []struct {
			kind           string
			ids            []string
			regex, exclude *string
		}{{"subscription", subscriptions, group.Regex, group.ExcludeRegex}, {"aggregation", aggregations, group.AggregationRegex, group.AggregationExcludeRegex}} {
			regex, exclude := strings.TrimSpace(stringValue(source.regex)), strings.TrimSpace(stringValue(source.exclude))
			var names []string
			for _, id := range source.ids {
				name, err := remoteName(source.kind, id, exclude)
				if err != nil {
					return "", err
				}
				if name != "" {
					names = append(names, name)
				}
			}
			if len(names) == 0 {
				continue
			}
			if regex != "" {
				suffix := "筛选"
				if source.kind != "subscription" {
					suffix = "聚合筛选"
				}
				name := allocator.allocate(group.Name+"-"+suffix, suffix)
				filterLines = append(filterLines, name+" = NameRegex,"+strings.Join(names, ",")+`,FilterKey="`+strings.ReplaceAll(regex, `"`, `\"`)+`"`)
				members = append(members, clientLoonMember{name: name})
			} else {
				for _, name := range names {
					members = append(members, clientLoonMember{name: name})
				}
			}
		}
		entries[group.ID] = clientLoonEntry{group, members}
	}
	emitted := map[string]bool{}
	for id := range entries {
		emitted[id] = true
	}
	for changed := true; changed; {
		changed = false
		for _, group := range effective {
			if !emitted[group.ID] {
				continue
			}
			usable := false
			for _, member := range entries[group.ID].members {
				if !member.isRef || emitted[member.ref] {
					usable = true
					break
				}
			}
			if !usable {
				delete(emitted, group.ID)
				changed = true
			}
		}
	}
	groups := []string{"[Proxy Group]"}
	for _, group := range effective {
		if !emitted[group.ID] {
			continue
		}
		var names []string
		for _, member := range entries[group.ID].members {
			if member.isRef {
				if emitted[member.ref] {
					names = append(names, clientSanitizeName(byID[member.ref].Name))
				}
			} else {
				names = append(names, member.name)
			}
		}
		line, err := LoonGroup(group, names)
		if err != nil {
			return "", err
		}
		if line != "" {
			groups = append(groups, line)
		}
	}
	if len(groups) == 1 && len(orderedNames) > 0 {
		joined := strings.Join(orderedNames, ",")
		groups = append(groups, "Proxy = select,"+joined, "Auto = url-test,"+joined+",url="+LoonDefaultTestURL+",interval=600")
	}
	rules, remoteRules, err := LoonRules(config, clientBaseURL(config, base))
	if err != nil {
		return "", err
	}
	sections := []string{general, strings.Join(proxies, "\n"), strings.Join(remoteLines, "\n"), strings.Join(filterLines, "\n"), strings.Join(groups, "\n"), strings.Join(rules, "\n"), strings.Join(remoteRules, "\n")}
	for _, section := range custom {
		switch section.name {
		case "General", "Proxy", "Remote Proxy", "Remote Filter", "Proxy Group", "Rule", "Remote Rule":
		default:
			sections = append(sections, section.text)
		}
	}
	if err := CurrentMihomoEnvironment().ValidateRules(config, clientPolicyNames(append(append([]string{}, proxies[1:]...), groups[1:]...)), "loon"); err != nil {
		return "", err
	}
	return "# Generated by ConfigFlow for Loon\n\n" + strings.Join(sections, "\n\n") + "\n", nil
}

type missingClientNodeConverter struct{}

func (missingClientNodeConverter) ConvertProxy(context.Context, string) (*yamlx.Node, error) {
	return nil, &HookMissingError{Hook: "convert_proxy_string", Provider: "convert/mihomoenv (C13 substore)"}
}
