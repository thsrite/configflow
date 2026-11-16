package importer

import (
	"encoding/json"
	"fmt"
	"strings"
)

type remoteFilter struct {
	providers []string
	regex     string
}

func parseProxyLines(lines []string, plan *Plan, parse func(string, string) (*Proxy, error)) {
	for _, line := range lines {
		name, value, err := nameValue(line)
		var proxy *Proxy
		if err == nil {
			proxy, err = parse(name, value)
		}
		if err != nil {
			label, _, _ := strings.Cut(line, "=")
			plan.warnings.add(msgUnparsedNode, strings.TrimSpace(label)+"（"+err.Error()+"）")
			continue
		}
		if proxy != nil {
			plan.Proxies = append(plan.Proxies, *proxy)
		}
	}
}
func parseTextGroup(name, value, client string, plan *Plan, filters map[string]remoteFilter) *Group {
	args := splitArgs(value, ',')
	kind := strings.ToLower(strings.TrimSpace(at(args, 0)))
	if kind == "smart" {
		plan.warnings.add(msgSmart, name)
		kind = "url-test"
	}
	if !groupTypes[kind] {
		plan.warnings.add(msgUnsupportedGroup, name+"("+kind+")")
		return nil
	}
	members, options := splitOptions(args[1:])
	group := &Group{Name: name, Type: kind, Members: []string{}, Providers: []string{}, IncludeAllProxies: enabled(options["include-all-proxies"]), URL: options["url"], Interval: integer(options["interval"]), Extra: map[string]json.RawMessage{}}
	if options["tolerance"] != "" {
		group.Extra["tolerance"] = jsonValue(integer(options["tolerance"]))
	}
	if kind == "load-balance" {
		strategy := map[string]string{"round-robin": "round-robin", "pcc": "consistent-hashing", "random": "round-robin"}[options["algorithm"]]
		if strategy != "" {
			group.Extra["strategy"] = jsonValue(strategy)
		}
	}
	if options["include-other-group"] != "" {
		members = append(members, nonemptySplit(options["include-other-group"], ",")...)
	}
	if options["policy-path"] != "" {
		provider := fmt.Sprintf(msgGroupProvider, name)
		plan.Providers = append(plan.Providers, Provider{provider, options["policy-path"]})
		group.Providers = append(group.Providers, provider)
	}
	regexes := []string{}
	if options["policy-regex-filter"] != "" {
		regexes = append(regexes, options["policy-regex-filter"])
		if client == "shadowrocket" && options["policy-path"] == "" {
			group.IncludeAllProxies = true
			group.IncludeAllProviders = true
		}
	}
	for _, member := range members {
		if filter, exists := filters[member]; exists {
			group.Providers = append(group.Providers, filter.providers...)
			if len(filter.providers) == 0 {
				group.IncludeAllProviders = true
			}
			if filter.regex != "" {
				regexes = append(regexes, filter.regex)
			}
		} else {
			group.Members = append(group.Members, member)
		}
	}
	if len(regexes) == 1 {
		group.Filter = regexes[0]
	} else if len(regexes) > 1 {
		for i := range regexes {
			regexes[i] = "(" + regexes[i] + ")"
		}
		group.Filter = strings.Join(regexes, "|")
	}
	return group
}
func parseSurge(content, client string, plan *Plan) error {
	sections := parseSections(content)
	if len(sections) == 0 {
		return fmt.Errorf(msgNoSections)
	}
	parseProxyLines(sections.content("Proxy"), plan, func(name, value string) (*Proxy, error) { return parseSurgeProxy(name, value, sections, plan) })
	for _, line := range sections.content("Proxy Group") {
		name, value, err := nameValue(line)
		if err != nil {
			plan.warnings.add(msgInvalidGroup, line)
			continue
		}
		if group := parseTextGroup(name, value, client, plan, nil); group != nil {
			plan.Groups = append(plan.Groups, *group)
		}
	}
	for _, line := range sections.content("Rule") {
		parts := splitArgs(line, ',')
		head := strings.ToUpper(strings.TrimSpace(at(parts, 0)))
		if (head == "RULE-SET" || head == "DOMAIN-SET") && len(parts) >= 3 {
			source := unquote(parts[1])
			if !httpURL(source) {
				plan.warnings.add(msgLocalRules, source)
				continue
			}
			behavior := "classical"
			if head == "DOMAIN-SET" {
				behavior = "domain"
			}
			name := registerRuleset(plan, source, behavior, "")
			line = strings.Join(append([]string{"RULE-SET", name}, parts[2:]...), ",")
		}
		if rule := parseRule(line, &plan.warnings); rule != nil {
			plan.Rules = append(plan.Rules, *rule)
		}
	}
	generated := func(name string) bool { return name == "Proxy" || name == "Proxy Group" || name == "Rule" }
	if client == "surge" {
		text := customSections(sections, func(name string) bool { return generated(name) || strings.HasPrefix(name, "WireGuard ") })
		if strings.TrimSpace(text) != "" {
			plan.Custom = &CustomConfig{"surge", text}
		}
	} else {
		for _, section := range sections {
			if !generated(section.name) {
				plan.warnings.add(msgShadowrocket, "")
				break
			}
		}
	}
	return nil
}
func httpURL(value string) bool {
	value = strings.ToLower(value)
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}
func parseLoon(content string, plan *Plan) error {
	sections := parseSections(content)
	if len(sections) == 0 {
		return fmt.Errorf(msgNoSections)
	}
	parseProxyLines(sections.content("Proxy"), plan, func(name, value string) (*Proxy, error) { return parseLoonProxy(name, value, plan) })
	for _, line := range sections.content("Remote Proxy") {
		name, value, err := nameValue(line)
		if err != nil {
			continue
		}
		pos, options := splitOptions(splitArgs(value, ','))
		if len(pos) == 0 || strings.ToLower(option(options, "enabled", "true")) == "false" {
			continue
		}
		plan.Providers = append(plan.Providers, Provider{name, pos[0]})
	}
	providerNames := map[string]bool{}
	for _, provider := range plan.Providers {
		providerNames[provider.Name] = true
	}
	filters := map[string]remoteFilter{}
	for _, line := range sections.content("Remote Filter") {
		name, value, err := nameValue(line)
		if err != nil {
			continue
		}
		pos, options := splitOptions(splitArgs(value, ','))
		kind := at(pos, 0)
		filter := remoteFilter{providers: []string{}}
		for _, member := range pos[min(1, len(pos)):] {
			if providerNames[member] {
				filter.providers = append(filter.providers, member)
			}
		}
		if strings.EqualFold(kind, "NameRegex") {
			filter.regex = options["filterkey"]
		} else {
			plan.warnings.add(msgNameRegex, name)
		}
		filters[name] = filter
	}
	for _, line := range sections.content("Proxy Group") {
		name, value, err := nameValue(line)
		if err != nil {
			plan.warnings.add(msgInvalidGroup, line)
			continue
		}
		group := parseTextGroup(name, value, "loon", plan, filters)
		if group == nil {
			continue
		}
		members := []string{}
		for _, member := range group.Members {
			if providerNames[member] {
				group.Providers = append(group.Providers, member)
			} else {
				members = append(members, member)
			}
		}
		group.Members = members
		plan.Groups = append(plan.Groups, *group)
	}
	var final *Rule
	for _, line := range sections.content("Rule") {
		if rule := parseRule(line, &plan.warnings); rule != nil {
			if rule.Type == "MATCH" {
				final = rule
			} else {
				plan.Rules = append(plan.Rules, *rule)
			}
		}
	}
	for _, line := range sections.content("Remote Rule") {
		pos, options := splitOptions(splitArgs(line, ','))
		if len(pos) == 0 || strings.EqualFold(option(options, "enabled", "true"), "false") {
			continue
		}
		source := pos[0]
		if !httpURL(source) {
			plan.warnings.add(msgLocalLoonRule, source)
			continue
		}
		if options["policy"] == "" {
			plan.warnings.add(msgMissingPolicy, source)
			continue
		}
		name := registerRuleset(plan, source, "classical", options["tag"])
		plan.Rules = append(plan.Rules, Rule{Type: "RULE-SET", Value: name, Policy: options["policy"]})
	}
	if final != nil {
		plan.Rules = append(plan.Rules, *final)
	}
	text := customSections(sections, func(name string) bool {
		switch name {
		case "Proxy", "Remote Proxy", "Remote Filter", "Proxy Group", "Rule", "Remote Rule":
			return true
		}
		return false
	})
	if strings.TrimSpace(text) != "" {
		plan.Custom = &CustomConfig{"loon", text}
	}
	return nil
}
