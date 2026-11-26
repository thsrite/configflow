package subs

import (
	"errors"

	"configflow/internal/logx"
	"configflow/internal/model"
	"configflow/internal/rules"
	"configflow/internal/yamlx"
	"encoding/json"
	"gopkg.in/yaml.v3"
	"slices"
	"strings"
)

var filterLogger = logx.Get("backend.utils.subscription_filter")

// Matcher is a compiled Python regex used with re.search.
type Matcher interface{ Search(text string) bool }

// compileRegex is re.compile(pattern) through rules.CompilePyRegex (RE2, or
// the backtracking engine for look-around, back references and the like).
// Only \N{name} escapes and repeat counts beyond int32 yield
// *rules.UnsupportedRegexError: Python would run those patterns.
func compileRegex(pattern string) (Matcher, error) {
	re, err := rules.CompilePyRegex(pattern, false)
	if err != nil {
		return nil, err
	}
	return re, nil
}

// isUnsupported tells apart "Python would run this pattern, this engine cannot".
func isUnsupported(err error) bool {
	var u *rules.UnsupportedRegexError
	return errors.As(err, &u)
}

// ValidateFilter reports the existing business prefix and native regex diagnostic.
func ValidateFilter(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if _, err := compileRegex(text); err != nil && !isUnsupported(err) {
		return msgFilterInvalid + err.Error()
	}
	return ""
}

// CompileFilter compiles the configured filter once for a subscription batch.
func CompileFilter(sub model.Subscription) Matcher {
	if sub.RegexFilter == nil {
		return nil
	}
	pattern := strings.TrimSpace(*sub.RegexFilter)
	if pattern == "" {
		return nil
	}
	re, err := compileRegex(pattern)
	if err != nil {
		if !isUnsupported(err) {
			filterLogger.Warningf(msgFilterIgnored, sub.Name, err.Error())
		}
		return nil
	}
	return re
}

// FilterNodes selects owned typed node records in input order.
func FilterNodes(sub model.Subscription, nodes []model.Node) []model.Node {
	re := CompileFilter(sub)
	if re == nil {
		return slices.Clone(nodes)
	}
	kept := make([]model.Node, 0, len(nodes))
	for _, node := range nodes {
		if re.Search(node.Name) {
			kept = append(kept, node)
		}
	}
	return kept
}

// FilterProxies selects original open proxy graph nodes without copying their parameters.
func FilterProxies(sub model.Subscription, nodes []*yamlx.Node) []*yamlx.Node {
	re := CompileFilter(sub)
	if re == nil {
		return slices.Clone(nodes)
	}
	kept := make([]*yamlx.Node, 0, len(nodes))
	for _, item := range nodes {
		proxy := yamlx.Resolve(item)
		if proxy == nil || proxy.Kind != yaml.MappingNode {
			continue
		}
		var name model.String
		data, err := yamlx.ToJSON(yamlx.Get(proxy, "name"))
		if err != nil {
			continue
		}
		if json.Unmarshal(data, &name) != nil {
			name = ""
		}
		if re.Search(string(name)) {
			kept = append(kept, item)
		}
	}
	return kept
}
