package rulesync

import (
	"encoding/json"
	"slices"
	"strings"

	"configflow/internal/deploy"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"gopkg.in/yaml.v3"
)

var selectors = []string{"direct_rulesets", "proxy_rulesets", "direct_rules", "proxy_rules"}
var dnsFields = []string{"rule_actions", "rule_dns_parked", "rule_dns_follow"}

func obj() *yaml.Node                       { return yamlx.Map() }
func get(n *yaml.Node, k string) *yaml.Node { return yamlx.Get(n, k) }
func text(n *yaml.Node) string {
	n = yamlx.Resolve(n)
	if n != nil && n.Kind == yaml.ScalarNode && n.ShortTag() == "!!str" {
		return n.Value
	}
	return ""
}
func list(n *yaml.Node) []*yaml.Node {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.SequenceNode {
		return nil
	}
	return n.Content
}
func keys(n *yaml.Node) []string {
	n = yamlx.Resolve(n)
	out := []string{}
	if n != nil && n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, text(n.Content[i]))
		}
	}
	return out
}
func stringsNode(values []string) *yaml.Node {
	n := yamlx.Seq()
	for _, v := range values {
		n.Content = append(n.Content, yamlx.Str(v))
	}
	return n
}
func sourceDocument(s *deploy.Source) (*yaml.Node, error) {
	raw, err := s.Raw()
	if err != nil {
		return nil, err
	}
	return oxidns.FromJSON(raw)
}
func selected(s Settings, scope string) bool {
	return scope == "library" && s.Library || scope == "rules" && s.Rules
}
func scope(item *yaml.Node) string {
	switch text(get(item, "itemType")) {
	case "ruleset":
		return "library"
	case "rule":
		return "rules"
	}
	return ""
}
func selectorScope(k string) string {
	if strings.HasSuffix(k, "rulesets") {
		return "library"
	}
	return "rules"
}
func scopes(configs ...*yaml.Node) map[string]string {
	out := map[string]string{}
	for _, c := range configs {
		for _, r := range list(get(c, "rule_configs")) {
			id := text(get(r, "id"))
			if _, ok := out[id]; !ok && id != "" {
				out[id] = scope(r)
			}
		}
	}
	return out
}
func pin(action *yaml.Node) string {
	a, err := oxidns.NormalizeAction(action, true)
	if err != nil || text(get(a, "action")) != "upstream" {
		return ""
	}
	return oxidns.EntryKey(get(a, "upstream"), text(get(a, "source")))
}

// PublishedUpstreams returns resolvable pinned upstream identities from the deployed source.
func PublishedUpstreams(source *deploy.Source) map[string]bool {
	out := map[string]bool{}
	doc, err := sourceDocument(source)
	if err != nil {
		return out
	}
	settings := get(doc, "oxidns")
	catalog, err := oxidns.UpstreamCatalog(settings, nil, nil)
	if err == nil {
		for _, entry := range catalog {
			out[entry.Key] = true
		}
	}
	for _, id := range keys(get(settings, "rule_actions")) {
		if k := pin(get(get(settings, "rule_actions"), id)); k != "" {
			out[k] = true
		}
	}
	return out
}
func mergeItems(previous, desired []*yaml.Node, s Settings) []*yaml.Node {
	replacements := []*yaml.Node{}
	for _, r := range desired {
		if selected(s, scope(r)) {
			replacements = append(replacements, r)
		}
	}
	if s.Library && s.Rules {
		for _, r := range previous {
			if scope(r) == "" {
				replacements = append(replacements, r)
			}
		}
		return replacements
	}
	last := -1
	for i, r := range previous {
		if selected(s, scope(r)) {
			last = i
		}
	}
	if last < 0 {
		return append(replacements, previous...)
	}
	out := []*yaml.Node{}
	offset := 0
	for i, r := range previous {
		if selected(s, scope(r)) {
			if offset < len(replacements) {
				out = append(out, replacements[offset])
				offset++
			}
			if i == last {
				out = append(out, replacements[offset:]...)
			}
		} else {
			out = append(out, r)
		}
	}
	return out
}
func hydrate(doc *yaml.Node) error {
	library := map[string]*yaml.Node{}
	for _, r := range list(get(doc, "rule_library")) {
		library[text(get(r, "id"))] = r
	}
	items := list(get(doc, "rule_configs"))
	out := yamlx.Seq()
	for _, r := range items {
		if scope(r) == "library" && text(get(r, "library_rule_id")) != "" {
			source := library[text(get(r, "library_rule_id"))]
			if source == nil {
				return refuse(msgMissingLibrary)
			}
			r = oxidns.HydrateRuleset(r, source, text(get(doc, "profile_id")))
		}
		out.Content = append(out.Content, r)
	}
	yamlx.Set(doc, "rule_configs", out)
	return nil
}
func dnsProjection(previous, desired *yaml.Node, enabled func(string) bool, published, withheld map[string]bool) *yaml.Node {
	held := map[string]bool{}
	for _, field := range []string{"rule_actions", "rule_dns_parked"} {
		old := get(previous, field)
		next := get(desired, field)
		for _, id := range keys(next) {
			action := get(next, id)
			if enabled(id) && !yamlx.Equal(action, get(old, id)) {
				key := pin(action)
				if key != "" && !published[key] {
					held[id] = true
					withheld[id] = true
				}
			}
		}
	}
	take := func(id string) bool { return enabled(id) && !held[id] }
	out := obj()
	for _, field := range selectors {
		if get(previous, field) == nil && get(desired, field) == nil {
			continue
		}
		ids := []string{}
		for _, id := range list(get(desired, field)) {
			if !held[text(id)] {
				ids = append(ids, text(id))
			}
		}
		for _, id := range list(get(previous, field)) {
			v := text(id)
			if held[v] && !slices.Contains(ids, v) {
				ids = append(ids, v)
			}
		}
		yamlx.Set(out, field, stringsNode(ids))
	}
	for _, field := range []string{"rule_actions", "rule_dns_parked"} {
		old, next := get(previous, field), get(desired, field)
		if old == nil && next == nil {
			continue
		}
		merged := obj()
		for _, id := range keys(old) {
			if !take(id) {
				yamlx.Set(merged, id, get(old, id))
			}
		}
		for _, id := range keys(next) {
			if take(id) {
				yamlx.Set(merged, id, get(next, id))
			}
		}
		yamlx.Set(out, field, merged)
	}
	if get(previous, "rule_dns_follow") != nil || get(desired, "rule_dns_follow") != nil {
		ids := []string{}
		for _, id := range list(get(previous, "rule_dns_follow")) {
			v := text(id)
			if !take(v) {
				ids = append(ids, v)
			}
		}
		for _, id := range list(get(desired, "rule_dns_follow")) {
			v := text(id)
			if take(v) && !slices.Contains(ids, v) {
				ids = append(ids, v)
			}
		}
		yamlx.Set(out, "rule_dns_follow", stringsNode(ids))
	}
	return out
}
func pruneDNS(settings *yaml.Node, known map[string]string) {
	for _, field := range dnsFields {
		value := get(settings, field)
		if value == nil {
			continue
		}
		if field == "rule_dns_follow" {
			ids := []string{}
			for _, id := range list(value) {
				v := text(id)
				_, ok := known[v]
				if ok && get(get(settings, "rule_actions"), v) == nil && get(get(settings, "rule_dns_parked"), v) == nil {
					ids = append(ids, v)
				}
			}
			yamlx.Set(settings, field, stringsNode(ids))
		} else {
			out := obj()
			for _, id := range keys(value) {
				if _, ok := known[id]; ok {
					yamlx.Set(out, id, get(value, id))
				}
			}
			yamlx.Set(settings, field, out)
		}
	}
}

// ProjectRuleChanges advances opted-in scopes while retaining the deployed source's
// other members. Newly pinned unavailable upstreams keep all previous choices.
func ProjectRuleChanges(base, current *deploy.Source, s Settings, service string, withheld map[string]bool) (*deploy.Source, error) {
	if !s.Any() {
		return base, nil
	}
	if withheld == nil {
		withheld = map[string]bool{}
	}
	before, err := sourceDocument(base)
	if err != nil {
		return nil, err
	}
	next, err := sourceDocument(current)
	if err != nil {
		return nil, err
	}
	result, err := sourceDocument(base)
	if err != nil {
		return nil, err
	}
	if s.Library {
		library := get(next, "rule_library")
		if library == nil {
			library = yamlx.Seq()
		}
		yamlx.Set(result, "rule_library", library)
	}
	yamlx.Set(result, "rule_configs", yamlx.Seq(mergeItems(list(get(before, "rule_configs")), list(get(next, "rule_configs")), s)...))
	if err = hydrate(result); err != nil {
		return nil, err
	}
	published := PublishedUpstreams(base)
	known := scopes(result, next, before)
	values := dnsProjection(get(before, "oxidns"), get(next, "oxidns"), func(id string) bool { return selected(s, known[id]) }, published, withheld)
	settings := get(result, "oxidns")
	if settings == nil {
		settings = obj()
		yamlx.Set(result, "oxidns", settings)
	}
	for _, field := range selectors {
		if selected(s, selectorScope(field)) {
			v := get(values, field)
			if v == nil {
				v = yamlx.Seq()
			}
			yamlx.Set(settings, field, v)
		}
	}
	for _, field := range dnsFields {
		if v := get(values, field); v != nil {
			yamlx.Set(settings, field, v)
		}
	}
	pruneDNS(settings, scopes(result))
	if service != "mihomo" && text(get(get(before, "oxidns"), "config_mode")) == "native" {
		if err = nativeProjection(before, next, result, s, published, withheld); err != nil {
			return nil, err
		}
	}
	out := base
	for _, field := range []string{"rule_library", "rule_configs", "oxidns"} {
		if field == "rule_library" && !s.Library {
			continue
		}
		raw, e := yamlx.ToJSON(get(result, field))
		if e != nil {
			return nil, e
		}
		out, e = out.WithMember(field, json.RawMessage(raw))
		if e != nil {
			return nil, e
		}
	}
	return out, nil
}
