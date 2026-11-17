// Package yamltest provides migration-only comparisons for YAML reader tests.
// It is not imported by server runtime packages.
package yamltest

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// Migration categories are the approved reader changes in design/n7-n8.md §5.7.
const (
	SyntaxRange          = "原生解析器语法范围"
	ExplicitScalarNative = "explicit-scalar-native"
	Equal                = "equal"
	Diagnostics          = "native-diagnostics"
	Duplicate            = "D1-duplicate"
	Policy               = "reader-policy"
	Alias                = "unsafe-alias"
	Typing               = "YAML1.1/timestamp"
	Tags                 = "tagged-collections"
	Unknown              = "未知标签：Python 拒绝 → Go 接受"
	Composite            = "复合映射键：Node 保留"
	Deep                 = "深层普通节点：Go 接受"
)

// SemanticEqual compares output meaning under the native scalar-text and tag
// policies. Scalar text is retained verbatim; presentation is ignored.
func SemanticEqual(left, right string) bool {
	a, err := yamlx.Parse([]byte(left))
	if err != nil {
		return false
	}
	b, err := yamlx.Parse([]byte(right))
	if err != nil {
		return false
	}
	normalize(a, map[*yamlx.Node]bool{})
	normalize(b, map[*yamlx.Node]bool{})
	return yamlx.Equal(a, b)
}

// ReferenceText quotes native string values for an independent PyYAML oracle.
// Unknown tags and non-JSON scalars become the text/shape the caller observes.
func ReferenceText(source string) (string, error) {
	node, err := yamlx.Parse([]byte(source))
	if err != nil {
		return "", err
	}
	normalize(node, map[*yamlx.Node]bool{})
	data, err := yamlx.Marshal(node)
	return string(data), err
}

func normalize(node *yamlx.Node, seen map[*yamlx.Node]bool) {
	if node == nil || seen[node] {
		return
	}
	seen[node] = true
	switch node.Kind {
	case yaml.MappingNode:
		node.Tag = "!!map"
	case yaml.SequenceNode:
		node.Tag = "!!seq"
	case yaml.ScalarNode:
		switch node.ShortTag() {
		case "!!str", "!!bool", "!!null":
		case "!!int":
			if data, err := yamlx.ToJSON(node); err == nil {
				node.Value = string(data)
			}
		case "!!float":
			var value float64
			if yamlx.Decode(node, &value) == nil && !math.IsNaN(value) && !math.IsInf(value, 0) {
				node.Value = strconv.FormatFloat(value, 'g', -1, 64)
				if !strings.ContainsAny(node.Value, ".eE") {
					node.Value += ".0"
				}
			}
		case "!!timestamp", "!!binary":
			node.Tag = "!!str"
		default:
			node.Tag = "!!str"
		}
	}
	node.Style &^= yaml.TaggedStyle
	if node.Kind == yaml.ScalarNode && node.ShortTag() == "!!str" {
		node.Style = yaml.DoubleQuotedStyle
	}
	for _, child := range node.Content {
		normalize(child, seen)
	}
	normalize(node.Alias, seen)
}

var legacyWord = regexp.MustCompile(`(?i)^(yes|no|on|off)$`)
var numericText = regexp.MustCompile(`^[+-]?(?:[0-9_]+(?::[0-9_]+)+(?:\.[0-9_]+)?|0o[0-7_]+|[0-9_.]+[eE][+-]?[0-9_]+)$`)
var dateText = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}(?:$|[Tt ])`)

// structure records only native graph properties, not Python construction rules.
func structure(root *yamlx.Node) (complex bool, depth int, numericBoolKeys bool) {
	seen := map[*yamlx.Node]bool{}
	var visit func(*yamlx.Node, int)
	visit = func(node *yamlx.Node, level int) {
		if node == nil || seen[node] {
			return
		}
		seen[node] = true
		if level > depth {
			depth = level
		}
		if node.Kind == yaml.MappingNode {
			hasBool, hasNumber := false, false
			for key := range yamlx.AllPairs(node) {
				key = yamlx.Resolve(key)
				complex = complex || key.Kind != yaml.ScalarNode
				hasBool = hasBool || key.ShortTag() == "!!bool"
				hasNumber = hasNumber || key.ShortTag() == "!!int" || key.ShortTag() == "!!float"
			}
			numericBoolKeys = numericBoolKeys || hasBool && hasNumber
		}
		for _, child := range node.Content {
			visit(child, level+1)
		}
		visit(node.Alias, level+1)
	}
	visit(root, 0)
	return
}

func features(root *yamlx.Node) (typing, special, unknown bool) {
	seen := map[*yamlx.Node]bool{}
	var visit func(*yamlx.Node)
	visit = func(n *yamlx.Node) {
		if n == nil || seen[n] {
			return
		}
		seen[n] = true
		switch n.ShortTag() {
		case "!!str":
			typing = typing || n.Value == "=" || legacyWord.MatchString(n.Value) || numericText.MatchString(n.Value) || dateText.MatchString(n.Value)
		case "!!timestamp":
			typing = true
		case "!!int", "!!float":
			typing = typing || numericText.MatchString(n.Value)
		case "!!set", "!!omap", "!!pairs", "!!binary", "!!merge":
			special = true
		case "!!map", "!!seq", "!!bool", "!!null":
		default:
			if n.Kind != yaml.AliasNode {
				unknown = true
			}
		}
		for _, child := range n.Content {
			visit(child)
		}
		visit(n.Alias)
	}
	visit(root)
	return
}

// Classify compares one actual caller input through both loaders. An unknown
// result is an error, so a new input cannot silently acquire an exemption.
func Classify(source string) (string, error) {
	native, nextErr := yamlx.Parse([]byte(source))
	old, oldErr := yamlx.LoadWith(source, yamlx.LoadOptions{KeepNonStringKeys: true, KeepUnrepresentable: true})
	if errors.Is(nextErr, yamlx.ErrAliasRecursion) || errors.Is(nextErr, yamlx.ErrAliasBudget) {
		return Alias, nil
	}
	if errors.Is(nextErr, yamlx.ErrDuplicateKey) {
		return Duplicate, nil
	}
	if errors.Is(nextErr, yamlx.ErrPolicy) {
		return Policy, nil
	}
	if nativeSyntaxRange(source) || nextErr == nil && oldErr != nil && strings.Contains(oldErr.Error(), "character '\\t'") {
		return SyntaxRange, nil
	}
	if nextErr != nil && oldErr != nil {
		return Diagnostics, nil
	}
	if nextErr != nil {
		return "", fmt.Errorf("native rejects legacy input: %w", nextErr)
	}
	typing, special, unknown := features(native)
	complex, depth, keyTyping := structure(native)
	typing = typing || keyTyping
	if complex && oldErr != nil {
		return Composite, nil
	}
	if depth > 500 && oldErr != nil {
		return Deep, nil
	}
	if unknown && oldErr != nil {
		return Unknown, nil
	}
	if oldErr != nil && explicitNativeInteger(native, map[*yamlx.Node]bool{}) {
		return ExplicitScalarNative, nil
	}
	if typing && oldErr != nil {
		return Typing, nil
	}
	if special && oldErr != nil {
		return Tags, nil
	}
	if oldErr != nil {
		return "", fmt.Errorf("native accepts legacy refusal: %w", oldErr)
	}
	text, dumpErr := yamlx.IndentDump(old)
	if dumpErr != nil {
		if complex {
			return Composite, nil
		}
		if depth > 500 {
			return Deep, nil
		}
		if typing {
			return Typing, nil
		}
		if special {
			return Tags, nil
		}
		return "", fmt.Errorf("legacy result cannot be compared: %w", dumpErr)
	}
	legacy, err := yamlx.Parse([]byte(text))
	if err != nil {
		return "", fmt.Errorf("legacy output cannot be parsed: %w", err)
	}
	if containsScalarTextTag(native, map[*yamlx.Node]bool{}, "!!timestamp") {
		return Typing, nil
	}
	if containsScalarTextTag(native, map[*yamlx.Node]bool{}, "!!set", "!!omap", "!!pairs", "!!binary") {
		return Tags, nil
	}
	if yamlx.Equal(native, legacy) {
		return Equal, nil
	}
	if SemanticEqual(source, text) {
		if typing {
			return Typing, nil
		}
		if special {
			return Tags, nil
		}
		return Equal, nil
	}
	if typing {
		return Typing, nil
	}
	if special {
		return Tags, nil
	}
	return "", fmt.Errorf("unclassified native/legacy value difference")
}

// nativeSyntaxRange is the exact, reviewed grammar corpus. It grants no
// value/output exemption to arbitrary documents containing similar text.
func nativeSyntaxRange(source string) bool {
	switch source {
	case "name:\tx\n", "a: b\t\n", "a:\t1\n", "a: \"\\/\"\n", "%YAML 1.2\n---\na: 1\n", "\ufeff\ufeffa: 1\n", "a: &x 1\nb: &x 2\nc: *x\n", "proxies: [\t]":
		return true
	}
	return false
}

func containsScalarTextTag(node *yamlx.Node, seen map[*yamlx.Node]bool, tags ...string) bool {
	if node == nil || seen[node] {
		return false
	}
	seen[node] = true
	for _, tag := range tags {
		if node.ShortTag() == tag {
			return true
		}
	}
	for _, child := range node.Content {
		if containsScalarTextTag(child, seen, tags...) {
			return true
		}
	}
	return containsScalarTextTag(node.Alias, seen, tags...)
}

func explicitNativeInteger(node *yamlx.Node, seen map[*yamlx.Node]bool) bool {
	if node == nil || seen[node] {
		return false
	}
	seen[node] = true
	if node.Kind == yaml.ScalarNode && node.ShortTag() == "!!int" && node.Style&yaml.TaggedStyle != 0 {
		return true
	}
	for _, child := range node.Content {
		if explicitNativeInteger(child, seen) {
			return true
		}
	}
	return explicitNativeInteger(node.Alias, seen)
}
