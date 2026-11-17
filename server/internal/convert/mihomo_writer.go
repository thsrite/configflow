package convert

import (
	"encoding/base64"
	"fmt"
	"strings"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// mihomoDocumentNode is a temporary boundary for the generator's jsonx values
// and the old YAML loader's values. N7-3d removes Timestamp/bytes/Mapping support
// when migrating the remaining convert readers. N8 removes the jsonx boundary.
// Containers are visited physically once; repeated values become aliases, so
// yamlx can reject recursion/expansion before the native writer emits anything.
func mihomoDocumentNode(value any) (*yamlx.Node, error) {
	type listID struct {
		first  *any
		length int
	}
	seen := map[any]*yamlx.Node{}
	anchors := 0
	alias := func(n *yamlx.Node) *yamlx.Node {
		if n.Anchor == "" {
			anchors++
			n.Anchor = fmt.Sprintf("id%03d", anchors)
		}
		return &yamlx.Node{Kind: yaml.AliasNode, Value: n.Anchor, Alias: n}
	}
	var build func(any) (*yamlx.Node, error)
	build = func(value any) (*yamlx.Node, error) {
		switch v := value.(type) {
		case nil:
			return yamlx.Null(), nil
		case string:
			return yamlx.Str(v), nil
		case bool:
			return yamlx.Bool(v), nil
		case yamlx.Timestamp:
			return &yamlx.Node{Kind: yaml.ScalarNode, Tag: "!!timestamp", Value: v.String()}, nil
		case []byte:
			// Retain the old loaded binary value; line breaks are insignificant
			// to binary consumers, but are part of the Node scalar text.
			encoded := base64.StdEncoding.EncodeToString(v)
			var text strings.Builder
			for len(encoded) > 76 {
				text.WriteString(encoded[:76])
				text.WriteByte('\n')
				encoded = encoded[76:]
			}
			if encoded != "" {
				text.WriteString(encoded)
				text.WriteByte('\n')
			}
			return &yamlx.Node{Kind: yaml.ScalarNode, Tag: "!!binary", Value: text.String(), Style: yaml.LiteralStyle}, nil
		case *jsonx.Object:
			if v == nil {
				return yamlx.Null(), nil
			}
			if n := seen[v]; n != nil {
				return alias(n), nil
			}
			n := yamlx.Map()
			seen[v] = n
			for key, item := range v.All() {
				child, err := build(item)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, yamlx.Str(key), child)
			}
			return n, nil
		case *yamlx.Mapping:
			if v == nil {
				return yamlx.Null(), nil
			}
			if n := seen[v]; n != nil {
				return alias(n), nil
			}
			n := yamlx.Map()
			seen[v] = n
			for _, pair := range v.Pairs {
				key, err := build(pair.Key)
				if err != nil {
					return nil, err
				}
				child, err := build(pair.Value)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, key, child)
			}
			return n, nil
		case []any:
			n := yamlx.Seq()
			if len(v) > 0 {
				id := listID{&v[0], len(v)}
				if previous := seen[id]; previous != nil {
					return alias(previous), nil
				}
				seen[id] = n
			}
			for _, item := range v {
				child, err := build(item)
				if err != nil {
					return nil, err
				}
				n.Content = append(n.Content, child)
			}
			return n, nil
		}
		if number, ok := jsonx.AsNumber(value); ok {
			if number == "NaN" || number == "Infinity" || number == "-Infinity" {
				return yamlx.Float(number.Float64()), nil
			}
			return yamlx.FromJSON([]byte(string(number)))
		}
		return nil, fmt.Errorf("cannot represent an object: %T", value)
	}
	n, err := build(value)
	if err != nil {
		return nil, err
	}
	return yamlx.Encode(n)
}
