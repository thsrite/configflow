package substore

import (
	"configflow/internal/jsonx"
	"configflow/internal/yamlx"
)

// proxyYAMLValue is the temporary Node→jsonx boundary until N8-9's S-substore.
// Projection visits each physical node once and keeps aliases shared. ToJSON
// validates the projected subtree's alias budget before any output expansion;
// failures propagate, never becoming null or a truncated proxy.
func proxyYAMLValue(n *yamlx.Node) (any, error) {
	if err := yamlx.ProjectJSONKeys(n, yamlx.StringifyScalarKeys); err != nil {
		return nil, err
	}
	data, err := yamlx.ToJSON(n)
	if err != nil {
		return nil, err
	}
	return jsonx.Parse(data)
}
