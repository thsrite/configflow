package oxidns

import (
	"errors"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"strings"

	"configflow/internal/yamlx/yamlxtest"

	"gopkg.in/yaml.v3"
)

// Frozen pre-migration readers are test-only references; business builders are
// shared so the comparison isolates the YAML reader rather than porting logic.
func legacyParseYAML(text string, syntax error) (*Node, error) {
	doc, err := yamlxtest.FormerNativeDocument(text)
	if err != nil {
		return nil, syntax
	}
	if duplicateAnchor(doc, map[string]bool{}) {
		return nil, syntax
	}
	root := doc
	if doc.Kind == yaml.DocumentNode {
		if len(doc.Content) == 0 {
			return Null(), nil
		}
		root = doc.Content[0]
	}
	b := &builder{memo: map[*Node]*Node{}, syntax: syntax}
	out, err := b.node(root)
	for len(b.queue) > 0 && err == nil {
		src, dst := b.queue[0][0], b.queue[0][1]
		b.queue = b.queue[1:]
		err = b.fill(src, dst)
	}
	for _, pair := range b.aliasCopies {
		pair[1].Content = pair[0].Content
	}
	if err == nil {
		err = validateMerges(out, map[*Node]bool{})
	}
	return out, err
}

func legacySafeLoad(text string) (*Node, bool, error) {
	doc, err := yamlxtest.FormerNativeDocument(text)
	if err != nil {
		return nil, false, nil
	}
	if len(doc.Content) == 0 {
		return Null(), true, nil
	}
	memo := map[*Node]*Node{}
	var convert func(n *Node) (*Node, error)
	convert = func(n *Node) (*Node, error) {
		for n.Kind == yaml.AliasNode {
			n = n.Alias
		}
		if out, ok := memo[n]; ok {
			if out == nil {
				return nil, errUnsupportedYAML // recursive alias
			}
			return out, nil
		}
		memo[n] = nil
		out := &Node{Kind: n.Kind, Tag: n.ShortTag(), Value: n.Value, Style: n.Style, HeadComment: n.HeadComment, LineComment: n.LineComment, FootComment: n.FootComment}
		switch n.Kind {
		case yaml.ScalarNode:
			switch out.Tag {
			case tagInt:
				v, ok := new(big.Int).SetString(strings.ReplaceAll(n.Value, "_", ""), 0)
				if !ok {
					out.Tag = tagStr
				} else {
					out.Value = v.String()
				}
			case tagFloat:
				f, err := strconv.ParseFloat(strings.ReplaceAll(n.Value, "_", ""), 64)
				if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
					return nil, errUnsupportedYAML
				}
				out.Value = Float(f).Value
			case tagBool:
				out.Value = strconv.FormatBool(strings.EqualFold(n.Value, "true"))
			case tagNull:
				out.Value = "null"
			}
		case yaml.MappingNode, yaml.SequenceNode:
			for _, child := range n.Content {
				c, err := convert(child)
				if err != nil {
					return nil, err
				}
				out.Content = append(out.Content, c)
			}
			if n.Kind == yaml.MappingNode {
				for i := 0; i < len(out.Content); i += 2 {
					if out.Content[i].Kind != yaml.ScalarNode {
						return nil, errUnsupportedYAML
					}
				}
			}
		}
		if isStr(out) {
			out.Style = stringStyle(out.Value, out.Style)
		}
		memo[n] = out
		return out, nil
	}
	root, err := convert(doc.Content[0])
	return root, true, err
}

var (
	nativeInt   = regexp.MustCompile(`^[-+]?(?:[0-9][0-9_]*|0o[0-7_]+|0x[0-9a-fA-F_]+|0b[01_]+)$`)
	nativeFloat = regexp.MustCompile(`^[-+]?(?:(?:[0-9][0-9_]*\.[0-9_]*|\.[0-9_]+)(?:[eE][-+]?[0-9]+)?|[0-9][0-9_]*[eE][-+]?[0-9]+|\.(?:inf|Inf|INF|nan|NaN|NAN))$`)
)

func validateMerges(n *Node, active map[*Node]bool) error {
	if active[n] {
		return nil
	} // loadNative reports recursive aliases with its normal limit.
	active[n] = true
	for i, child := range n.Content {
		if isMap(n) && i%2 == 0 && child.Tag == "!!merge" {
			v := n.Content[i+1]
			if !isMap(v) && !isSeq(v) {
				return fail(messageMergeSources)
			}
			for _, source := range items(v) {
				if !isMap(source) {
					return fail(messageMergeSources)
				}
			}
		}
		if err := validateMerges(child, active); err != nil {
			return err
		}
	}
	return nil
}

type builder struct {
	memo        map[*Node]*Node
	queue       [][2]*Node
	syntax      error
	aliasCopies [][2]*Node
}

func (b *builder) node(src *Node) (*Node, error) {
	if src.Kind == yaml.AliasNode {
		value, err := b.node(src.Alias)
		if err != nil {
			return nil, err
		}
		if src.HeadComment != "" || src.LineComment != "" || src.FootComment != "" {
			copy := presentation(value, src)
			b.aliasCopies = append(b.aliasCopies, [2]*Node{value, copy})
			return copy, nil
		}
		return value, nil
	}
	if out, ok := b.memo[src]; ok {
		return out, nil
	}
	explicit := src.Style&yaml.TaggedStyle != 0
	var out *Node
	switch src.Kind {
	case yaml.ScalarNode:
		tag, value, err := resolveScalar(src, explicit)
		if err != nil {
			return nil, b.syntax
		}
		out = &Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
	case yaml.MappingNode, yaml.SequenceNode:
		tag := src.Tag
		switch {
		case !explicit || tag == "!!map" && src.Kind == yaml.MappingNode || tag == "!!seq" && src.Kind == yaml.SequenceNode:
			out = &Node{Kind: src.Kind, Tag: map[yaml.Kind]string{yaml.MappingNode: "!!map", yaml.SequenceNode: "!!seq"}[src.Kind]}
			b.queue = append(b.queue, [2]*Node{src, out})
		case tag == "!!set" && src.Kind == yaml.MappingNode, (tag == "!!omap" || tag == "!!pairs") && src.Kind == yaml.SequenceNode:
			out = &Node{Kind: yaml.ScalarNode, Tag: tagUnsupported}
		default:
			return nil, b.syntax
		}
	default:
		return nil, b.syntax
	}
	out.Style, out.HeadComment, out.LineComment, out.FootComment = src.Style&^yaml.TaggedStyle, src.HeadComment, src.LineComment, src.FootComment
	if out.Kind == yaml.ScalarNode && out.Tag != tagStr {
		out.Style = 0
	} else if isStr(out) {
		out.Style = stringStyle(out.Value, out.Style)
	}
	b.memo[src] = out
	return out, nil
}

func (b *builder) fill(src, dst *Node) error {
	if src.Kind == yaml.SequenceNode {
		dst.Content = make([]*Node, 0, len(src.Content))
		for _, child := range src.Content {
			n, err := b.node(child)
			if err != nil {
				return err
			}
			dst.Content = append(dst.Content, n)
		}
		return nil
	}
	seen := map[string]bool{}
	for i := 0; i+1 < len(src.Content); i += 2 {
		key, err := b.node(src.Content[i])
		if err != nil {
			return err
		}
		if (!isStr(key) && key.Tag != "!!merge") || key.Value == "__proto__" || key.Value == "prototype" || key.Value == "constructor" {
			return fail("OxiDNS YAML 字段名必须为安全文本")
		}
		if seen[key.Value] {
			return fail("OxiDNS YAML 包含重复字段")
		}
		seen[key.Value] = true
		value, err := b.node(src.Content[i+1])
		if err != nil {
			return err
		}
		dst.Content = append(dst.Content, key, value)
	}
	return nil
}

func resolveScalar(src *Node, explicit bool) (string, string, error) {
	value := src.Value
	plain := src.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle|yaml.LiteralStyle|yaml.FoldedStyle) == 0
	tag := src.Tag
	if !explicit {
		tag = tagStr
		if plain {
			switch {
			case value == "" || value == "~" || value == "null" || value == "Null" || value == "NULL":
				tag = tagNull
			case value == "true" || value == "True" || value == "TRUE" || value == "false" || value == "False" || value == "FALSE":
				tag = tagBool
			case nativeInt.MatchString(value):
				tag = tagInt
			case nativeFloat.MatchString(value):
				tag = tagFloat
			case value == "<<" && src.Tag == "!!merge":
				return "!!merge", value, nil
			case value == "=":
				return "", "", errMalformed
			}
		}
	}
	switch tag {
	case "!!merge":
		return "!!merge", value, nil
	case tagStr, "!":
		return tagStr, value, nil
	case tagNull:
		return tagNull, "null", nil
	case tagBool:
		switch strings.ToLower(value) {
		case "true", "yes", "on":
			return tagBool, "true", nil
		case "false", "no", "off":
			return tagBool, "false", nil
		}
		return "", "", errMalformed
	case tagInt:
		text := strings.TrimSpace(strings.ReplaceAll(value, "_", ""))
		base := 10
		unsigned := strings.TrimLeft(text, "-+")
		if strings.HasPrefix(unsigned, "0x") || strings.HasPrefix(unsigned, "0o") || strings.HasPrefix(unsigned, "0b") {
			base = 0
		}
		n, ok := new(big.Int).SetString(text, base)
		if !ok {
			return "", "", errMalformed
		}
		return tagInt, n.String(), nil
	case tagFloat:
		f, err := yamlFloat(value)
		if err != nil {
			return "", "", err
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			return tagFloat, strconv.FormatFloat(f, 'g', -1, 64), nil
		}
		return tagFloat, Float(f).Value, nil
	case "!!binary", "!!timestamp":
		return tagUnsupported, value, nil
	}
	return "", "", errMalformed
}

// yamlFloat is SafeConstructor.construct_yaml_float.
func yamlFloat(text string) (float64, error) {
	value := strings.ToLower(strings.ReplaceAll(text, "_", ""))
	sign := 1.0
	if strings.HasPrefix(value, "-") {
		sign = -1
	}
	if value != "" && (value[0] == '-' || value[0] == '+') {
		value = value[1:]
	}
	switch {
	case value == ".inf":
		return sign * math.Inf(1), nil
	case value == ".nan":
		return math.NaN(), nil
	case strings.Contains(value, ":"):
		digits := strings.Split(value, ":")
		total, base := 0.0, 1.0
		for i := len(digits) - 1; i >= 0; i-- {
			d, err := strconv.ParseFloat(digits[i], 64)
			if err != nil {
				return 0, errMalformed
			}
			total += d * base
			base *= 60
		}
		return sign * total, nil
	}
	if strings.HasPrefix(value, "0x") || value == "" {
		return 0, errMalformed
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, errMalformed
	}
	return sign * f, nil
}
