package yamlx

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// JSONKeyMode selects the key policy at a temporary YAML-to-JSON boundary.
type JSONKeyMode uint8

const (
	// RejectNonStringKeys rejects every effective key that is not a string.
	RejectNonStringKeys JSONKeyMode = iota
	// DiscardNonStringKeys omits non-string keys and their values.
	DiscardNonStringKeys
	// StringifyScalarKeys converts number, bool and null keys to JSON names.
	StringifyScalarKeys
)

// ProjectJSONKeys projects a caller-owned physical graph for ToJSON. It preserves
// alias sharing, flattens merges in effective order, and keeps the last value at
// the first position on converted-name collisions. It checks the alias budget
// before traversal; ToJSON must check it again on the projected subtree.
// This is the single temporary bridge until N8-9 removes jsonx from consumers.
func ProjectJSONKeys(n *Node, mode JSONKeyMode) error {
	if mode > StringifyScalarKeys {
		return fmt.Errorf("yamlx: invalid JSON key mode %d", mode)
	}
	if n == nil {
		return nil
	}
	if err := validateNode(n); err != nil {
		return err
	}
	plan := map[*Node][]*Node{}
	if err := planJSONKeys(n, mode, map[*Node]bool{}, plan); err != nil {
		return err
	}
	// Plan against the untouched graph: changing an anchor first would change
	// later merges' typed-key equality and converted-name collision order.
	for mapping, content := range plan {
		mapping.Content = content
	}
	return nil
}

func jsonKey(n *Node) (string, error) {
	n = Resolve(n)
	if n.Kind != yaml.ScalarNode {
		return "", &jsonKeyError{line: n.Line}
	}
	switch n.ShortTag() {
	case "!!str":
		return n.Value, nil
	case "!!null", "!!bool", "!!int", "!!float":
		data, err := ToJSON(n)
		if err != nil {
			return "", err
		}
		if len(data) > 0 && data[0] == '"' {
			var key string
			err = json.Unmarshal(data, &key)
			return key, err
		}
		return string(data), nil
	default:
		return "", &jsonKeyError{line: n.Line}
	}
}

func planJSONKeys(n *Node, mode JSONKeyMode, seen map[*Node]bool, plan map[*Node][]*Node) error {
	if n == nil || seen[n] {
		return nil
	}
	seen[n] = true
	content := n.Content
	if n.Kind == yaml.MappingNode {
		content = nil
		indices := map[string]int{}
		for key, value := range AllPairs(n) {
			name, isString := stringKey(key)
			if !isString {
				switch mode {
				case RejectNonStringKeys:
					return invalidNode(key, "JSON object key must be a string")
				case DiscardNonStringKeys:
					continue
				case StringifyScalarKeys:
					var err error
					name, err = jsonKey(key)
					if err != nil {
						return err
					}
				}
			}
			if i, ok := indices[name]; ok {
				content[i] = value
			} else {
				indices[name] = len(content) + 1
				content = append(content, Str(name), value)
			}
		}
		plan[n] = content
	}
	for _, child := range content {
		if err := planJSONKeys(child, mode, seen, plan); err != nil {
			return err
		}
	}
	return planJSONKeys(n.Alias, mode, seen, plan)
}

type jsonKeyError struct{ line int }

func (e *jsonKeyError) Error() string { return fmt.Sprintf(msgJSONKey, e.line) }
func (e *jsonKeyError) Unwrap() error { return ErrType }
