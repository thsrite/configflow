package rulesync

import (
	"configflow/internal/deploy"
	"configflow/internal/oxidns"
	"configflow/internal/yamlx"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"slices"
)

// Kind distinguishes no effective change, content-only refresh and rule structure publication.
type Kind string

// Rule change categories.
const (
	None      Kind = "none"
	Content   Kind = "content"
	Structure Kind = "structure"
)

func active(item *yaml.Node) bool {
	for _, key := range []string{"enabled", "library_enabled"} {
		n := get(item, key)
		if n != nil && n.ShortTag() == "!!bool" && n.Value == "false" {
			return false
		}
	}
	return true
}
func sourceRuleState(doc *yaml.Node, content bool) *yaml.Node {
	out := yamlx.Seq()
	for _, r := range list(get(doc, "rule_configs")) {
		if !active(r) {
			continue
		}
		fields := []string{}
		switch scope(r) {
		case "library":
			fields = []string{"id", "library_rule_id", "name", "url", "behavior", "source_type", "format", "policy", "no_resolve"}
			if content {
				fields = append(fields, "content")
			}
		case "rules":
			fields = []string{"rule_type", "value", "policy", "no_resolve", "udp_fallback", "ntp_direct"}
		default:
			continue
		}
		entry := obj()
		for _, key := range fields {
			if n := get(r, key); n != nil {
				yamlx.Set(entry, key, n)
			}
		}
		out.Content = append(out.Content, entry)
	}
	return out
}
func ruleState(source *deploy.Source, service string) (*yaml.Node, error) {
	doc, err := sourceDocument(source)
	if err != nil {
		return nil, err
	}
	if err = hydrate(doc); err != nil {
		return nil, err
	}
	if service == "mihomo" {
		return sourceRuleState(doc, true), nil
	}
	if service != "oxidns" {
		return nil, refuse(msgProjectionService)
	}
	p, err := profile(doc)
	if err != nil {
		return nil, err
	}
	content, err := oxidns.Generate(p, "")
	if err != nil {
		return nil, err
	}
	generated, err := yamlx.Parse([]byte(content))
	if err != nil {
		return nil, err
	}
	refs, err := oxidns.ResourceReferences(generated)
	if err != nil {
		return nil, err
	}
	referenced := map[string]bool{}
	for _, path := range refs {
		referenced[relativeResource(path)] = true
	}
	sources := yamlx.Seq()
	for _, r := range list(get(doc, "rule_configs")) {
		if scope(r) != "library" || !active(r) {
			continue
		}
		paths := []string{}
		behavior := text(get(r, "behavior"))
		if behavior == "" {
			behavior = "domain"
		}
		for _, part := range oxidns.RulesetParts(behavior) {
			path := relativeResource(oxidns.RulePath(r, part))
			if referenced[path] {
				paths = append(paths, path)
			}
		}
		if len(paths) == 0 {
			continue
		}
		entry := yamlx.Map(yamlx.Str("paths"), stringsNode(paths))
		for _, key := range []string{"source_type", "url", "behavior"} {
			if v := get(r, key); v != nil {
				yamlx.Set(entry, key, v)
			}
		}
		if text(get(r, "source_type")) == "content" {
			v := get(r, "content")
			if v == nil {
				v = yamlx.Str("")
			}
			yamlx.Set(entry, "content", v)
		}
		sources.Content = append(sources.Content, entry)
	}
	return yamlx.Map(yamlx.Str("document"), generated, yamlx.Str("sources"), sources), nil
}
func nodeDigest(n *yaml.Node) (string, error) {
	raw, err := yamlx.ToJSON(n)
	if err != nil {
		return "", err
	}
	var value json.RawMessage = raw
	return deploy.CanonicalDigest(value), nil
}

// RuleSignatures compares effective consumers; unused library metadata has no effect.
func RuleSignatures(source *deploy.Source, service string) (withContent, bare string, err error) {
	state, err := ruleState(source, service)
	if err != nil {
		return "", "", err
	}
	withContent, err = nodeDigest(state)
	if err != nil {
		return "", "", err
	}
	items := state
	if service == "oxidns" {
		items = get(state, "sources")
	}
	for _, item := range list(items) {
		yamlx.Delete(item, "content")
	}
	bare, err = nodeDigest(state)
	return
}

// RuleChanges projects once and classifies effective generated changes.
func RuleChanges(base, current *deploy.Source, service string, s Settings) (Kind, *deploy.Source, []string, error) {
	withheld := map[string]bool{}
	projected, err := ProjectRuleChanges(base, current, s, service, withheld)
	if err != nil {
		return "", nil, nil, err
	}
	before, oldBare, err := RuleSignatures(base, service)
	if err != nil {
		return "", nil, nil, err
	}
	after, newBare, err := RuleSignatures(projected, service)
	if err != nil {
		return "", nil, nil, err
	}
	kind := Structure
	if before == after {
		kind = None
	} else if oldBare == newBare {
		kind = Content
	}
	ids := make([]string, 0, len(withheld))
	for id := range withheld {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return kind, projected, ids, nil
}

func relativeResource(path string) string {
	value, ok, err := oxidns.RelativeResource(yamlx.Str(path))
	if err != nil || !ok {
		return ""
	}
	return value
}
