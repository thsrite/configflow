package importer

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"
)

var builtinPolicies = map[string]string{"DIRECT": "DIRECT", "REJECT": "REJECT", "REJECT-DROP": "REJECT", "REJECT-TINYGIF": "REJECT", "REJECT-NO-DROP": "REJECT", "REJECT-IMG": "REJECT", "REJECT-DICT": "REJECT", "REJECT-ARRAY": "REJECT", "REJECT-200": "REJECT"}
var groupTypes = map[string]bool{"select": true, "url-test": true, "fallback": true, "load-balance": true}
var supportedRules = map[string]bool{}

func init() {
	for _, name := range strings.Fields("DOMAIN DOMAIN-SUFFIX DOMAIN-KEYWORD DOMAIN-REGEX IP-CIDR IP-CIDR6 IP-SUFFIX IP-ASN SRC-IP-CIDR DST-PORT SRC-PORT IN-PORT GEOIP GEOSITE PROCESS-NAME PROCESS-PATH NETWORK RULE-SET AND OR NOT MATCH") {
		supportedRules[name] = true
	}
}

var ruleAliases = map[string]string{"FINAL": "MATCH", "DEST-PORT": "DST-PORT", "SRC-IP": "SRC-IP-CIDR", "IP6-CIDR": "IP-CIDR6"}
var ruleOptions = map[string]bool{"no-resolve": true, "extended-matching": true, "dns-failed": true, "pre-matching": true, "src": true}
var generatedKeys = map[string]bool{"proxies": true, "proxy-groups": true, "proxy-providers": true, "rules": true, "rule-providers": true, "sub-rules": true}
var groupPassthrough = strings.Fields("tolerance lazy strategy timeout max-failed-times expected-status disable-udp hidden icon exclude-filter exclude-type")

// Parse decodes a supported client file into an independent import plan.
func Parse(clientType, content string) (*Plan, error) {
	switch clientType {
	case "mihomo", "surge", "loon", "shadowrocket":
	default:
		return nil, fmt.Errorf(msgUnsupportedClient, clientType)
	}
	if strings.TrimSpace(content) == "" {
		return nil, fmt.Errorf(msgEmpty)
	}
	content = strings.TrimLeft(content, "\ufeff")
	plan := &Plan{Aliases: map[string]string{}}
	var err error
	switch clientType {
	case "mihomo":
		err = parseMihomo(content, plan)
	case "loon":
		err = parseLoon(content, plan)
	default:
		err = parseSurge(content, clientType, plan)
	}
	if err != nil {
		return nil, err
	}
	if len(plan.Proxies)+len(plan.Providers)+len(plan.Groups)+len(plan.Rules) == 0 {
		return nil, fmt.Errorf(msgNoItems)
	}
	return plan, nil
}
func parseRule(line string, w *warnings) *Rule {
	parts := splitArgs(line, ',')
	if len(parts) == 0 || parts[0] == "" {
		return nil
	}
	kind := strings.ToUpper(strings.TrimSpace(parts[0]))
	if alias := ruleAliases[kind]; alias != "" {
		kind = alias
	}
	invalid := func() *Rule { w.add(msgInvalidRule, line); return nil }
	if kind == "MATCH" {
		if len(parts) < 2 {
			return invalid()
		}
		return &Rule{Type: kind, Policy: unquote(parts[1])}
	}
	if len(parts) < 3 {
		return invalid()
	}
	tail := append([]string{}, parts[2:]...)
	noResolve := false
	for len(tail) > 1 {
		last := strings.ToLower(tail[len(tail)-1])
		if !ruleOptions[last] && !strings.Contains(last, "=") {
			break
		}
		noResolve = noResolve || last == "no-resolve"
		tail = tail[:len(tail)-1]
	}
	if len(tail) != 1 {
		return invalid()
	}
	if !supportedRules[kind] {
		w.add(msgUnsupportedRule, kind)
		return nil
	}
	return &Rule{Type: kind, Value: strings.TrimSpace(parts[1]), Policy: unquote(tail[0]), NoResolve: noResolve}
}
func field(node *yaml.Node, key string) *yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		return field(node.Alias, key)
	}
	if node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
func scalar(node *yaml.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind == yaml.AliasNode {
		return scalar(node.Alias)
	}
	if node.Kind == yaml.ScalarNode && node.Tag != "!!null" {
		return node.Value
	}
	return ""
}
func sequence(node *yaml.Node) []*yaml.Node {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.AliasNode {
		return sequence(node.Alias)
	}
	if node.Kind == yaml.SequenceNode {
		return node.Content
	}
	return nil
}
func nodeBool(node *yaml.Node) bool {
	if node == nil {
		return false
	}
	var out bool
	_ = node.Decode(&out)
	return out
}
func nodeStrings(node *yaml.Node) []string {
	out := []string{}
	for _, value := range sequence(node) {
		out = append(out, scalar(value))
	}
	return out
}
func nodeJSON(node *yaml.Node) (json.RawMessage, error) {
	var value any
	if node != nil {
		if err := node.Decode(&value); err != nil {
			return nil, err
		}
	}
	return json.Marshal(value)
}
func jsonValue(value any) json.RawMessage { data, _ := json.Marshal(value); return data }
func (p *Proxy) set(key string, value any) {
	if p.Options == nil {
		p.Options = map[string]json.RawMessage{}
	}
	p.Options[key] = jsonValue(value)
}
func parseMihomo(content string, plan *Plan) error {
	var doc yaml.Node
	decoder := yaml.NewDecoder(strings.NewReader(content))
	if err := decoder.Decode(&doc); err != nil && err != io.EOF {
		return fmt.Errorf(msgYAML, err)
	}
	var trailing yaml.Node
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			err = errors.New("multiple YAML documents are not supported")
		}
		return fmt.Errorf(msgYAML, err)
	}
	if len(doc.Content) == 0 || doc.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf(msgMihomoObject)
	}
	root := doc.Content[0]
	// yaml.v3 validates duplicate keys and alias cycles consistently before any
	// generated section is removed from the retained custom YAML.
	var checked any
	if err := root.Decode(&checked); err != nil {
		return fmt.Errorf(msgYAML, err)
	}
	for _, node := range sequence(field(root, "proxies")) {
		name, kind := scalar(field(node, "name")), scalar(field(node, "type"))
		if node.Kind != yaml.MappingNode || name == "" || kind == "" {
			plan.warnings.add(msgInvalidNode, name)
			continue
		}
		lowered := strings.ToLower(kind)
		if lowered == "direct" || lowered == "reject" || lowered == "dns" {
			policy := "DIRECT"
			if lowered == "reject" {
				policy = "REJECT"
			}
			plan.Aliases[name] = policy
			continue
		}
		proxy := Proxy{Name: name, Type: kind, Options: map[string]json.RawMessage{}}
		proxy.Server = scalar(field(node, "server"))
		proxy.Port = jsonValue(0)
		if port := field(node, "port"); port != nil {
			raw, err := nodeJSON(port)
			if err != nil {
				return err
			}
			proxy.Port = raw
		}
		for i := 0; i+1 < len(node.Content); i += 2 {
			key := node.Content[i].Value
			if key == "name" || key == "type" || key == "server" || key == "port" {
				continue
			}
			raw, err := nodeJSON(node.Content[i+1])
			if err != nil {
				return err
			}
			proxy.Options[key] = raw
		}
		plan.Proxies = append(plan.Proxies, proxy)
	}
	if providers := field(root, "proxy-providers"); providers != nil && providers.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(providers.Content); i += 2 {
			name, node := providers.Content[i].Value, providers.Content[i+1]
			if node.Kind != yaml.MappingNode || scalar(field(node, "type")) != "http" || scalar(field(node, "url")) == "" {
				plan.warnings.add(msgHTTPProviders, name)
				continue
			}
			plan.Providers = append(plan.Providers, Provider{name, scalar(field(node, "url"))})
		}
	}
	if providers := field(root, "rule-providers"); providers != nil && providers.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(providers.Content); i += 2 {
			name, node := providers.Content[i].Value, providers.Content[i+1]
			if node.Kind != yaml.MappingNode {
				continue
			}
			behavior := first(scalar(field(node, "behavior")), "classical")
			if scalar(field(node, "format")) == "mrs" {
				plan.warnings.add(msgMRS, name)
				continue
			}
			if scalar(field(node, "type")) == "http" && scalar(field(node, "url")) != "" {
				plan.RuleProviders = append(plan.RuleProviders, RuleProvider{Name: name, URL: scalar(field(node, "url")), Behavior: behavior})
			} else if scalar(field(node, "type")) == "inline" && field(node, "payload") != nil && field(node, "payload").Kind == yaml.SequenceNode {
				text := strings.Join(nodeStrings(field(node, "payload")), "\n")
				plan.RuleProviders = append(plan.RuleProviders, RuleProvider{Name: name, Content: &text, Behavior: behavior})
			} else {
				plan.warnings.add(msgRuleProviders, name)
			}
		}
	}
	for _, node := range sequence(field(root, "proxy-groups")) {
		name := scalar(field(node, "name"))
		if node.Kind != yaml.MappingNode || name == "" {
			continue
		}
		kind := strings.ToLower(scalar(field(node, "type")))
		if !groupTypes[kind] {
			plan.warnings.add(msgUnsupportedGroup, name+"("+kind+")")
			continue
		}
		all := nodeBool(field(node, "include-all"))
		group := Group{Name: name, Type: kind, Members: nodeStrings(field(node, "proxies")), Providers: nodeStrings(field(node, "use")), Filter: scalar(field(node, "filter")), IncludeAllProxies: all || nodeBool(field(node, "include-all-proxies")), IncludeAllProviders: all || nodeBool(field(node, "include-all-providers")), URL: scalar(field(node, "url")), Interval: integer(scalar(field(node, "interval"))), Extra: map[string]json.RawMessage{}}
		for _, key := range groupPassthrough {
			if value := field(node, key); value != nil {
				raw, err := nodeJSON(value)
				if err != nil {
					return err
				}
				group.Extra[key] = raw
			}
		}
		plan.Groups = append(plan.Groups, group)
	}
	for _, node := range sequence(field(root, "rules")) {
		if node.Kind != yaml.ScalarNode || node.Tag != "!!str" {
			continue
		}
		line := node.Value
		if strings.HasPrefix(strings.ToUpper(strings.TrimSpace(line)), "SUB-RULE") {
			plan.warnings.add(msgSubRule, strings.TrimSpace(line))
			continue
		}
		if rule := parseRule(line, &plan.warnings); rule != nil {
			plan.Rules = append(plan.Rules, *rule)
		}
	}
	if value := field(root, "sub-rules"); value != nil && len(value.Content) > 0 {
		plan.warnings.add(msgSubRules, "")
	}
	custom := *root
	custom.Content = nil
	for i := 0; i+1 < len(root.Content); i += 2 {
		if !generatedKeys[root.Content[i].Value] {
			custom.Content = append(custom.Content, root.Content[i], root.Content[i+1])
		}
	}
	if len(custom.Content) > 0 {
		data, err := yaml.Marshal(&custom)
		if err != nil {
			return err
		}
		plan.Custom = &CustomConfig{"mihomo", string(data)}
	}
	return nil
}
func registerRuleset(plan *Plan, source, behavior, name string) string {
	for _, provider := range plan.RuleProviders {
		if provider.URL == source {
			return provider.Name
		}
	}
	base := first(name, rulesetName(source))
	taken := map[string]bool{}
	for _, provider := range plan.RuleProviders {
		taken[provider.Name] = true
	}
	name = uniqueName(base, taken, true)
	plan.RuleProviders = append(plan.RuleProviders, RuleProvider{Name: name, URL: source, Behavior: behavior})
	return name
}
