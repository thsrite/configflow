package oxidns

// RoutingGroup identifies a recognized provider owned by a rule routing block.
type RoutingGroup struct {
	Tag      string
	Provider *Node
}

// RuleRouting is the read-only common routing analysis used by rule projection.
type RuleRouting struct {
	Values       *Node
	Capabilities map[string]Capability
	Groups       []RoutingGroup
}

// Analyze exposes the existing routing recognizer without duplicating its logic.
func Analyze(document, settings *Node, p *Profile) RuleRouting {
	s := p.analyzeRouting(document, settings)
	out := RuleRouting{Values: s.values, Capabilities: s.caps}
	for _, g := range s.groups.all() {
		out.Groups = append(out.Groups, RoutingGroup{Tag: g.tag, Provider: g.provider})
	}
	return out
}

// References reports whether a document refers to a plugin tag.
func References(document *Node, tag string) bool { return refersTo(document, tag) }

// ParseExec recognizes the existing rule DNS execution grammar.
func ParseExec(value *Node, part string, plugins map[string]*Node) string {
	return parseExec(value, part, plugins)
}

// PinnedAction returns the canonical action of an owned pinned upstream chain.
func PinnedAction(key string, plugins map[string]*Node) *Node { return pinnedAction(key, plugins) }

// BlackholePlugin returns a fresh canonical rule DNS blackhole plugin.
func BlackholePlugin() *Node { return clone(blackholeNode) }

// RulesetParts returns the converter's parts for one rule behavior.
func RulesetParts(behavior string) []string { return managedRulesetParts(M("behavior", behavior)) }

// RuleEnabled applies the converter's existing enabled/library-enabled policy.
func RuleEnabled(item *Node) bool {
	return enabledOrDefault(item, "enabled") && enabledOrDefault(item, "library_enabled")
}
