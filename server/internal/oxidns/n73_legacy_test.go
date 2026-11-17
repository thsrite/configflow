package oxidns

import (
	"math"
	"math/big"
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
