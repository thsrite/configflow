package subs

import (
	"fmt"

	"configflow/internal/jsonx"
	"configflow/internal/yamlx"

	"gopkg.in/yaml.v3"
)

// readSubscriptionYAML is the temporary Node→jsonx boundary until N8-9 removes
// jsonx from subs. Parse validates the full document; ToJSON validates the
// projected graph again, so a subtree cannot bypass the alias budget. No Clone
// or per-occurrence copy is used while projecting the physical graph.
func readSubscriptionYAML(source string, stringKeysOnly bool) (any, error) {
	n, err := yamlx.Parse([]byte(source))
	if err != nil {
		return nil, err
	}
	mode := yamlx.RejectNonStringKeys
	if stringKeysOnly {
		mode = yamlx.DiscardNonStringKeys
	}
	if err := yamlx.ProjectJSONKeys(n, mode); err != nil {
		return nil, err
	}
	return subscriptionYAMLValue(n)
}

// readSubscriptionJSON gives valid JSON its native encoding/json semantics,
// including paired UTF-16 escapes. FromJSON is the shared native decoder. The
// legacy fallback preserves nonstandard JSON behavior until the N8-9 migration.
func readSubscriptionJSON(source string) (any, error) {
	n, err := yamlx.FromJSON([]byte(source))
	if err != nil {
		return jsonx.ParseString(source)
	}
	return subscriptionYAMLValue(n)
}

func subscriptionYAMLValue(n *yamlx.Node) (any, error) {
	data, err := yamlx.ToJSON(n)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}

// ProjectHostsYAMLKeys validates the hosts transition without coercing scalar
// keys, which the hosts business rules must inspect in source order. Values
// must fit their own alias/JSON budget. N8-9 removes this transition helper.
func ProjectHostsYAMLKeys(n *yamlx.Node) error {
	n = yamlx.Resolve(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for key, value := range yamlx.AllPairs(n) {
		if yamlx.Resolve(key).Kind != yaml.ScalarNode {
			return &hostYAMLKeyError{line: key.Line}
		}
		if _, err := yamlx.ToJSON(value); err != nil {
			return err
		}
	}
	return nil
}

type hostYAMLKeyError struct{ line int }

func (e *hostYAMLKeyError) Error() string { return fmt.Sprintf(msgHostsKeyShape, e.line) }
func (e *hostYAMLKeyError) Unwrap() error { return yamlx.ErrPolicy }
