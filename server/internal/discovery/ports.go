package discovery

import (
	"configflow/internal/convert"
	"configflow/internal/repo"
	"configflow/internal/yamlx"
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
)

func customPorts(text string) (map[int]bool, error) {
	out := map[int]bool{}
	parsed, parseErr := yamlx.Parse([]byte(text))
	if parseErr != nil {
		if errors.Is(parseErr, yamlx.ErrPolicy) || errors.Is(parseErr, yamlx.ErrAliasRecursion) || errors.Is(parseErr, yamlx.ErrAliasBudget) {
			return out, customPortYAMLError(parseErr)
		}
		return out, nil
	}
	if parsed.Kind != yaml.MappingNode {
		return out, nil
	}
	projection := yamlx.Map()
	for _, key := range convert.InboundPortKeys {
		if node := yamlx.Get(parsed, key); node != nil {
			value, err := customTaggedPort(node, key)
			if err != nil {
				return out, err
			}
			yamlx.Set(projection, key, value)
		}
	}
	if listeners := yamlx.Resolve(yamlx.Get(parsed, "listeners")); listeners != nil && listeners.Kind == yaml.SequenceNode {
		selected := yamlx.Seq()
		for _, listener := range listeners.Content {
			entry := yamlx.Map()
			if name := yamlx.Resolve(yamlx.Get(listener, "name")); name != nil && name.Kind == yaml.ScalarNode {
				yamlx.Set(entry, "name", name)
			}
			if node := yamlx.Get(listener, "port"); node != nil {
				value, err := customTaggedPort(node, "listeners.port")
				if err != nil {
					return out, err
				}
				yamlx.Set(entry, "port", value)
			}
			selected.Content = append(selected.Content, entry)
		}
		yamlx.Set(projection, "listeners", selected)
	}

	for port := range convert.UsedPorts(projection) {
		out[int(port)] = true
	}
	return out, nil
}

// customTaggedPort applies the existing validation-error path when an unknown
// tag cannot decode to this field's integer type; failure is never a zero port.
func customTaggedPort(node *yamlx.Node, field string) (*yamlx.Node, error) {
	resolved := yamlx.Resolve(node)
	switch resolved.ShortTag() {
	case "!!str", "!!int", "!!float", "!!bool", "!!null", "!!timestamp", "!!binary", "!!seq", "!!map":
		return node, nil
	}
	var port int
	if err := yamlx.Decode(resolved, &port); err != nil {
		return nil, invalid(fmt.Sprintf(msgCustomPort, field))
	}
	return yamlx.Int(int64(port)), nil
}

func customPortYAMLError(cause error) error {
	failure := repo.NewValidationError(fmt.Sprintf(msgCustomPortYAML, cause))
	failure.Cause = cause
	return failure
}
