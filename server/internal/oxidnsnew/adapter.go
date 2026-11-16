package oxidns

import (
	"context"
	"errors"
	"strings"
	"sync"

	"configflow/internal/jsonx"
	"configflow/internal/mosdns"
	"configflow/internal/rules"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// The boundary to packages that still speak jsonx: the profile configuration
// from repo, the shared MosDNS/rule helpers, PyYAML-compatible loading of the
// template text fields, and the routers' classical_ip_lines hook. Nothing
// else in this package touches jsonx.

// Profile is a profile configuration (rule_configs, system_config, oxidns,
// proxy_groups, agents, device_notes, ...).
type Profile struct {
	doc *Node
	raw *jsonx.Object
}

// NewProfile wraps a repository configuration document.
func NewProfile(raw *jsonx.Object) *Profile {
	if raw == nil {
		raw = jsonx.NewObject()
	}
	return &Profile{doc: fromJSONX(raw), raw: raw}
}

// ProfileFromJSON reads a configuration from JSON.
func ProfileFromJSON(data []byte) (*Profile, error) {
	v, err := jsonx.Parse(data)
	if err != nil {
		return nil, err
	}
	obj, ok := v.(*jsonx.Object)
	if !ok {
		return nil, errors.New("oxidns: profile configuration must be an object")
	}
	return NewProfile(obj), nil
}

func (p *Profile) get(key string) *Node { return get(p.doc, key) }

// Settings is config['oxidns'] (nil when absent).
func (p *Profile) Settings() *Node { return p.get("oxidns") }

// ToJSONX converts a node for jsonx consumers (repo).
func ToJSONX(n *Node) any {
	switch {
	case n == nil || isTag(n, tagNull):
		return nil
	case n.Kind == yaml.MappingNode:
		obj := jsonx.NewObject()
		pairs(n, func(k string, v *Node) bool { obj.Set(k, ToJSONX(v)); return true })
		return obj
	case n.Kind == yaml.SequenceNode:
		out := make([]any, len(n.Content))
		for i, c := range n.Content {
			out[i] = ToJSONX(c)
		}
		return out
	case n.Tag == tagBool:
		return n.Value == "true"
	case n.Tag == tagInt, n.Tag == tagFloat:
		return jsonx.Number(n.Value)
	}
	return n.Value
}

var errUnsupportedYAML = fail("OxiDNS YAML 只支持普通对象、数组和标量")

func fromJSONX(v any) *Node {
	n, err := convertJSONX(v)
	if err != nil {
		return Null()
	}
	return n
}

// convertJSONX converts jsonx/yamlx values; values a document cannot hold
// (non-string keys, sets, timestamps, bytes) are errUnsupportedYAML.
func convertJSONX(v any) (*Node, error) {
	switch x := v.(type) {
	case nil:
		return Null(), nil
	case *jsonx.Object:
		m := M()
		for key, value := range x.All() {
			n, err := convertJSONX(value)
			if err != nil {
				return nil, err
			}
			m.Content = append(m.Content, S(key), n)
		}
		return m, nil
	case []any:
		l := L()
		for _, value := range x {
			n, err := convertJSONX(value)
			if err != nil {
				return nil, err
			}
			l.Content = append(l.Content, n)
		}
		return l, nil
	case string:
		return S(x), nil
	case bool:
		return B(x), nil
	case jsonx.Number:
		if strings.ContainsAny(string(x), ".eEna") {
			f, _ := jsonx.AsFloat(x)
			return Float(f), nil
		}
		return &Node{Kind: yaml.ScalarNode, Tag: tagInt, Value: string(x)}, nil
	}
	return nil, errUnsupportedYAML
}

// safeLoad is yaml.safe_load (PyYAML rules) of a template text field; ok
// false on a YAML syntax error.
func safeLoad(text string) (*Node, bool, error) {
	v, err := yamlx.SafeLoad(text)
	if err != nil {
		return nil, false, nil
	}
	n, err := convertJSONX(v)
	return n, true, err
}

// splitRules is split_rules_and_rulesets: legacy rules and rule sets.
func (p *Profile) splitRules() (*Node, *Node) {
	ruleList, sets, err := mosdns.SplitRulesAndRulesets(p.raw)
	if err != nil {
		return L(), L()
	}
	return fromJSONX(ruleList), fromJSONX(sets)
}

// ruleProxyURL is the MosDNS rule-proxy URL of a ruleset part.
func (p *Profile) ruleProxyURL(effective, url, part string) (string, error) {
	return mosdns.BuildRuleProxyURL(p.raw, effective, url, part, nil)
}

// rulesetParts is _ruleset_parts(behavior).
func rulesetParts(behavior *Node) []string {
	if !isStr(behavior) {
		return mosdns.RulesetParts(nil)
	}
	return mosdns.RulesetParts(behavior.Value)
}

func convertRule(text, part string) (string, error) {
	return mosdns.ConvertRuleContentForMosDNS(text, part)
}

// parseUpstreams is parse_dns_upstreams(text).
func parseUpstreams(text string) (*Node, error) {
	list, err := mosdns.ParseDNSUpstreams(text)
	if err != nil {
		return nil, err
	}
	return convertJSONX(list)
}

// parseCustomMatches is _parse_custom_matches(list).
func parseCustomMatches(list *Node) (*Node, error) {
	out, err := mosdns.ParseCustomMatches(ToJSONX(list))
	if err != nil {
		return nil, err
	}
	return convertJSONX(out)
}

// renderMembers is group_candidates.render_members(group, config).
func (p *Profile) renderMembers(group *Node) *Node {
	obj, _ := ToJSONX(group).(*jsonx.Object)
	return fromJSONX(rules.RenderMembers(obj, p.raw))
}

// ClassicalIPLinesFunc is rule_ip_presence.classical_ip_lines(item, config):
// true/false for a classical rule set whose content is known, nil otherwise.
type ClassicalIPLinesFunc func(ctx context.Context, item, config any) (any, error)

var classicalHook struct {
	sync.RWMutex
	fn ClassicalIPLinesFunc
}

// SetClassicalIPLines registers the routers' hook and returns a restore
// function (tests).
func SetClassicalIPLines(fn ClassicalIPLinesFunc) (restore func()) {
	classicalHook.Lock()
	previous := classicalHook.fn
	classicalHook.fn = fn
	classicalHook.Unlock()
	return func() {
		classicalHook.Lock()
		classicalHook.fn = previous
		classicalHook.Unlock()
	}
}

// classicalWithoutIP: the classical set's known content has no IP lines.
// Unknown content, a missing hook or a read error keep the old assumption
// that it has some.
func (p *Profile) classicalWithoutIP(item *Node) bool {
	classicalHook.RLock()
	fn := classicalHook.fn
	classicalHook.RUnlock()
	if fn == nil {
		return false
	}
	value, err := fn(context.Background(), ToJSONX(item), p.raw)
	return err == nil && value == false
}
