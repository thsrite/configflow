package mosdns

import (
	"configflow/internal/yamlx"
	"errors"
	"gopkg.in/yaml.v3"
	"strings"
)

// Upstreams parses native YAML or the line-oriented DNS upstream notation.
// Only YAML syntax errors may fall back; policy and alias failures are returned.
func Upstreams(text string) ([]*yamlx.Node, error) {
	text = strings.TrimSpace(text)
	out := []*yamlx.Node{}
	if text == "" {
		return out, nil
	}
	root, err := yamlx.Parse([]byte(text))
	if err != nil && !errors.Is(err, yamlx.ErrSyntax) {
		return nil, customYAMLError(err)
	}
	if err == nil {
		root = yamlx.Resolve(root)
		switch root.Kind {
		case yaml.SequenceNode:
			for _, entry := range root.Content {
				node := yamlx.Resolve(entry)
				if node.Kind == yaml.MappingNode && yamlx.Get(node, "addr") != nil {
					out = append(out, entry)
				} else if textScalar(node) {
					out = append(out, yamlx.Map(yamlx.Str("addr"), yamlx.Str(node.Value)))
				}
			}
			if len(out) > 0 {
				return out, nil
			}
		case yaml.MappingNode:
			if yamlx.Get(root, "addr") != nil {
				return []*yamlx.Node{root}, nil
			}
		}
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) == 0 {
			continue
		}
		upstream := yamlx.Map(yamlx.Str("addr"), yamlx.Str(parts[0]))
		for i, part := range parts[1:] {
			if key, value, ok := strings.Cut(part, "="); ok {
				switch key {
				case "bootstrap":
					yamlx.Set(upstream, key, yamlx.Str(value))
				case "enable_pipeline":
					switch strings.ToLower(value) {
					case "true", "1", "yes":
						yamlx.Set(upstream, key, yamlx.Bool(true))
					case "false", "0", "no":
						yamlx.Set(upstream, key, yamlx.Bool(false))
					}
				}
			} else if i == 0 {
				yamlx.Set(upstream, "bootstrap", yamlx.Str(part))
			}
		}
		out = append(out, upstream)
	}
	return out, nil
}
